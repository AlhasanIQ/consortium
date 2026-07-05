package admin

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/storage"
)

// --- Chart types ---

type benchmarkChartSeries struct {
	Split  string
	Points []benchmarkChartPoint
}

type benchmarkChartPoint struct {
	RunID      string
	WorkflowID string
	Benchmark  string
	Accuracy   float64
	CostUSD    float64
	TotalItems int
	Date       string
}

// --- Utility functions ---

func dedupeOrderedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func sortedMapKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		if strings.TrimSpace(k) == "" {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func parseIntDefault(raw string, fallback int) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func strPtr(s string) *string { return &s }

// --- Import & artifact handling ---

func (s *Server) importBenchmarkArtifacts(resultsDir string) (int, int, error) {
	entries, err := os.ReadDir(resultsDir)
	if err != nil {
		return 0, 0, fmt.Errorf("read %s: %w", resultsDir, err)
	}

	imported := 0
	failed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".summary.json") {
			continue
		}
		path := filepath.Join(resultsDir, name)
		runResult, ok, err := parseBenchmarkRunFile(path)
		if err != nil {
			failed++
			log.Printf("benchmark import skip (parse): %s err=%v", path, err)
			continue
		}
		if !ok {
			continue
		}
		if err := s.storage.UpsertBenchmarkRunResult(runResult, storage.BenchmarkRunPersistMeta{
			Status:       "imported",
			Source:       "imported",
			ArtifactPath: path,
		}); err != nil {
			failed++
			log.Printf("benchmark import skip (upsert): %s err=%v", path, err)
			continue
		}
		imported++
	}
	return imported, failed, nil
}

func parseBenchmarkRunFile(path string) (*storage.BenchmarkRunResultInput, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "[") {
		return nil, false, nil
	}
	var probe struct {
		Summary json.RawMessage `json:"summary"`
		Items   json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, false, err
	}
	if len(probe.Summary) == 0 || len(probe.Items) == 0 {
		// Not a canonical run artifact (e.g. incorrect_dive.json).
		return nil, false, nil
	}

	var rr bench.RunResult
	if err := json.Unmarshal(data, &rr); err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(rr.Summary.RunID) == "" || strings.TrimSpace(rr.Summary.WorkflowID) == "" {
		return nil, false, nil
	}
	converted := convertBenchRunResult(rr)
	return &converted, true, nil
}

// --- Charts & filters ---

func collectBenchmarkFilters(runs []storage.BenchmarkRun) ([]string, []string, []string, []string) {
	benchmarks := make(map[string]struct{})
	workflows := make(map[string]struct{})
	statuses := make(map[string]struct{})
	splits := make(map[string]struct{})
	for _, run := range runs {
		benchmarks[run.Benchmark] = struct{}{}
		workflows[run.WorkflowID] = struct{}{}
		statuses[run.Status] = struct{}{}
		if run.Split != "" {
			splits[run.Split] = struct{}{}
		}
	}
	return sortedMapKeys(benchmarks), sortedMapKeys(workflows), sortedMapKeys(statuses), sortedMapKeys(splits)
}

func buildRunChartSeries(runs []storage.BenchmarkRun) []benchmarkChartSeries {
	// First pass: find max TotalItems per benchmark+split to identify "full" runs.
	type bsKey struct{ benchmark, split string }
	maxItems := map[bsKey]int{}

	type candidateRun struct {
		run   storage.BenchmarkRun
		split string
	}
	var candidates []candidateRun

	for _, run := range runs {
		split := strings.TrimSpace(strings.ToLower(run.Split))
		if split != "dev" && split != "test" {
			continue
		}
		if strings.TrimSpace(strings.ToLower(run.Status)) != "completed" {
			continue
		}
		if strings.TrimSpace(strings.ToLower(run.Source)) == "optimizer" {
			continue
		}
		if run.ItemLimit != 0 {
			continue
		}
		candidates = append(candidates, candidateRun{run: run, split: split})
		key := bsKey{benchmark: run.Benchmark, split: split}
		if run.TotalItems > maxItems[key] {
			maxItems[key] = run.TotalItems
		}
	}

	// Second pass: only include runs whose TotalItems matches the full count.
	bySplit := map[string][]benchmarkChartPoint{
		"test": {},
		"dev":  {},
	}
	for _, c := range candidates {
		key := bsKey{benchmark: c.run.Benchmark, split: c.split}
		if c.run.TotalItems < maxItems[key] {
			continue
		}
		ts := c.run.CreatedAt.UTC()
		if c.run.StartedAt != nil {
			ts = c.run.StartedAt.UTC()
		}
		bySplit[c.split] = append(bySplit[c.split], benchmarkChartPoint{
			RunID:      c.run.ID,
			WorkflowID: c.run.WorkflowID,
			Benchmark:  c.run.Benchmark,
			Accuracy:   c.run.Accuracy,
			CostUSD:    c.run.TotalCostUSD,
			TotalItems: c.run.TotalItems,
			Date:       ts.Format(time.RFC3339),
		})
	}

	ordered := []string{"test", "dev"}
	out := make([]benchmarkChartSeries, 0, len(ordered))
	for _, split := range ordered {
		points := bySplit[split]
		if len(points) == 0 {
			continue
		}
		sort.SliceStable(points, func(i, j int) bool {
			return points[i].Date < points[j].Date
		})
		if len(points) > 30 {
			points = points[len(points)-30:]
		}
		out = append(out, benchmarkChartSeries{
			Split:  split,
			Points: points,
		})
	}
	return out
}

// failureChartSource controls which stored count maps to consult first.
type failureChartSource int

const (
	failureSourceAllAttempts failureChartSource = iota // AllAttemptFailureCounts -> FailureReasonCounts -> items
	failureSourceFinalOnly                             // FailureReasonCounts -> items
)

// buildFailureChartData produces sorted (labels, counts) for failure chart display.
// source determines which stored count maps are consulted first.
func buildFailureChartData(run *storage.BenchmarkRun, items []storage.BenchmarkRunItem, source failureChartSource) ([]string, []int) {
	counts := make(map[string]int)

	switch source {
	case failureSourceAllAttempts:
		for k, v := range run.AllAttemptFailureCounts {
			if v > 0 {
				counts[k] = v
			}
		}
		if len(counts) == 0 {
			for k, v := range run.FailureReasonCounts {
				if v > 0 {
					counts[k] = v
				}
			}
		}
	case failureSourceFinalOnly:
		for k, v := range run.FailureReasonCounts {
			if v > 0 {
				counts[k] = v
			}
		}
	}

	if len(counts) == 0 && len(items) > 0 {
		for _, item := range items {
			reason := strings.TrimSpace(item.FailureReason)
			if reason != "" {
				counts[reason]++
			}
		}
	}

	wrongAnswerCount := 0
	for _, item := range items {
		if !item.Correct && item.ParseOK && strings.TrimSpace(item.FailureReason) == "" {
			wrongAnswerCount++
		}
	}
	if wrongAnswerCount > 0 {
		counts["wrong_answer"] = wrongAnswerCount
	}

	return sortedChartData(counts)
}

func sortedChartData(counts map[string]int) ([]string, []int) {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	series := make([]int, 0, len(keys))
	for _, k := range keys {
		series = append(series, counts[k])
	}
	return keys, series
}

func topIncorrectSubjects(items []storage.BenchmarkRunItem, limit int) ([]string, []int) {
	bySubject := make(map[string]int)
	for _, item := range items {
		if item.Correct {
			continue
		}
		subject := strings.TrimSpace(item.Subject)
		if subject == "" {
			subject = "unknown"
		}
		bySubject[subject]++
	}

	type subjectCount struct {
		Subject string
		Count   int
	}
	list := make([]subjectCount, 0, len(bySubject))
	for subject, count := range bySubject {
		list = append(list, subjectCount{Subject: subject, Count: count})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Count == list[j].Count {
			return list[i].Subject < list[j].Subject
		}
		return list[i].Count > list[j].Count
	})
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}

	labels := make([]string, 0, len(list))
	values := make([]int, 0, len(list))
	for _, entry := range list {
		labels = append(labels, entry.Subject)
		values = append(values, entry.Count)
	}
	return labels, values
}

// --- Item loading ---

func loadDatasetItemByID(datasetPath, itemID string) (*bench.DatasetItem, bool, error) {
	datasetPath = strings.TrimSpace(datasetPath)
	if datasetPath == "" {
		return nil, false, nil
	}
	items, err := bench.LoadDataset(datasetPath, 0)
	if err != nil {
		// If path is relative and current cwd differs, try absolute via project root marker.
		if !filepath.IsAbs(datasetPath) {
			absPath, absErr := filepath.Abs(datasetPath)
			if absErr == nil {
				items, err = bench.LoadDataset(absPath, 0)
			}
		}
		if err != nil {
			return nil, false, err
		}
	}
	for i := range items {
		if items[i].ID == itemID {
			item := items[i]
			return &item, true, nil
		}
	}
	return nil, false, nil
}

func (s *Server) loadAllBenchmarkRunItems(runID string) ([]storage.BenchmarkRunItem, error) {
	// Keep paging aligned with storage.ListBenchmarkRunItems() clamp.
	const pageSize = 500
	var all []storage.BenchmarkRunItem
	for offset := 0; ; {
		items, err := s.storage.ListBenchmarkRunItems(runID, storage.BenchmarkRunItemFilters{
			Limit:  pageSize,
			Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(items) == 0 || len(items) < pageSize {
			break
		}
		offset += len(items)
	}
	return all, nil
}

// buildMergedItems loads all items and their full attempt history from the
// DB (original attempts preserved, rerun attempts appended by
// AppendBenchmarkRunItemResult) and returns them as bench.ItemResult plus
// the original run's start time. Cost enrichment is applied by the caller
// after this returns.
func (s *Server) buildMergedItems(run *benchmarkRunPlan) ([]bench.ItemResult, time.Time, error) {
	dbItems, err := s.loadAllBenchmarkRunItems(run.MergeIntoRunID)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("load items: %w", err)
	}
	dbAttempts, err := s.storage.ListAllBenchmarkRunItemAttempts(run.MergeIntoRunID)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("load attempts: %w", err)
	}

	merged := convertDBItemsToBenchResults(dbItems, dbAttempts)

	// Preserve the original run's start time.
	origRun, err := s.storage.GetBenchmarkRun(run.MergeIntoRunID)
	startedAt := run.StartedAt
	if err == nil && origRun != nil && origRun.StartedAt != nil && !origRun.StartedAt.IsZero() {
		startedAt = *origRun.StartedAt
	}

	return merged, startedAt, nil
}
