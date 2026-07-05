// Package seeds embeds and provides access to all seed workflow definitions.
// This is a leaf-level package with no internal dependencies.
package seeds

import (
	_ "embed"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
)

// Agent runtime examples
//
//go:embed data/agent-run-novomo-basic.json
var agentRunNovomoBasicSeed string

//go:embed data/superagent-novo-run-basic.json
var superagentNovoRunBasicSeed string

// L0 Aggregation Workflows
//
//go:embed data/aggregation-collect.json
var aggregationCollectSeed string

//go:embed data/aggregation-majority-vote.json
var aggregationMajorityVoteSeed string

//go:embed data/aggregation-synthesis.json
var aggregationSynthesisSeed string

//go:embed data/aggregation-judge.json
var aggregationJudgeSeed string

//go:embed data/aggregation-debate-decide.json
var aggregationDebateDecideSeed string

//go:embed data/aggregation-scoring.json
var aggregationScoringSeed string

//go:embed data/aggregation-peer-matrix.json
var aggregationPeerMatrixSeed string

// ErrNotFound is returned when a seed workflow is not found by ID.
var ErrNotFound = errors.New("seed not found")

// L1 Reasoning Primitives
//
//go:embed data/reasoning-informed-captain-synthesis.json
var reasoningSynthesisSeed string

//go:embed data/reasoning-informed-captain-synthesis-cheap.json
var reasoningSynthesisCheapSeed string

//go:embed data/reasoning-judge-pick.json
var reasoningJudgeSeed string

//go:embed data/reasoning-judge-pick-cheap.json
var reasoningJudgeCheapSeed string

//go:embed data/reasoning-judge-score-pick.json
var reasoningScoredSeed string

//go:embed data/reasoning-judge-score-pick-cheap.json
var reasoningScoredCheapSeed string

//go:embed data/reasoning-peer-score-pick.json
var reasoningPeerReviewSeed string

//go:embed data/reasoning-peer-score-pick-cheap.json
var reasoningPeerReviewCheapSeed string

//go:embed data/reasoning-majority-pick.json
var reasoningVoteSeed string

//go:embed data/reasoning-majority-pick-cheap.json
var reasoningVoteCheapSeed string

//go:embed data/reasoning-adversarial-defense-judge-pick.json
var reasoningDebateSeed string

//go:embed data/reasoning-adversarial-defense-judge-pick-cheap.json
var reasoningDebateCheapSeed string

//go:embed data/reasoning-camp-split-judge-pick.json
var reasoningCampDebateSeed string

//go:embed data/reasoning-camp-split-judge-pick-cheap.json
var reasoningCampDebateCheapSeed string

//go:embed data/reasoning-self-consistency-majority-pick.json
var reasoningSelfConsistencySeed string

//go:embed data/reasoning-self-consistency-majority-pick-cheap.json
var reasoningSelfConsistencyCheapSeed string

//go:embed data/reasoning-multi-round-majority-pick.json
var reasoningDeliberationSeed string

//go:embed data/reasoning-multi-round-majority-pick-cheap.json
var reasoningDeliberationCheapSeed string

// L2 Composite Workflows
//
//go:embed data/composite-judge-synthesis-cheap.json
var compositeJudgeSynthesisCheapSeed string

// L3 Benchmark Wrappers
//
//go:embed data/benchmark-informed-captain-synthesis.json
var benchmarkSynthesisSeed string

//go:embed data/benchmark-informed-captain-synthesis-cheap.json
var benchmarkSynthesisCheapSeed string

//go:embed data/benchmark-judge-pick.json
var benchmarkJudgeSeed string

//go:embed data/benchmark-judge-pick-cheap.json
var benchmarkJudgeCheapSeed string

//go:embed data/benchmark-judge-score-pick.json
var benchmarkScoredSeed string

//go:embed data/benchmark-judge-score-pick-cheap.json
var benchmarkScoredCheapSeed string

//go:embed data/benchmark-peer-score-pick.json
var benchmarkPeerReviewSeed string

//go:embed data/benchmark-peer-score-pick-cheap.json
var benchmarkPeerReviewCheapSeed string

//go:embed data/benchmark-adversarial-defense-judge-pick.json
var benchmarkDebateSeed string

//go:embed data/benchmark-adversarial-defense-judge-pick-cheap.json
var benchmarkDebateCheapSeed string

//go:embed data/benchmark-majority-pick.json
var benchmarkVoteSeed string

//go:embed data/benchmark-majority-pick-cheap.json
var benchmarkVoteCheapSeed string

//go:embed data/benchmark-camp-split-judge-pick.json
var benchmarkCampDebateSeed string

//go:embed data/benchmark-camp-split-judge-pick-cheap.json
var benchmarkCampDebateCheapSeed string

//go:embed data/benchmark-self-consistency-majority-pick.json
var benchmarkSelfConsistencySeed string

//go:embed data/benchmark-self-consistency-majority-pick-cheap.json
var benchmarkSelfConsistencyCheapSeed string

//go:embed data/benchmark-multi-round-majority-pick.json
var benchmarkDeliberationSeed string

//go:embed data/benchmark-multi-round-majority-pick-cheap.json
var benchmarkDeliberationCheapSeed string

// Math benchmark wrappers (cheap variants only)
//
//go:embed data/benchmark-math-informed-captain-synthesis-cheap.json
var benchmarkMathSynthesisCheapSeed string

//go:embed data/benchmark-math-judge-pick-cheap.json
var benchmarkMathJudgeCheapSeed string

//go:embed data/benchmark-math-judge-score-pick-cheap.json
var benchmarkMathScoredCheapSeed string

//go:embed data/benchmark-math-peer-score-pick-cheap.json
var benchmarkMathPeerReviewCheapSeed string

//go:embed data/benchmark-math-adversarial-defense-judge-pick-cheap.json
var benchmarkMathDebateCheapSeed string

//go:embed data/benchmark-math-majority-pick-cheap.json
var benchmarkMathVoteCheapSeed string

//go:embed data/benchmark-math-camp-split-judge-pick-cheap.json
var benchmarkMathCampDebateCheapSeed string

//go:embed data/benchmark-math-self-consistency-majority-pick-cheap.json
var benchmarkMathSelfConsistencyCheapSeed string

//go:embed data/benchmark-math-multi-round-majority-pick-cheap.json
var benchmarkMathDeliberationCheapSeed string

//go:embed data/benchmark-math-baseline-deepseek-v4-flash-cheap.json
var benchmarkMathBaselineDeepSeekV4FlashCheapSeed string

//go:embed data/benchmark-math-baseline-minimax-cheap.json
var benchmarkMathBaselineMinimaxCheapSeed string

//go:embed data/benchmark-math-baseline-mimo-cheap.json
var benchmarkMathBaselineMimoCheapSeed string

// Single-model baselines
//
//go:embed data/benchmark-baseline-deepseek-v4-flash-cheap.json
var benchmarkBaselineDeepSeekV4FlashCheapSeed string

//go:embed data/benchmark-baseline-minimax-cheap.json
var benchmarkBaselineMinimaxCheapSeed string

//go:embed data/benchmark-baseline-mimo-cheap.json
var benchmarkBaselineMimoCheapSeed string

// Workflow layer designations, derived from the seed ID prefix convention:
// L0 reusable aggregation internals, L1 reusable reasoning primitives,
// L2 composite strategies, L3 benchmark harnesses.
const (
	LayerL0 = "L0"
	LayerL1 = "L1"
	LayerL2 = "L2"
	LayerL3 = "L3"
)

// Layer returns the workflow layer designation for a workflow ID based on its
// naming-convention prefix (aggregation- → L0, reasoning- → L1,
// composite- → L2, benchmark- → L3),
// or "" when the ID does not match a known layer. The ID prefix is the single
// source of truth for layering — there is no separate stored layer field.
func Layer(id string) string {
	switch {
	case strings.HasPrefix(id, "aggregation-"):
		return LayerL0
	case strings.HasPrefix(id, "reasoning-"):
		return LayerL1
	case strings.HasPrefix(id, "composite-"):
		return LayerL2
	case strings.HasPrefix(id, "benchmark-"):
		return LayerL3
	default:
		return ""
	}
}

// Info contains metadata about a seed workflow for the frontend.
type Info struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	AggregationMethod string `json:"aggregation_method"`
	AgentCount        int    `json:"agent_count"`
	Layer             string `json:"layer"` // L0/L1/L2/L3 derived from ID prefix, or "" if uncategorized
}

// Definition contains the clean JSON and metadata needed to seed storage.
type Definition struct {
	ID          string
	Name        string
	Description string
	JSON        string
}

var (
	allJSONOnce        sync.Once
	allJSONCache       []string
	allDefinitionOnce  sync.Once
	allDefinitionCache []Definition
	allInfoOnce        sync.Once
	allInfoCache       []Info
)

// GetAllJSON returns all embedded seed workflow definitions as clean JSON strings
// (JSONC comments and trailing commas stripped).
func GetAllJSON() []string {
	allJSONOnce.Do(func() {
		raw := []string{
			// Agent runtime examples
			agentRunNovomoBasicSeed,
			superagentNovoRunBasicSeed,
			// L0 Aggregation Workflows
			aggregationCollectSeed,
			aggregationMajorityVoteSeed,
			aggregationSynthesisSeed,
			aggregationJudgeSeed,
			aggregationDebateDecideSeed,
			aggregationScoringSeed,
			aggregationPeerMatrixSeed,
			// L1 Reasoning Primitives
			reasoningSynthesisSeed,
			reasoningSynthesisCheapSeed,
			reasoningJudgeSeed,
			reasoningJudgeCheapSeed,
			reasoningScoredSeed,
			reasoningScoredCheapSeed,
			reasoningPeerReviewSeed,
			reasoningPeerReviewCheapSeed,
			reasoningVoteSeed,
			reasoningVoteCheapSeed,
			reasoningDebateSeed,
			reasoningDebateCheapSeed,
			reasoningCampDebateSeed,
			reasoningCampDebateCheapSeed,
			reasoningSelfConsistencySeed,
			reasoningSelfConsistencyCheapSeed,
			reasoningDeliberationSeed,
			reasoningDeliberationCheapSeed,
			// L2 Composite Workflows
			compositeJudgeSynthesisCheapSeed,
			// L3 Benchmark Wrappers
			benchmarkSynthesisSeed,
			benchmarkSynthesisCheapSeed,
			benchmarkJudgeSeed,
			benchmarkJudgeCheapSeed,
			benchmarkScoredSeed,
			benchmarkScoredCheapSeed,
			benchmarkPeerReviewSeed,
			benchmarkPeerReviewCheapSeed,
			benchmarkDebateSeed,
			benchmarkDebateCheapSeed,
			benchmarkVoteSeed,
			benchmarkVoteCheapSeed,
			benchmarkCampDebateSeed,
			benchmarkCampDebateCheapSeed,
			benchmarkSelfConsistencySeed,
			benchmarkSelfConsistencyCheapSeed,
			benchmarkDeliberationSeed,
			benchmarkDeliberationCheapSeed,
			// L3 Math Benchmark Wrappers (cheap)
			benchmarkMathSynthesisCheapSeed,
			benchmarkMathJudgeCheapSeed,
			benchmarkMathScoredCheapSeed,
			benchmarkMathPeerReviewCheapSeed,
			benchmarkMathDebateCheapSeed,
			benchmarkMathVoteCheapSeed,
			benchmarkMathCampDebateCheapSeed,
			benchmarkMathSelfConsistencyCheapSeed,
			benchmarkMathDeliberationCheapSeed,
			benchmarkMathBaselineDeepSeekV4FlashCheapSeed,
			benchmarkMathBaselineMinimaxCheapSeed,
			benchmarkMathBaselineMimoCheapSeed,
			// Single-model baselines
			benchmarkBaselineDeepSeekV4FlashCheapSeed,
			benchmarkBaselineMinimaxCheapSeed,
			benchmarkBaselineMimoCheapSeed,
		}
		allJSONCache = make([]string, len(raw))
		for i, r := range raw {
			allJSONCache[i] = stripJSONComments(r)
		}
	})
	return append([]string(nil), allJSONCache...)
}

// GetAllDefinitions returns all embedded seed workflow definitions with parsed metadata.
func GetAllDefinitions() []Definition {
	allDefinitionOnce.Do(func() {
		allSeeds := GetAllJSON()
		allDefinitionCache = make([]Definition, 0, len(allSeeds))
		for _, seedJSON := range allSeeds {
			var seedData struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				Description string `json:"description"`
			}
			if err := json.Unmarshal([]byte(seedJSON), &seedData); err != nil {
				log.Printf("Warning: failed to parse seed workflow metadata: %v", err)
				continue
			}
			allDefinitionCache = append(allDefinitionCache, Definition{
				ID:          seedData.ID,
				Name:        seedData.Name,
				Description: seedData.Description,
				JSON:        seedJSON,
			})
		}
	})
	return append([]Definition(nil), allDefinitionCache...)
}

// GetJSONByID returns the embedded seed workflow JSON by workflow ID.
func GetJSONByID(workflowID string) (string, error) {
	for _, definition := range GetAllDefinitions() {
		if definition.ID == workflowID {
			return definition.JSON, nil
		}
	}
	return "", ErrNotFound
}

// GetAllInfo returns metadata about all available seed workflows.
func GetAllInfo() []Info {
	allInfoOnce.Do(func() {
		allSeeds := GetAllJSON()
		allInfoCache = make([]Info, 0, len(allSeeds))

		for _, seedJSON := range allSeeds {
			var seedData struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				Description string `json:"description"`
				Nodes       []struct {
					Type string `json:"type"`
					Data struct {
						Type   string `json:"type"`
						Config struct {
							AggregationMethod string `json:"aggregationMethod"`
						} `json:"config"`
					} `json:"data"`
				} `json:"nodes"`
			}
			if err := json.Unmarshal([]byte(seedJSON), &seedData); err != nil {
				log.Printf("Warning: failed to parse seed workflow for metadata: %v", err)
				continue
			}

			// Count agent nodes and find aggregation method
			agentCount := 0
			aggregationMethod := "collect"
			for _, node := range seedData.Nodes {
				if node.Type == "agent" || node.Type == "agent_run" || node.Type == "novo_run" {
					agentCount++
				}
				if (node.Type == "aggregation" || node.Data.Type == "aggregation") && node.Data.Config.AggregationMethod != "" {
					aggregationMethod = node.Data.Config.AggregationMethod
				} else if node.Type == "result" && node.Data.Config.AggregationMethod != "" {
					// Legacy seed shape retained for backwards-compatible metadata parsing.
					aggregationMethod = node.Data.Config.AggregationMethod
				}
			}

			allInfoCache = append(allInfoCache, Info{
				ID:                seedData.ID,
				Name:              seedData.Name,
				Description:       seedData.Description,
				AggregationMethod: aggregationMethod,
				AgentCount:        agentCount,
				Layer:             Layer(seedData.ID),
			})
		}
	})

	return append([]Info(nil), allInfoCache...)
}
