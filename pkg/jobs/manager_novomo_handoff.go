package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alhasaniq/consortium/pkg/novomo"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

func agentRunRowID(jobID, runID, nodeID string, attempt int) string {
	return fmt.Sprintf("%s:%s:%s:%d", jobID, fallbackString(runID, jobID), nodeID, attempt)
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func workflowNovomoTaskID(explicit, runID, jobID string) string {
	if taskID := strings.TrimSpace(explicit); taskID != "" {
		return taskID
	}
	source := fallbackString(runID, jobID)
	if strings.TrimSpace(source) == "" {
		source = "workflow"
	}
	sanitized := sanitizeNovomoTaskIDPart(source)
	candidate := "consortium-" + sanitized
	if len(candidate) <= 64 && !strings.HasPrefix(candidate, ".") && !strings.HasPrefix(candidate, "-") {
		return candidate
	}
	sum := sha256.Sum256([]byte(source))
	return "consortium-" + hex.EncodeToString(sum[:])[:32]
}

func sanitizeNovomoTaskIDPart(value string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(value) {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '.' || r == '_' || r == '-':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	out := strings.Trim(builder.String(), ".-")
	if out == "" {
		sum := sha256.Sum256([]byte(value))
		return hex.EncodeToString(sum[:])[:32]
	}
	return out
}

func (m *Manager) resolveNovomoHandoff(ctx context.Context, explicit *workflow.NovomoHandoffRef, upstreamNodeID, policy string, workflowTask bool, workflowTaskID string, jobID, runID string) (*workflow.NovomoHandoffRef, error) {
	explicit = workflow.NormalizeNovomoHandoff(explicit)
	upstreamNodeID = strings.TrimSpace(upstreamNodeID)
	policy = strings.TrimSpace(policy)
	if workflow.CountNovomoHandoffSources(explicit != nil, upstreamNodeID != "", workflowTask) > 1 {
		return nil, workflow.NewNonRetryableError(fmt.Errorf("set only one of inherit_from, inherit_from_node_id, or inherit_from_workflow_task"), "INVALID_CONFIG")
	}
	if err := workflow.ValidateNovomoHandoffRef("inherit_from", explicit); err != nil {
		return nil, workflow.NewNonRetryableError(err, "INVALID_CONFIG")
	}
	if explicit != nil {
		return explicit, nil
	}
	if workflowTask {
		ref := &workflow.NovomoHandoffRef{
			Kind:   workflow.NovomoHandoffKindTask,
			ID:     strings.TrimSpace(workflowTaskID),
			Policy: policy,
		}
		if err := workflow.ValidateNovomoHandoffRef("resolved inherit_from", ref); err != nil {
			return nil, workflow.NewNonRetryableError(err, "INVALID_CONFIG")
		}
		return ref, nil
	}
	if upstreamNodeID == "" {
		return nil, nil
	}

	row, err := m.storage.GetLatestAgentRunByNode(ctx, jobID, runID, upstreamNodeID)
	if err != nil {
		return nil, workflow.NewRetryableError(fmt.Errorf("lookup upstream Novomo handoff: %w", err), "STORAGE_ERROR")
	}
	if row == nil {
		return nil, workflow.NewNonRetryableError(fmt.Errorf("upstream Novomo node %q has no completed agent run row for run %q", upstreamNodeID, runID), "NOVOMO_HANDOFF_NOT_FOUND")
	}
	if !strings.EqualFold(strings.TrimSpace(row.Status), "completed") {
		return nil, workflow.NewNonRetryableError(fmt.Errorf("upstream Novomo node %q is not completed; status=%q", upstreamNodeID, row.Status), "NOVOMO_HANDOFF_NOT_READY")
	}
	ref, err := novomoHandoffFromAgentRun(row, policy)
	if err != nil {
		return nil, workflow.NewNonRetryableError(err, "NOVOMO_HANDOFF_NOT_FOUND")
	}
	return ref, nil
}

func novomoHandoffFromAgentRun(row *storage.AgentRun, policy string) (*workflow.NovomoHandoffRef, error) {
	if row == nil {
		return nil, fmt.Errorf("upstream agent run row is nil")
	}
	ref := &workflow.NovomoHandoffRef{Policy: strings.TrimSpace(policy)}
	switch strings.TrimSpace(row.RunKind) {
	case "novo_run":
		ref.Kind = workflow.NovomoHandoffKindNovoRun
		ref.ID = strings.TrimSpace(row.ExternalRunID)
	default:
		if strings.TrimSpace(row.ExternalJobRunID) != "" {
			ref.Kind = workflow.NovomoHandoffKindJobRun
			ref.ID = strings.TrimSpace(row.ExternalJobRunID)
		} else {
			ref.Kind = workflow.NovomoHandoffKindJob
			ref.ID = strings.TrimSpace(row.ExternalRunID)
		}
	}
	if err := workflow.ValidateNovomoHandoffRef("resolved inherit_from", ref); err != nil {
		return nil, err
	}
	return ref, nil
}

func novomoHandoffFromWorkflow(ref *workflow.NovomoHandoffRef) *novomo.HandoffRef {
	normalized := workflow.NormalizeNovomoHandoff(ref)
	if normalized == nil {
		return nil
	}
	return &novomo.HandoffRef{
		Kind:   normalized.Kind,
		ID:     normalized.ID,
		Policy: normalized.Policy,
	}
}

func novomoHandoffJSON(ref *workflow.NovomoHandoffRef) string {
	normalized := workflow.NormalizeNovomoHandoff(ref)
	if normalized == nil {
		return ""
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	return string(data)
}

func novomoHandoffFromJSON(raw string) *workflow.NovomoHandoffRef {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var ref workflow.NovomoHandoffRef
	if err := json.Unmarshal([]byte(raw), &ref); err != nil {
		return nil
	}
	normalized := workflow.NormalizeNovomoHandoff(&ref)
	if err := workflow.ValidateNovomoHandoffRef("inherit_from", normalized); err != nil {
		return nil
	}
	return normalized
}
