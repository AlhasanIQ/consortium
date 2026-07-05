package bench

import (
	"encoding/json"
	"time"
)

const (
	BenchmarkGlobalMMLU     = "global-mmlu"
	BenchmarkGlobalMMLULite = "global-mmlu-lite"
	BenchmarkMMLUPro        = "mmlu-pro"
	BenchmarkMath500        = "math-500"
)

type BenchmarkFormat string

const (
	BenchmarkFormatMCQA       BenchmarkFormat = "mcqa"
	BenchmarkFormatMathAnswer BenchmarkFormat = "math_answer"
)

func FormatForBenchmark(benchmark string) BenchmarkFormat {
	switch benchmark {
	case BenchmarkMath500:
		return BenchmarkFormatMathAnswer
	default:
		return BenchmarkFormatMCQA
	}
}

// DatasetItem is the canonical record format shared by all benchmark datasets.
type DatasetItem struct {
	ID           string   `json:"id"`
	Benchmark    string   `json:"benchmark"`
	Split        string   `json:"split"`
	Subject      string   `json:"subject"`
	Language     string   `json:"language"`
	Question     string   `json:"question"`
	Choices      []string `json:"choices,omitempty"`
	AnswerIndex  int      `json:"answer_index,omitempty"`
	AnswerLabel  string   `json:"answer_label"`
	GoldAnswer   string   `json:"gold_answer,omitempty"`
	GoldSolution string   `json:"gold_solution,omitempty"`
}

// ItemResult captures one evaluated benchmark item.
type ItemResult struct {
	ItemID           string          `json:"item_id"`
	Subject          string          `json:"subject"`
	Language         string          `json:"language"`
	AnswerLabel      string          `json:"answer_label"`
	Predicted        string          `json:"predicted"`
	ParseOK          bool            `json:"parse_ok"`
	Correct          bool            `json:"correct"`
	JobID            string          `json:"job_id,omitempty"`
	LatencyMS        float64         `json:"latency_ms"`
	TokensInput      int             `json:"tokens_input"`
	TokensOutput     int             `json:"tokens_output"`
	TotalTokens      int             `json:"total_tokens"`
	CostUSD          float64         `json:"cost_usd"`
	RawOutput        string          `json:"raw_output"`
	Error            string          `json:"error,omitempty"`
	FailureReason    string          `json:"failure_reason,omitempty"`
	OutputSource     string          `json:"output_source"`
	WorkflowID       string          `json:"workflow_id"`
	BenchmarkName    string          `json:"benchmark_name"`
	Attempts         int             `json:"attempts"`
	NonLetterRetries int             `json:"non_letter_retries"`
	AttemptDetails   []AttemptDetail `json:"attempt_details,omitempty"`
}

// AttemptDetail captures one execution attempt for a benchmark item.
type AttemptDetail struct {
	Attempt                  int     `json:"attempt"`
	JobID                    string  `json:"job_id,omitempty"`
	LatencyMS                float64 `json:"latency_ms"`
	TokensInput              int     `json:"tokens_input"`
	TokensOutput             int     `json:"tokens_output"`
	TotalTokens              int     `json:"total_tokens"`
	CostUSD                  float64 `json:"cost_usd"`
	RawOutput                string  `json:"raw_output"`
	Predicted                string  `json:"predicted"`
	ParseOK                  bool    `json:"parse_ok"`
	Correct                  bool    `json:"correct"`
	Error                    string  `json:"error,omitempty"`
	FailureReason            string  `json:"failure_reason,omitempty"`
	OutputSource             string  `json:"output_source"`
	ContractNodeID           string  `json:"contract_node_id,omitempty"`
	ContractModel            string  `json:"contract_model,omitempty"`
	ContractFinishReason     string  `json:"contract_finish_reason,omitempty"`
	ContractTokensOutput     int     `json:"contract_tokens_output,omitempty"`
	ContractMaxTokens        int     `json:"contract_max_tokens,omitempty"`
	ContractDiagnostic       string  `json:"contract_diagnostic,omitempty"`
	ContractExtractionMethod string  `json:"contract_extraction_method,omitempty"`
}

type AccuracyBucket struct {
	Total    int     `json:"total"`
	Correct  int     `json:"correct"`
	Accuracy float64 `json:"accuracy"`
}

type RunSummary struct {
	RunID                     string                    `json:"run_id"`
	Benchmark                 string                    `json:"benchmark"`
	Split                     string                    `json:"split"`
	WorkflowID                string                    `json:"workflow_id"`
	WorkflowName              string                    `json:"workflow_name"`
	TotalItems                int                       `json:"total_items"`
	CompletedItems            int                       `json:"completed_items"`
	FailedItems               int                       `json:"failed_items"`
	ParsedItems               int                       `json:"parsed_items"`
	CorrectItems              int                       `json:"correct_items"`
	Accuracy                  float64                   `json:"accuracy"`
	ParseRate                 float64                   `json:"parse_rate"`
	RetriedItems              int                       `json:"retried_items"`
	TotalAttempts             int                       `json:"total_attempts"`
	TotalNonLetterRetries     int                       `json:"total_non_letter_retries"`
	AdmissionRetries          int                       `json:"admission_retries"`
	ItemsWithAdmissionRetries int                       `json:"items_with_admission_retries"`
	FailureReasonCounts       map[string]int            `json:"failure_reason_counts"`
	AllAttemptFailureCounts   map[string]int            `json:"all_attempt_failure_counts"`
	TotalLatencyMS            float64                   `json:"total_latency_ms"`
	AvgLatencyMS              float64                   `json:"avg_latency_ms"`
	P50LatencyMS              float64                   `json:"p50_latency_ms"`
	P95LatencyMS              float64                   `json:"p95_latency_ms"`
	P99LatencyMS              float64                   `json:"p99_latency_ms"`
	TotalTokensInput          int                       `json:"total_tokens_input"`
	TotalTokensOutput         int                       `json:"total_tokens_output"`
	TotalTokens               int                       `json:"total_tokens"`
	AvgTokensPerItem          float64                   `json:"avg_tokens_per_item"`
	TotalCostUSD              float64                   `json:"total_cost_usd"`
	AvgCostUSDPerItem         float64                   `json:"avg_cost_usd_per_item"`
	BySubject                 map[string]AccuracyBucket `json:"by_subject"`
	ByLanguage                map[string]AccuracyBucket `json:"by_language"`
	StartedAt                 time.Time                 `json:"started_at"`
	CompletedAt               time.Time                 `json:"completed_at"`
	ElapsedSeconds            float64                   `json:"elapsed_seconds"`
	DatasetPath               string                    `json:"dataset_path"`
	RawWorkflowID             string                    `json:"raw_workflow_id"`
	NormalizedBenchmark       string                    `json:"normalized_benchmark"`
	NormalizedSplit           string                    `json:"normalized_split"`
	ExecutionEngine           string                    `json:"execution_engine"`
	ExecutionEngineNotes      string                    `json:"execution_engine_notes,omitempty"`
}

type RunResult struct {
	Summary          RunSummary      `json:"summary"`
	Items            []ItemResult    `json:"items"`
	WorkflowSnapshot json.RawMessage `json:"workflow_snapshot"`
}
