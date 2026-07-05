package admin

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/storage"
)

// --- Comparison types ---

type benchmarkComparisonPageData struct {
	Benchmark string                         `json:"benchmark"`
	Split     string                         `json:"split"`
	ControlID string                         `json:"control_id"`
	Metrics   []comparisonMetric             `json:"metrics"`
	Options   []storage.BenchmarkSplitOption `json:"options"`
}

type comparisonMetric struct {
	Run            storage.BenchmarkRun `json:"run"`
	IsControl      bool                 `json:"is_control"`
	AccuracyDelta  *float64             `json:"accuracy_delta,omitempty"`
	CostDelta      *float64             `json:"cost_delta_pct,omitempty"`
	ParseRateDelta *float64             `json:"parse_rate_delta,omitempty"`
}

// --- Comparison handlers ---

func (s *Server) handleBenchmarkComparison(w http.ResponseWriter, r *http.Request) {
	includeOptimizer := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_optimizer")), "true") || strings.TrimSpace(r.URL.Query().Get("include_optimizer")) == "1"
	options, err := s.storage.ListDistinctBenchmarkSplits(includeOptimizer)
	if err != nil {
		writeJSONError(w, "Failed to list benchmark options", http.StatusInternalServerError)
		return
	}

	benchmarkName := strings.TrimSpace(r.URL.Query().Get("benchmark"))
	split := strings.TrimSpace(r.URL.Query().Get("split"))
	controlWorkflow := strings.TrimSpace(r.URL.Query().Get("control"))

	// Default to first available option
	if benchmarkName == "" || split == "" {
		if len(options) > 0 {
			benchmarkName = options[0].Benchmark
			split = options[0].Split
		}
	}

	data := benchmarkComparisonPageData{
		Benchmark: benchmarkName,
		Split:     split,
		Options:   options,
	}

	if benchmarkName != "" && split != "" {
		runs, err := s.storage.ListAllBenchmarkRunsForSplit(benchmarkName, split, includeOptimizer)
		if err != nil {
			writeJSONError(w, "Failed to list benchmark runs for comparison", http.StatusInternalServerError)
			return
		}

		// Find control: user-specified or most recent run
		var control *storage.BenchmarkRun
		for i := range runs {
			if controlWorkflow != "" && runs[i].WorkflowID == controlWorkflow {
				control = &runs[i]
				break
			}
		}
		if control == nil && len(runs) > 0 {
			// Default to most accurate run (not most recent)
			best := 0
			for i := range runs {
				if runs[i].Accuracy > runs[best].Accuracy {
					best = i
				}
			}
			control = &runs[best]
		}
		if control != nil {
			data.ControlID = control.ID
		}

		for _, run := range runs {
			m := comparisonMetric{Run: run}
			if control != nil && run.ID == control.ID {
				m.IsControl = true
			} else if control != nil {
				accDelta := run.Accuracy - control.Accuracy
				m.AccuracyDelta = &accDelta
				parseDelta := run.ParseRate - control.ParseRate
				m.ParseRateDelta = &parseDelta
				if control.TotalCostUSD > 0 {
					costPct := ((run.TotalCostUSD - control.TotalCostUSD) / control.TotalCostUSD) * 100
					m.CostDelta = &costPct
				}
			}
			data.Metrics = append(data.Metrics, m)
		}
	}

	writeJSONResponse(w, data)
}

func (s *Server) handleBenchmarkCompareItems(w http.ResponseWriter, r *http.Request) {
	baseID := strings.TrimSpace(r.URL.Query().Get("base"))
	candidateID := strings.TrimSpace(r.URL.Query().Get("candidate"))
	if baseID == "" || candidateID == "" {
		writeJSONError(w, "base and candidate run IDs are required", http.StatusBadRequest)
		return
	}

	baseRun, err := s.storage.GetBenchmarkRun(baseID)
	if err != nil {
		writeJSONError(w, "Base run not found", http.StatusNotFound)
		return
	}
	candidateRun, err := s.storage.GetBenchmarkRun(candidateID)
	if err != nil {
		writeJSONError(w, "Candidate run not found", http.StatusNotFound)
		return
	}

	format := bench.FormatForBenchmark(candidateRun.Benchmark)

	baseItems, err := s.loadAllBenchmarkRunItems(baseID)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to load base run items: %v", err), http.StatusInternalServerError)
		return
	}
	candidateItems, err := s.loadAllBenchmarkRunItems(candidateID)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to load candidate run items: %v", err), http.StatusInternalServerError)
		return
	}

	baseMap := make(map[string]storage.BenchmarkRunItem, len(baseItems))
	for _, item := range baseItems {
		baseMap[item.ItemID] = item
	}

	var regressions, improvements []itemComparisonResult
	totalCompared, unchangedCorrect, unchangedWrong := 0, 0, 0
	neededCategoriesByParentJob := make(map[string]struct{})
	type comparisonTransition struct {
		base      storage.BenchmarkRunItem
		candidate storage.BenchmarkRunItem
		regressed bool
		improved  bool
	}
	transitions := make([]comparisonTransition, 0)

	for _, cItem := range candidateItems {
		bItem, ok := baseMap[cItem.ItemID]
		if !ok {
			continue
		}
		totalCompared++

		switch {
		case bItem.Correct && !cItem.Correct:
			transitions = append(transitions, comparisonTransition{
				base:      bItem,
				candidate: cItem,
				regressed: true,
			})
			if parentJobID := strings.TrimSpace(cItem.JobID); parentJobID != "" {
				neededCategoriesByParentJob[parentJobID] = struct{}{}
			}
		case !bItem.Correct && cItem.Correct:
			transitions = append(transitions, comparisonTransition{
				base:      bItem,
				candidate: cItem,
				improved:  true,
			})
			if parentJobID := strings.TrimSpace(bItem.JobID); parentJobID != "" {
				neededCategoriesByParentJob[parentJobID] = struct{}{}
			}
		case bItem.Correct && cItem.Correct:
			unchangedCorrect++
		default:
			unchangedWrong++
		}
	}

	categoryJobIDs := make([]string, 0, len(neededCategoriesByParentJob))
	for parentJobID := range neededCategoriesByParentJob {
		categoryJobIDs = append(categoryJobIDs, parentJobID)
	}
	childDataByParentJobID, err := s.loadBenchmarkChildDataByParentJobIDs(categoryJobIDs)
	if err != nil {
		log.Printf("Warning: failed to preload child data for benchmark compare-items base=%s candidate=%s: %v", baseID, candidateID, err)
		childDataByParentJobID = map[string]benchmarkChildAnalysisData{}
	}

	for _, transition := range transitions {
		if transition.regressed {
			regressions = append(regressions, itemComparisonResult{
				ItemID:                 transition.candidate.ItemID,
				Subject:                transition.candidate.Subject,
				AnswerLabel:            transition.candidate.AnswerLabel,
				BasePredicted:          transition.base.Predicted,
				CandidatePredicted:     transition.candidate.Predicted,
				CandidateFailureReason: transition.candidate.FailureReason,
				CandidateCategory:      s.classifyBenchmarkItemWithChildData(format, transition.candidate, childDataByParentJobID),
			})
			continue
		}
		if transition.improved {
			improvements = append(improvements, itemComparisonResult{
				ItemID:             transition.candidate.ItemID,
				Subject:            transition.candidate.Subject,
				AnswerLabel:        transition.candidate.AnswerLabel,
				BasePredicted:      transition.base.Predicted,
				CandidatePredicted: transition.candidate.Predicted,
				BaseFailureReason:  transition.base.FailureReason,
				BaseCategory:       s.classifyBenchmarkItemWithChildData(format, transition.base, childDataByParentJobID),
			})
		}
	}

	totalRegressions := len(regressions)
	totalImprovements := len(improvements)
	limit := parseIntDefault(r.URL.Query().Get("limit"), 0)
	if limit > 0 {
		if len(regressions) > limit {
			regressions = regressions[:limit]
		}
		if len(improvements) > limit {
			improvements = improvements[:limit]
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"base_run_id":           baseID,
		"candidate_run_id":      candidateID,
		"base_workflow_id":      baseRun.WorkflowID,
		"candidate_workflow_id": candidateRun.WorkflowID,
		"summary": map[string]int{
			"total_compared":    totalCompared,
			"regressed":         totalRegressions,
			"improved":          totalImprovements,
			"unchanged_correct": unchangedCorrect,
			"unchanged_wrong":   unchangedWrong,
		},
		"regressions":        regressions,
		"improvements":       improvements,
		"returned_limit":     limit,
		"returned_regressed": len(regressions),
		"returned_improved":  len(improvements),
	})
}
