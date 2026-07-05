package optimize

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
)

type SelectionConfig struct {
	Sharpness          float64
	MidpointPercentile float64
	NoveltyWeight      float64
}

func DefaultSelectionConfig() SelectionConfig {
	return SelectionConfig{
		Sharpness:          10,
		MidpointPercentile: 75,
		NoveltyWeight:      1.0,
	}
}

func SelectParents(population []*Organism, childCounts map[string]int, k int, cfg SelectionConfig, rng *rand.Rand) ([]*Organism, error) {
	if len(population) == 0 {
		return nil, fmt.Errorf("population is empty")
	}
	if k < 1 {
		return nil, fmt.Errorf("k must be >= 1")
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(rand.Int63())) //nolint:gosec // non-cryptographic sampling
	}
	if cfg.Sharpness <= 0 {
		cfg.Sharpness = 10
	}
	if cfg.MidpointPercentile <= 0 || cfg.MidpointPercentile >= 100 {
		cfg.MidpointPercentile = 75
	}
	if cfg.NoveltyWeight < 0 {
		cfg.NoveltyWeight = 0
	}

	scores := make([]float64, 0, len(population))
	for _, organism := range population {
		score := 0.0
		if organism != nil && organism.Fitness != nil {
			score = organism.Fitness.CompositeScore
		}
		scores = append(scores, score)
	}
	midpoint := percentile(scores, cfg.MidpointPercentile)

	weights := make([]float64, len(population))
	total := 0.0
	for i, organism := range population {
		score := 0.0
		if organism != nil && organism.Fitness != nil {
			score = organism.Fitness.CompositeScore
		}
		sigmoidWeight := 1.0 / (1.0 + math.Exp(-cfg.Sharpness*(score-midpoint)))

		noveltyBonus := 1.0
		if organism != nil {
			count := childCounts[organism.ID]
			noveltyBonus = 1.0 / (1.0 + (float64(count) * cfg.NoveltyWeight))
		}

		weight := sigmoidWeight * noveltyBonus
		if weight < 1e-9 {
			weight = 1e-9
		}
		weights[i] = weight
		total += weight
	}
	if total <= 0 {
		return nil, fmt.Errorf("selection weight total is zero")
	}

	parents := make([]*Organism, 0, k)
	for range k {
		r := rng.Float64() * total
		cum := 0.0
		for i, weight := range weights {
			cum += weight
			if r <= cum {
				parents = append(parents, population[i])
				break
			}
		}
	}
	return parents, nil
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cpy := append([]float64(nil), values...)
	sort.Float64s(cpy)
	if len(cpy) == 1 {
		return cpy[0]
	}
	rank := (p / 100.0) * float64(len(cpy)-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper {
		return cpy[lower]
	}
	frac := rank - float64(lower)
	return cpy[lower] + frac*(cpy[upper]-cpy[lower])
}
