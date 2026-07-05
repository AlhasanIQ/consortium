package admin

import (
	"log"
	"strings"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/storage"
)

// --- Job ID collection & cost attribution ---

// collectUniqueJobIDs extracts unique, non-empty job IDs from a slice using getJobID.
// Preserves first-occurrence order.
func collectUniqueJobIDs[T any](items []T, getJobID func(T) string) []string {
	set := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		jobID := strings.TrimSpace(getJobID(item))
		if jobID == "" {
			continue
		}
		if _, exists := set[jobID]; exists {
			continue
		}
		set[jobID] = struct{}{}
		out = append(out, jobID)
	}
	return out
}

func uniqueAttemptJobIDs(attempts []storage.BenchmarkRunItemAttempt) []string {
	return collectUniqueJobIDs(attempts, func(a storage.BenchmarkRunItemAttempt) string { return a.JobID })
}

func collectBenchmarkItemJobIDs(items []storage.BenchmarkRunItem) []string {
	return collectUniqueJobIDs(items, func(i storage.BenchmarkRunItem) string { return i.JobID })
}

func applyExecutionUsageToBenchmarkItems(items []storage.BenchmarkRunItem, usageByJob map[string]ExecutionCostSummary) (int, float64, float64) {
	totalTokens := 0
	totalCost := 0.0
	totalLatency := 0.0
	for i := range items {
		jobID := strings.TrimSpace(items[i].JobID)
		if summary, ok := usageByJob[jobID]; ok {
			items[i].TotalTokens = summary.TotalTokens
			items[i].CostUSD = summary.TotalCost
			if summary.TotalLatencyMs > 0 {
				items[i].LatencyMS = summary.TotalLatencyMs
			}
		}
		totalTokens += items[i].TotalTokens
		totalCost += items[i].CostUSD
		totalLatency += items[i].LatencyMS
	}
	return totalTokens, totalCost, totalLatency
}

func collectBenchmarkRunAttemptJobIDs(results []bench.ItemResult) []string {
	set := make(map[string]struct{})
	out := make([]string, 0, len(results))
	for _, item := range results {
		for _, attempt := range item.AttemptDetails {
			jobID := strings.TrimSpace(attempt.JobID)
			if jobID == "" {
				continue
			}
			if _, exists := set[jobID]; exists {
				continue
			}
			set[jobID] = struct{}{}
			out = append(out, jobID)
		}
	}
	return out
}

func (s *Server) applyExecutionUsageToRunResults(run *benchmarkRunPlan) {
	if run == nil || len(run.ItemResults) == 0 {
		return
	}
	jobIDs := collectBenchmarkRunAttemptJobIDs(run.ItemResults)
	if len(jobIDs) == 0 {
		return
	}
	usageByJob, err := s.loadExecutionCostSummaries(jobIDs)
	if err != nil {
		log.Printf("Warning: failed to load inclusive execution usage for run %s: %v", run.RunID, err)
		return
	}

	enrichedCount := 0
	for i := range run.ItemResults {
		item := &run.ItemResults[i]
		itemTokens := 0
		itemCost := 0.0
		itemLatency := 0.0
		for j := range item.AttemptDetails {
			attempt := &item.AttemptDetails[j]
			jobID := strings.TrimSpace(attempt.JobID)
			if summary, ok := usageByJob[jobID]; ok {
				attempt.TotalTokens = summary.TotalTokens
				attempt.CostUSD = summary.TotalCost
				if summary.TotalLatencyMs > 0 {
					attempt.LatencyMS = summary.TotalLatencyMs
				}
				if summary.TotalTokens > 0 || summary.TotalCost > 0 || summary.TotalLatencyMs > 0 {
					enrichedCount++
				}
			}
			itemTokens += attempt.TotalTokens
			itemCost += attempt.CostUSD
			itemLatency += attempt.LatencyMS
		}
		item.TotalTokens = itemTokens
		item.CostUSD = itemCost
		item.LatencyMS = itemLatency
	}

	if enrichedCount == 0 && len(jobIDs) > 0 {
		log.Printf("Warning: usage enrichment produced zero updates for run %s (%d jobs queried)", run.RunID, len(jobIDs))
	}
}
