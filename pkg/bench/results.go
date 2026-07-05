package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	DefaultResultDir = "benchmarks/results"
)

func BuildRunID(benchmark, workflowID string, now time.Time) string {
	b := sanitizeForID(benchmark)
	w := sanitizeForID(workflowID)
	return fmt.Sprintf("%s_%s_%s", now.UTC().Format("20060102_150405"), b, w)
}

func BuildSummary(
	runID string,
	benchmark string,
	split string,
	workflowID string,
	workflowName string,
	datasetPath string,
	startedAt time.Time,
	completedAt time.Time,
	items []ItemResult,
) RunSummary {
	total := len(items)
	parsed := 0
	correct := 0
	failed := 0
	retriedItems := 0
	totalAttempts := 0
	totalNonLetterRetries := 0
	admissionRetries := 0
	itemsWithAdmissionRetries := 0
	failureReasonCounts := make(map[string]int, len(FailureReasonPrecedence()))
	allAttemptFailureCounts := make(map[string]int, len(FailureReasonPrecedence()))
	for _, reason := range FailureReasonPrecedence() {
		failureReasonCounts[reason] = 0
		allAttemptFailureCounts[reason] = 0
	}

	totalLatency := 0.0
	totalTokensInput := 0
	totalTokensOutput := 0
	totalTokens := 0
	totalCost := 0.0
	latencies := make([]float64, 0, len(items))

	subjectAgg := map[string]AccuracyBucket{}
	langAgg := map[string]AccuracyBucket{}

	for _, item := range items {
		if item.Error != "" {
			failed++
		}
		if item.ParseOK {
			parsed++
		}
		if item.Correct {
			correct++
		}
		totalAttempts += item.Attempts
		totalNonLetterRetries += item.NonLetterRetries
		if item.FailureReason != "" {
			if _, ok := failureReasonCounts[item.FailureReason]; !ok {
				failureReasonCounts[item.FailureReason] = 0
			}
			failureReasonCounts[item.FailureReason]++
		}
		if item.NonLetterRetries > 0 {
			retriedItems++
		}
		itemAdmissionRetries := 0
		for _, ad := range item.AttemptDetails {
			if ad.FailureReason != "" {
				if _, ok := allAttemptFailureCounts[ad.FailureReason]; !ok {
					allAttemptFailureCounts[ad.FailureReason] = 0
				}
				allAttemptFailureCounts[ad.FailureReason]++
				if ad.FailureReason == FailureReasonToolRuntimeFailure {
					itemAdmissionRetries++
				}
			}
		}
		admissionRetries += itemAdmissionRetries
		if itemAdmissionRetries > 0 {
			itemsWithAdmissionRetries++
		}
		totalLatency += item.LatencyMS
		totalTokensInput += item.TokensInput
		totalTokensOutput += item.TokensOutput
		totalTokens += item.TotalTokens
		totalCost += item.CostUSD
		latencies = append(latencies, item.LatencyMS)

		subj := normalizeBucketKey(item.Subject)
		b := subjectAgg[subj]
		b.Total++
		if item.Correct {
			b.Correct++
		}
		subjectAgg[subj] = b

		lang := normalizeBucketKey(item.Language)
		bl := langAgg[lang]
		bl.Total++
		if item.Correct {
			bl.Correct++
		}
		langAgg[lang] = bl
	}

	bySubject := finalizeBuckets(subjectAgg)
	byLanguage := finalizeBuckets(langAgg)

	accuracy := ratio(correct, total)
	parseRate := ratio(parsed, total)
	avgLatency := ratioFloat(totalLatency, total)
	avgTokens := ratioFloat(float64(totalTokens), total)
	avgCost := ratioFloat(totalCost, total)
	p50, p95, p99 := percentile(latencies, 50), percentile(latencies, 95), percentile(latencies, 99)

	return RunSummary{
		RunID:                     runID,
		Benchmark:                 benchmark,
		Split:                     split,
		WorkflowID:                workflowID,
		WorkflowName:              workflowName,
		TotalItems:                total,
		CompletedItems:            total - failed,
		FailedItems:               failed,
		ParsedItems:               parsed,
		CorrectItems:              correct,
		Accuracy:                  accuracy,
		ParseRate:                 parseRate,
		RetriedItems:              retriedItems,
		TotalAttempts:             totalAttempts,
		TotalNonLetterRetries:     totalNonLetterRetries,
		AdmissionRetries:          admissionRetries,
		ItemsWithAdmissionRetries: itemsWithAdmissionRetries,
		FailureReasonCounts:       failureReasonCounts,
		AllAttemptFailureCounts:   allAttemptFailureCounts,
		TotalLatencyMS:            totalLatency,
		AvgLatencyMS:              avgLatency,
		P50LatencyMS:              p50,
		P95LatencyMS:              p95,
		P99LatencyMS:              p99,
		TotalTokensInput:          totalTokensInput,
		TotalTokensOutput:         totalTokensOutput,
		TotalTokens:               totalTokens,
		AvgTokensPerItem:          avgTokens,
		TotalCostUSD:              totalCost,
		AvgCostUSDPerItem:         avgCost,
		BySubject:                 bySubject,
		ByLanguage:                byLanguage,
		StartedAt:                 startedAt,
		CompletedAt:               completedAt,
		ElapsedSeconds:            completedAt.Sub(startedAt).Seconds(),
		DatasetPath:               datasetPath,
		RawWorkflowID:             workflowID,
		NormalizedBenchmark:       benchmark,
		NormalizedSplit:           split,
		ExecutionEngine:           "jobs.Manager.ExecuteWorkflow",
	}
}

func SaveRunResult(outputDir string, result RunResult) (string, string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create result dir: %w", err)
	}

	fullPath := filepath.Join(outputDir, result.Summary.RunID+".json")
	summaryPath := filepath.Join(outputDir, result.Summary.RunID+".summary.json")

	if err := writeJSONFile(fullPath, result); err != nil {
		return "", "", err
	}
	if err := writeJSONFile(summaryPath, result.Summary); err != nil {
		return "", "", err
	}
	return fullPath, summaryPath, nil
}

func writeJSONFile(path string, data interface{}) error {
	payload, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json for %q: %w", path, err)
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}

func sanitizeForID(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer("/", "-", " ", "-", "_", "-", ".", "-")
	s = replacer.Replace(s)
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-'
	})
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, "-")
}

func normalizeBucketKey(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "unknown"
	}
	return v
}

func finalizeBuckets(input map[string]AccuracyBucket) map[string]AccuracyBucket {
	out := make(map[string]AccuracyBucket, len(input))
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		b := input[key]
		b.Accuracy = ratio(b.Correct, b.Total)
		out[key] = b
	}
	return out
}

func ratio(numerator, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func ratioFloat(numerator float64, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return numerator / float64(denominator)
}

func percentile(values []float64, p int) float64 {
	if len(values) == 0 {
		return 0
	}
	if p <= 0 {
		p = 0
	}
	if p >= 100 {
		p = 100
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	idx := (len(sorted) - 1) * p / 100
	return sorted[idx]
}
