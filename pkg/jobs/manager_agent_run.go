package jobs

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/novomo"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

type novomoRunClient interface {
	SubmitRun(ctx context.Context, req novomo.SubmitRunRequest) (*novomo.SubmitRunResponse, error)
	GetRun(ctx context.Context, runID string) (*novomo.Run, error)
}

type novomoRunStopper interface {
	StopRun(ctx context.Context, runID string) error
}

type novomoWakeClient interface {
	SubmitNovoRun(ctx context.Context, req novomo.SubmitNovoRunRequest) (*novomo.SubmitNovoRunResponse, error)
	GetNovoRun(ctx context.Context, runID string) (*novomo.NovoRun, error)
}

type novomoNovoRunStopper interface {
	StopNovoRun(ctx context.Context, runID string) error
}

type novomoStopClient interface {
	novomoRunStopper
	novomoNovoRunStopper
}

type novomoRuntimeURLProvider interface {
	BaseURL() string
}

var _ novomoRuntimeURLProvider = (*novomo.Client)(nil)

func (m *Manager) executeAgentRun(ctx context.Context, req *workflow.AgentRunRequest) (*workflow.AgentRunResult, error) {
	client, err := novomo.NewClientFromEnv()
	if err != nil {
		return nil, classifyNovomoClientError(err)
	}
	return m.executeAgentRunWithClient(ctx, req, client, defaultAgentRunPollInterval)
}

func (m *Manager) executeNovoRun(ctx context.Context, req *workflow.NovoRunRequest) (*workflow.AgentRunResult, error) {
	client, err := novomo.NewClientFromEnv()
	if err != nil {
		return nil, classifyNovomoClientError(err)
	}
	return m.executeNovoRunWithClient(ctx, req, client, defaultAgentRunPollInterval)
}

func (m *Manager) executeAgentRunWithClient(ctx context.Context, req *workflow.AgentRunRequest, client novomoRunClient, pollInterval time.Duration) (*workflow.AgentRunResult, error) {
	if req == nil {
		return nil, workflow.NewNonRetryableError(fmt.Errorf("agent run request is nil"), "INVALID_CONFIG")
	}
	if client == nil {
		return nil, workflow.NewNonRetryableError(fmt.Errorf("novomo client is nil"), "INVALID_CONFIG")
	}
	sandbox := workflow.NormalizeNovomoSandbox(req.Sandbox)
	if !workflow.IsSupportedNovomoSandbox(sandbox) {
		return nil, workflow.NewNonRetryableError(fmt.Errorf("agent_run sandbox %q is not supported", sandbox), "INVALID_CONFIG")
	}
	taskID := workflowNovomoTaskID(req.TaskID, req.ParentRunID, req.ParentJobID)
	inheritFrom, err := m.resolveNovomoHandoff(ctx, req.InheritFrom, req.InheritFromNodeID, req.InheritFromPolicy, req.InheritFromWorkflowTask, taskID, req.ParentJobID, req.ParentRunID)
	if err != nil {
		return nil, err
	}
	cfg := externalAgentRunConfig{
		RunKind:           "agent_run",
		Harness:           req.Harness,
		InheritFrom:       inheritFrom,
		TaskID:            taskID,
		ParentJobID:       req.ParentJobID,
		ParentExecutionID: req.ParentExecutionID,
		ParentRunID:       req.ParentRunID,
		ParentNodeID:      req.ParentNodeID,
		Attempt:           req.Attempt,
		TimeoutSeconds:    req.TimeoutSeconds,
		GraceSeconds:      int(agentRunPollGrace / time.Second),
		Submit: func(submitCtx context.Context) (*externalAgentRunSnapshot, error) {
			submit, err := client.SubmitRun(submitCtx, novomo.SubmitRunRequest{
				Prompt:         req.Prompt,
				Harness:        req.Harness,
				Sandbox:        sandbox,
				TaskID:         taskID,
				TimeoutSeconds: req.TimeoutSeconds,
				InheritFrom:    novomoHandoffFromWorkflow(inheritFrom),
				IdempotencyKey: req.IdempotencyKey,
			})
			if err != nil {
				return nil, err
			}
			if submit == nil || strings.TrimSpace(submit.RunID) == "" {
				return nil, workflow.NewRetryableError(fmt.Errorf("novomo returned empty run id"), "NOVOMO_MALFORMED_RESPONSE")
			}
			return &externalAgentRunSnapshot{
				ExternalRunID:    submit.RunID,
				ExternalJobRunID: submit.JobRunID,
				ExternalTaskID:   taskID,
				Harness:          req.Harness,
				Status:           string(submit.Status),
			}, nil
		},
		Get: func(getCtx context.Context, externalRunID string) (*externalAgentRunSnapshot, error) {
			run, err := client.GetRun(getCtx, externalRunID)
			if err != nil {
				return nil, err
			}
			return snapshotFromNovomoRun(run), nil
		},
	}
	if stopper, ok := client.(novomoRunStopper); ok {
		cfg.Stop = stopper.StopRun
	}
	return m.executeExternalAgentRunWithConfig(ctx, cfg, pollInterval)
}

func (m *Manager) executeNovoRunWithClient(ctx context.Context, req *workflow.NovoRunRequest, client novomoWakeClient, pollInterval time.Duration) (*workflow.AgentRunResult, error) {
	if req == nil {
		return nil, workflow.NewNonRetryableError(fmt.Errorf("novo run request is nil"), "INVALID_CONFIG")
	}
	if client == nil {
		return nil, workflow.NewNonRetryableError(fmt.Errorf("novomo client is nil"), "INVALID_CONFIG")
	}
	if req.TimeoutSeconds <= 0 {
		return nil, workflow.NewNonRetryableError(fmt.Errorf("novo_run timeout_seconds is required"), "INVALID_CONFIG")
	}
	if req.GraceSeconds < 0 {
		return nil, workflow.NewNonRetryableError(fmt.Errorf("novo_run grace_seconds must be non-negative"), "INVALID_CONFIG")
	}
	// The runner and workflow-file converter normally default sandbox before this
	// point. Keep this fallback for direct manager calls and resumed legacy paths.
	sandbox := workflow.NormalizeNovomoSandbox(req.Sandbox)
	if !workflow.IsSupportedNovomoSandbox(sandbox) {
		return nil, workflow.NewNonRetryableError(fmt.Errorf("novo_run sandbox %q is not supported", sandbox), "INVALID_CONFIG")
	}
	workflowTaskID := workflowNovomoTaskID("", req.ParentRunID, req.ParentJobID)
	inheritFrom, err := m.resolveNovomoHandoff(ctx, req.InheritFrom, req.InheritFromNodeID, req.InheritFromPolicy, req.InheritFromWorkflowTask, workflowTaskID, req.ParentJobID, req.ParentRunID)
	if err != nil {
		return nil, err
	}
	runtimeURL := strings.TrimSpace(req.RuntimeURL)
	runtimeURLExplicit := runtimeURL != ""
	if runtimeURL == "" {
		if provider, ok := client.(novomoRuntimeURLProvider); ok {
			runtimeURL = strings.TrimSpace(provider.BaseURL())
		}
	}
	if !runtimeURLExplicit && sandbox == "docker" {
		runtimeURL = dockerReachableRuntimeURL(runtimeURL)
	}
	cfg := externalAgentRunConfig{
		RunKind:           "novo_run",
		InheritFrom:       inheritFrom,
		ParentJobID:       req.ParentJobID,
		ParentExecutionID: req.ParentExecutionID,
		ParentRunID:       req.ParentRunID,
		ParentNodeID:      req.ParentNodeID,
		Attempt:           req.Attempt,
		TimeoutSeconds:    req.TimeoutSeconds,
		GraceSeconds:      req.GraceSeconds,
		Submit: func(submitCtx context.Context) (*externalAgentRunSnapshot, error) {
			submit, err := client.SubmitNovoRun(submitCtx, novomo.SubmitNovoRunRequest{
				Goal:           req.Goal,
				TaskID:         req.TaskID,
				TaskSummary:    req.TaskSummary,
				Identity:       req.Identity,
				Image:          req.Image,
				Sandbox:        sandbox,
				RuntimeURL:     runtimeURL,
				TimeoutSeconds: req.TimeoutSeconds,
				GraceSeconds:   req.GraceSeconds,
				RepoSpecs:      req.RepoSpecs,
				WorkSource:     req.WorkSource,
				InheritFrom:    novomoHandoffFromWorkflow(inheritFrom),
				IdempotencyKey: req.IdempotencyKey,
			})
			if err != nil {
				return nil, err
			}
			if submit == nil || strings.TrimSpace(submit.NovoRunID) == "" {
				return nil, workflow.NewRetryableError(fmt.Errorf("novomo returned empty novo run id"), "NOVOMO_MALFORMED_RESPONSE")
			}
			return &externalAgentRunSnapshot{
				ExternalRunID:  submit.NovoRunID,
				ExternalTaskID: submit.TaskID,
				Status:         string(submit.Status),
			}, nil
		},
		Get: func(getCtx context.Context, externalRunID string) (*externalAgentRunSnapshot, error) {
			run, err := client.GetNovoRun(getCtx, externalRunID)
			if err != nil {
				return nil, err
			}
			return snapshotFromNovomoRun(run), nil
		},
	}
	if stopper, ok := client.(novomoNovoRunStopper); ok {
		cfg.Stop = stopper.StopNovoRun
	}
	return m.executeExternalAgentRunWithConfig(ctx, cfg, pollInterval)
}

func snapshotFromNovomoRun(run *novomo.Run) *externalAgentRunSnapshot {
	if run == nil {
		return nil
	}
	return &externalAgentRunSnapshot{
		ExternalRunID:    run.RunID,
		ExternalJobRunID: run.RawJobRunID,
		ExternalTaskID:   run.TaskID,
		Harness:          run.Harness,
		Status:           string(run.Status),
		Output:           run.Output,
		TokensInput:      run.TokensInput,
		TokensOutput:     run.TokensOutput,
		CostUSD:          run.CostUSD,
		ErrorCode:        run.ErrorCode,
		ErrorMessage:     run.ErrorMessage,
		StartedAt:        run.StartedAt,
		FinishedAt:       run.FinishedAt,
	}
}

func dockerReachableRuntimeURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return strings.TrimSpace(raw)
	}

	switch parsed.Hostname() {
	case "localhost", "127.0.0.1", "::1":
	default:
		return strings.TrimSpace(raw)
	}

	if port := parsed.Port(); port != "" {
		// TODO(v0.1-security): host.docker.internal is a developer convenience.
		// Make the Docker callback host configurable before relying on this in
		// heterogeneous production/container environments.
		parsed.Host = net.JoinHostPort("host.docker.internal", port)
	} else {
		parsed.Host = "host.docker.internal"
	}
	return parsed.String()
}
