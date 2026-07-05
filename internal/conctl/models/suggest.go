package models

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	DefaultRepoPath = "benchmarks/models_repo/models_flat.json"
	DefaultMinIntel = 30.0
	DefaultMaxCost  = 1000.0
)

// Repo holds flattened model records used by bench tooling.
type Repo struct {
	SnapshotAt string  `json:"snapshot_at"`
	Models     []Model `json:"models"`
}

// Model is the minimal record needed for cost/intelligence model suggestions.
type Model struct {
	ModelID               string   `json:"model_id"`
	Slug                  string   `json:"slug"`
	Name                  string   `json:"name"`
	ShortName             string   `json:"short_name"`
	OpenRouterModelID     string   `json:"openrouter_model_id"`
	OpenRouterModelName   string   `json:"openrouter_model_name"`
	Deprecated            bool     `json:"deprecated"`
	ReasoningModel        bool     `json:"reasoning_model"`
	IntelligenceIndex     *float64 `json:"intelligence_index"`
	CostToRunIndexTotalUS *float64 `json:"cost_to_run_index_total_usd"`
}

func (m Model) CandidateID() string {
	if id := strings.TrimSpace(m.OpenRouterModelID); id != "" {
		return id
	}
	return strings.TrimSpace(m.ModelID)
}

type SuggestOptions struct {
	Top               int
	Track             string // cheap|balanced|intelligence
	Query             string
	IncludeDeprecated bool
	Reasoning         string // any|true|false
	MinIntel          float64
	MaxCost           float64
}

type SuggestResult struct {
	SnapshotAt      string
	Cheap           []Model
	Expensive       []Model
	MostIntelligent []Model
}

func LoadModelsRepo(path string) (*Repo, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultRepoPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read models repo %s: %w", path, err)
	}
	var repo Repo
	if err := json.Unmarshal(data, &repo); err != nil {
		return nil, fmt.Errorf("decode models repo %s: %w", path, err)
	}
	if len(repo.Models) == 0 {
		return nil, fmt.Errorf("models repo %s has no models", path)
	}
	return &repo, nil
}

func Suggest(repo *Repo, opts SuggestOptions) (*SuggestResult, error) {
	if repo == nil {
		return nil, fmt.Errorf("repo is required")
	}
	if opts.Top < 1 {
		opts.Top = 5
	}
	if strings.TrimSpace(opts.Reasoning) == "" {
		opts.Reasoning = "true"
	}
	if opts.MinIntel <= 0 {
		opts.MinIntel = DefaultMinIntel
	}
	if opts.MaxCost <= 0 {
		opts.MaxCost = DefaultMaxCost
	}

	filtered, err := filter(repo.Models, opts)
	if err != nil {
		return nil, err
	}
	frontier := paretoFrontier(filtered, opts.MinIntel, opts.MaxCost)

	cheap := append([]Model(nil), frontier...)
	sortByWeightedComposite(cheap, true)
	cheap = applyLimit(cheap, opts.Top)

	expensive := append([]Model(nil), frontier...)
	sortByWeightedComposite(expensive, false)
	expensive = applyLimit(expensive, opts.Top)

	all := append([]Model(nil), filtered...)
	sort.SliceStable(all, func(i, j int) bool {
		return descPtrFloat(all[i].IntelligenceIndex, all[j].IntelligenceIndex, all[i].Slug, all[j].Slug)
	})
	all = applyLimit(all, opts.Top)

	return &SuggestResult{
		SnapshotAt:      strings.TrimSpace(repo.SnapshotAt),
		Cheap:           cheap,
		Expensive:       expensive,
		MostIntelligent: all,
	}, nil
}

func TrackCandidates(result *SuggestResult, track string) []Model {
	if result == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(track)) {
	case "", "cheap":
		return append([]Model(nil), result.Cheap...)
	case "intelligence":
		return append([]Model(nil), result.MostIntelligent...)
	case "balanced":
		combined := make([]Model, 0, len(result.Cheap)+len(result.Expensive))
		seen := make(map[string]struct{})
		for _, model := range append(append([]Model(nil), result.Cheap...), result.Expensive...) {
			key := strings.ToLower(model.CandidateID())
			if key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			combined = append(combined, model)
		}
		return combined
	default:
		return append([]Model(nil), result.Cheap...)
	}
}

func filter(models []Model, opts SuggestOptions) ([]Model, error) {
	reasoning := strings.ToLower(strings.TrimSpace(opts.Reasoning))
	if reasoning == "" {
		reasoning = "true"
	}
	switch reasoning {
	case "any", "true", "false":
	default:
		return nil, fmt.Errorf("unsupported reasoning filter %q", opts.Reasoning)
	}

	query := strings.ToLower(strings.TrimSpace(opts.Query))
	out := make([]Model, 0, len(models))
	for _, model := range models {
		if !opts.IncludeDeprecated && model.Deprecated {
			continue
		}
		switch reasoning {
		case "true":
			if !model.ReasoningModel {
				continue
			}
		case "false":
			if model.ReasoningModel {
				continue
			}
		}
		if query != "" {
			hay := strings.ToLower(strings.Join([]string{model.Name, model.ShortName, model.Slug, model.ModelID, model.OpenRouterModelID, model.OpenRouterModelName}, " "))
			if !strings.Contains(hay, query) {
				continue
			}
		}
		out = append(out, model)
	}
	return out, nil
}

func paretoFrontier(models []Model, minIntel, maxCost float64) []Model {
	cands := make([]Model, 0, len(models))
	for _, model := range models {
		if model.IntelligenceIndex == nil || model.CostToRunIndexTotalUS == nil {
			continue
		}
		if minIntel > 0 && *model.IntelligenceIndex < minIntel {
			continue
		}
		if maxCost > 0 && *model.CostToRunIndexTotalUS > maxCost {
			continue
		}
		cands = append(cands, model)
	}
	if len(cands) == 0 {
		return nil
	}
	// Cost ascending, intelligence descending.
	sort.SliceStable(cands, func(i, j int) bool {
		if *cands[i].CostToRunIndexTotalUS != *cands[j].CostToRunIndexTotalUS {
			return *cands[i].CostToRunIndexTotalUS < *cands[j].CostToRunIndexTotalUS
		}
		return *cands[i].IntelligenceIndex > *cands[j].IntelligenceIndex
	})
	out := make([]Model, 0, len(cands))
	bestIntel := -1e12
	for _, model := range cands {
		if *model.IntelligenceIndex > bestIntel {
			out = append(out, model)
			bestIntel = *model.IntelligenceIndex
		}
	}
	return out
}

func sortByWeightedComposite(models []Model, costPrimary bool) {
	if len(models) <= 1 {
		return
	}
	intelMin, intelMax := rangePtrFloat(models, func(model Model) *float64 { return model.IntelligenceIndex })
	costMin, costMax := rangePtrFloat(models, func(model Model) *float64 { return model.CostToRunIndexTotalUS })
	if intelMin == intelMax {
		intelMax = intelMin + 1
	}
	if costMin == costMax {
		costMax = costMin + 1
	}
	weightPrimary := 12.0
	weightSecondary := 1.0

	score := func(model Model) float64 {
		intelNorm := normalize(*model.IntelligenceIndex, intelMin, intelMax)
		costNorm := normalize(costMax-*model.CostToRunIndexTotalUS, 0, costMax-costMin)
		if costPrimary {
			return (weightPrimary * costNorm) + (weightSecondary * intelNorm)
		}
		return (weightPrimary * intelNorm) + (weightSecondary * costNorm)
	}

	sort.SliceStable(models, func(i, j int) bool {
		si := score(models[i])
		sj := score(models[j])
		if si == sj {
			return strings.ToLower(models[i].Slug) < strings.ToLower(models[j].Slug)
		}
		return si > sj
	})
}

func rangePtrFloat(models []Model, field func(Model) *float64) (float64, float64) {
	minV := 0.0
	maxV := 0.0
	set := false
	for _, model := range models {
		ptr := field(model)
		if ptr == nil {
			continue
		}
		if !set {
			minV = *ptr
			maxV = *ptr
			set = true
			continue
		}
		if *ptr < minV {
			minV = *ptr
		}
		if *ptr > maxV {
			maxV = *ptr
		}
	}
	if !set {
		return 0, 1
	}
	return minV, maxV
}

func normalize(value, min, max float64) float64 {
	if max <= min {
		return 1
	}
	n := (value - min) / (max - min)
	if n < 0 {
		return 0
	}
	if n > 1 {
		return 1
	}
	return n
}

func applyLimit(models []Model, limit int) []Model {
	if limit <= 0 || limit >= len(models) {
		return models
	}
	return models[:limit]
}

func descPtrFloat(left *float64, right *float64, leftTie string, rightTie string) bool {
	if left == nil && right == nil {
		return strings.ToLower(leftTie) < strings.ToLower(rightTie)
	}
	if left == nil {
		return false
	}
	if right == nil {
		return true
	}
	if *left == *right {
		return strings.ToLower(leftTie) < strings.ToLower(rightTie)
	}
	return *left > *right
}
