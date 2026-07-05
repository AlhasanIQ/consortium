package admin

import (
	"math/rand"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/bench"
)

// buildInterleavedBenchmarkWork produces a round-robin interleaved work list
// across all plans so that items from different runs are distributed evenly
// across concurrent workers rather than being processed run-by-run.
func buildInterleavedBenchmarkWork(plans []benchmarkRunPlan) []benchmarkWorkItem {
	maxItems := 0
	totalItems := 0
	for _, run := range plans {
		if len(run.Items) > maxItems {
			maxItems = len(run.Items)
		}
		totalItems += len(run.Items)
	}

	workItems := make([]benchmarkWorkItem, 0, totalItems)
	for itemIndex := 0; itemIndex < maxItems; itemIndex++ {
		for runIndex := range plans {
			if itemIndex >= len(plans[runIndex].Items) {
				continue
			}
			workItems = append(workItems, benchmarkWorkItem{
				RunIndex:  runIndex,
				ItemIndex: itemIndex,
			})
		}
	}
	return workItems
}

// applyLimit trims or samples items down to limit. When sampleMode is
// benchmarkSampleModeRandom a deterministic shuffle is applied using sampleSeed.
func applyLimit(items []bench.DatasetItem, limit int, sampleMode string, sampleSeed int64) []bench.DatasetItem {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	if sampleMode == benchmarkSampleModeRandom {
		rngSeed := sampleSeed
		if rngSeed == 0 {
			rngSeed = time.Now().UnixNano()
		}
		rng := rand.New(rand.NewSource(rngSeed)) //nolint:gosec
		perm := rng.Perm(len(items))
		sampled := make([]bench.DatasetItem, 0, limit)
		for i := 0; i < limit; i++ {
			sampled = append(sampled, items[perm[i]])
		}
		return sampled
	}
	return items[:limit]
}

// filterBenchmarkItems filters items by language. When englishOnly is true
// only English items are kept, with a special carve-out for "unknown" language
// items in Global-MMLU benchmarks.
func filterBenchmarkItems(items []bench.DatasetItem, benchmarkName string, englishOnly bool) []bench.DatasetItem {
	if !englishOnly {
		return items
	}
	filtered := make([]bench.DatasetItem, 0, len(items))
	for _, item := range items {
		if isEnglishLanguage(item.Language) {
			filtered = append(filtered, item)
			continue
		}
		if (benchmarkName == bench.BenchmarkMMLUPro ||
			benchmarkName == bench.BenchmarkGlobalMMLU ||
			benchmarkName == bench.BenchmarkGlobalMMLULite) &&
			strings.TrimSpace(strings.ToLower(item.Language)) == "unknown" {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func isEnglishLanguage(language string) bool {
	lang := strings.ToLower(strings.TrimSpace(language))
	if lang == "" {
		return false
	}
	if lang == "english" {
		return true
	}
	lang = strings.ReplaceAll(lang, "_", "-")
	return strings.HasPrefix(lang, "en")
}
