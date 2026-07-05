package optimize

import (
	"context"
	"math/rand"
)

type Mutator interface {
	Mutate(ctx context.Context, req *MutationRequest) ([]*MutationResult, error)
}

type MutationRequest struct {
	Parent          *Organism
	Spec            *OptimizeSpec
	FailureCases    []FailureCase
	LearningLog     []LearningEntry
	Generation      int
	Count           int
	ProposalContext *ProposalContext // enriched context for instruction proposals (DSPy GroundedProposer-style)
}

type FailureAgentAnswer struct {
	NodeID  string `json:"node_id,omitempty"`
	Model   string `json:"model,omitempty"`
	Answer  string `json:"answer,omitempty"`
	Correct bool   `json:"correct"`
	ParseOK bool   `json:"parse_ok"`
}

// FailureNodeTrace captures per-node intermediate output from a failed
// benchmark execution, analogous to DSPy's execution trace per predictor.
// This gives the LLM proposer visibility into what each workflow node produced.
type FailureNodeTrace struct {
	NodeID string `json:"node_id"`
	Model  string `json:"model,omitempty"`
	Output string `json:"output,omitempty"` // trimmed raw output from this node
}

type FailureCase struct {
	ItemID         string               `json:"item_id"`
	Question       string               `json:"question"`
	CorrectAnswer  string               `json:"correct_answer"`
	Predicted      string               `json:"predicted"`
	ChildPredicted string               `json:"child_predicted,omitempty"`
	AgentAnswers   []FailureAgentAnswer `json:"agent_answers,omitempty"`
	NodeTraces     []FailureNodeTrace   `json:"node_traces,omitempty"` // per-node execution trace (DSPy-style)
	FailureReason  string               `json:"failure_reason"`
	Category       string               `json:"category"`
	Diagnosis      string               `json:"diagnosis,omitempty"`
	RawOutput      string               `json:"raw_output"`
	ParentJobID    string               `json:"parent_job_id,omitempty"`
	ChildJobID     string               `json:"child_job_id,omitempty"`
	Flagged        bool                 `json:"flagged"`
	FlagReason     string               `json:"flag_reason,omitempty"`
}

// prepareMutation validates the common MutationRequest fields and returns
// the resolved count and a ready-to-use RNG. All Mutator implementations
// share this preamble, so it lives here to avoid duplication.
func prepareMutation(req *MutationRequest, structRng *rand.Rand) (int, *rand.Rand, error) {
	if req == nil || req.Parent == nil || req.Spec == nil {
		return 0, nil, ErrMutationRequestInvalid
	}
	count := req.Count
	if count < 1 {
		count = 1
	}
	rng := structRng
	if rng == nil {
		rng = rand.New(rand.NewSource(rand.Int63())) //nolint:gosec // non-cryptographic sampling
	}
	return count, rng, nil
}

type MutationResult struct {
	Organism  *Organism
	Changes   []ParamChange
	Artifacts []MutationArtifact
	Reasoning string
}

type ParamChange struct {
	Path     string `json:"path"`
	OldValue string `json:"old_value"`
	NewValue string `json:"new_value"`
	Reason   string `json:"reason"`
}

type MutationArtifact struct {
	InputPromptHash  string `json:"input_prompt_hash"`
	InputPrompt      string `json:"input_prompt,omitempty"`
	RawOutputHash    string `json:"raw_output_hash"`
	RawOutput        string `json:"raw_output,omitempty"`
	ClaudeModel      string `json:"claude_model,omitempty"`
	ClaudeCLIVersion string `json:"claude_cli_version,omitempty"`
}
