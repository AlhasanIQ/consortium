package optimize

import "time"

// OptimizationRun tracks one optimization campaign.
type OptimizationRun struct {
	ID              string `json:"id"`
	WorkflowID      string `json:"workflow_id"`
	WorkflowVersion int    `json:"workflow_version"`
	Benchmark       string `json:"benchmark"`
	Split           string `json:"split"`
	ItemLimit       int    `json:"item_limit"`
	Concurrency     int    `json:"concurrency"`

	Spec *OptimizeSpec `json:"spec"`

	Strategy         string `json:"strategy"`
	PopulationSize   int    `json:"population_size"`
	ClaudeModel      string `json:"claude_model"`
	MutatorMode      string `json:"mutator_mode,omitempty"`
	RNGSeed          *int64 `json:"rng_seed,omitempty"`
	CompactArtifacts bool   `json:"compact_artifacts,omitempty"`

	ChildrenPerParent        int  `json:"children_per_parent,omitempty"`
	MaxChildrenPerGeneration int  `json:"max_children_per_generation,omitempty"`
	AdaptiveFanout           bool `json:"adaptive_fanout,omitempty"`

	TotalBudgetUSD float64 `json:"total_budget_usd"`
	SpentUSD       float64 `json:"spent_usd"`
	// DSPYMetricCallsUsed tracks persisted metric-call usage for strategy=dspy
	// runs so pause/resume budget accounting remains exact even for transient
	// minibatch evaluations that are not persisted as organism fitness.
	DSPYMetricCallsUsed int `json:"dspy_metric_calls_used,omitempty"`

	Status         string   `json:"status"`
	Generation     int      `json:"generation"`
	TotalOrganisms int      `json:"total_organisms"`
	BestOrganismID string   `json:"best_organism_id"`
	BestFitness    *Fitness `json:"best_fitness"`

	LearningLog []LearningEntry `json:"learning_log,omitempty"`

	OwnerPID        int        `json:"owner_pid,omitempty"`
	OwnerHostname   string     `json:"owner_hostname,omitempty"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at,omitempty"`

	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// LearningEntry records one mutation decision and outcome.
type LearningEntry struct {
	Generation   int       `json:"generation"`
	OrganismID   string    `json:"organism_id"`
	ParentID     string    `json:"parent_id"`
	MutationType string    `json:"mutation_type"`
	VerifyMethod string    `json:"verify_method,omitempty"`
	Description  string    `json:"description"`
	Outcome      string    `json:"outcome"`
	FitnessDelta float64   `json:"fitness_delta"`
	CreatedAt    time.Time `json:"created_at"`
}

func (r *OptimizationRun) IsTerminal() bool {
	switch r.Status {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func (r *OptimizationRun) IsOwnedAndActive(now time.Time, ttl time.Duration) bool {
	if r == nil || r.LastHeartbeatAt == nil {
		return false
	}
	if r.OwnerPID == 0 || r.OwnerHostname == "" {
		return false
	}
	return now.Sub(*r.LastHeartbeatAt) <= ttl
}
