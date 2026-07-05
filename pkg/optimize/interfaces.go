package optimize

import (
	"context"
	"encoding/json"
)

// BenchmarkEvaluator runs benchmark evaluations for candidate workflows.
type BenchmarkEvaluator interface {
	RunBenchmark(ctx context.Context, req RunBenchmarkRequest) (runID string, err error)
	RunBatchBenchmark(ctx context.Context, req RunBatchBenchmarkRequest) (runIDsByWorkflow map[string]string, err error)
	ReplayBenchmark(ctx context.Context, req ReplayBenchmarkRequest) (runID string, err error)
	WaitForCompletion(ctx context.Context) error
	GetRunSummary(ctx context.Context, runID string) (*Fitness, error)
	GetFailureCases(ctx context.Context, runID string, limit int) ([]FailureCase, error)
}

// FailureCaseEnricher is an optional evaluator capability that enriches sampled
// failure rows with deeper item-level drill context.
type FailureCaseEnricher interface {
	EnrichFailureCases(ctx context.Context, runID string, cases []FailureCase) ([]FailureCase, error)
}

// SuccessCaseProvider is an optional evaluator capability that retrieves
// correctly-answered benchmark items for use as positive grounding context
// in proposal prompts.
type SuccessCaseProvider interface {
	GetSuccessCases(ctx context.Context, runID string, limit int) ([]SuccessExample, error)
}

type RunBenchmarkRequest struct {
	WorkflowID  string
	Benchmark   string
	Split       string
	ItemLimit   int
	Concurrency int
	Meta        map[string]string
}

type RunBatchBenchmarkRequest struct {
	WorkflowIDs []string
	Benchmark   string
	Split       string
	ItemLimit   int
	Concurrency int
	Meta        map[string]string
}

type ReplayBenchmarkRequest struct {
	BaseRunID        string
	WorkflowID       string
	Items            []string
	Mode             string // required|best_effort|off
	Concurrency      int
	ChangedWorkflows []string
}

// PopulationStore persists run state and organism history.
type PopulationStore interface {
	CreateRun(ctx context.Context, run *OptimizationRun) error
	UpdateRunProgress(ctx context.Context, runID string, generation int, bestOrgID string, bestFitness *Fitness, spentUSD float64, totalOrganisms int, dspyMetricCallsUsed int) error
	UpdateRunStatus(ctx context.Context, runID string, status string) error
	UpdateRunLease(ctx context.Context, runID string, ownerPID int, ownerHostname string) error
	ClearRunLease(ctx context.Context, runID string) error
	GetRun(ctx context.Context, runID string) (*OptimizationRun, error)
	ListRuns(ctx context.Context, status string, workflowID string, limit int) ([]OptimizationRun, error)

	CreateOrganism(ctx context.Context, organism *Organism) error
	UpdateOrganismFitness(ctx context.Context, orgID, benchRunID string, fitness *Fitness) error
	GetOrganism(ctx context.Context, orgID string) (*Organism, error)
	GetOrganismsByGeneration(ctx context.Context, runID string, generation int, limit int) ([]*Organism, error)
	GetAllOrganisms(ctx context.Context, runID string) ([]*Organism, error)
	GetBestOrganisms(ctx context.Context, runID string, limit int) ([]*Organism, error)
	GetOrganismLineage(ctx context.Context, orgID string) ([]*Organism, error)

	AppendLearningEntry(ctx context.Context, runID string, entry *LearningEntry) error
	GetLearningLog(ctx context.Context, runID string, limit int) ([]LearningEntry, error)

	CreateParamChanges(ctx context.Context, organismID string, changes []ParamChange) error
	GetParamChanges(ctx context.Context, organismID string) ([]ParamChange, error)
	CreateMutationArtifacts(ctx context.Context, organismID string, artifacts []MutationArtifact, compact bool) error
	GetMutationArtifacts(ctx context.Context, organismID string) ([]MutationArtifact, error)
}

// WorkflowManager manages temporary candidate workflows.
type WorkflowManager interface {
	CreateTemporaryWorkflow(ctx context.Context, id string, workflowJSON json.RawMessage, name string, description string) error
	DeleteTemporaryWorkflow(ctx context.Context, id string) error
}
