package runtime

type ReplayMode string

const (
	ReplayModeOff        ReplayMode = "off"
	ReplayModeBestEffort ReplayMode = "best_effort"
	ReplayModeRequired   ReplayMode = "required"
)

type ReplaySeedNode struct {
	NodeID         string                 `json:"node_id"`
	Output         string                 `json:"output"`
	TokensInput    int                    `json:"tokens_input,omitempty"`
	TokensOutput   int                    `json:"tokens_output,omitempty"`
	Cost           float64                `json:"cost,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	SourceJobID    string                 `json:"source_job_id,omitempty"`
	SourceRunID    string                 `json:"source_run_id,omitempty"`
	SourceAttempt  int                    `json:"source_attempt,omitempty"`
	OutputChecksum string                 `json:"output_checksum,omitempty"`
}

type ReplayPlan struct {
	BaseDAGHash      string            `json:"base_dag_hash"`
	CandidateDAGHash string            `json:"candidate_dag_hash"`
	ReuseNodeIDs     []string          `json:"reuse_node_ids"`
	ExecuteNodeIDs   []string          `json:"execute_node_ids"`
	DirtyNodeIDs     []string          `json:"dirty_node_ids,omitempty"`
	ReasonByNode     map[string]string `json:"reason_by_node,omitempty"`
}

type ReplayRequest struct {
	Mode        ReplayMode                `json:"mode"`
	BaseRunID   string                    `json:"base_run_id,omitempty"`
	BaseJobID   string                    `json:"base_job_id,omitempty"`
	Plan        *ReplayPlan               `json:"plan,omitempty"`
	SeedNodes   map[string]ReplaySeedNode `json:"seed_nodes,omitempty"`
	ChildByNode map[string]*ReplayRequest `json:"child_by_node,omitempty"`
}
