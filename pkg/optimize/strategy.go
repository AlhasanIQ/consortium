package optimize

import "strings"

const (
	OptimizeStrategyEvolutionary = "evolutionary"
	OptimizeStrategyDarwinian    = "darwinian"
	OptimizeStrategyDSPY         = "dspy"
)

func NormalizeOptimizeStrategy(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case "", OptimizeStrategyEvolutionary, OptimizeStrategyDarwinian:
		return OptimizeStrategyEvolutionary
	case OptimizeStrategyDSPY:
		return OptimizeStrategyDSPY
	default:
		return strings.ToLower(strings.TrimSpace(strategy))
	}
}

func IsSupportedOptimizeStrategy(strategy string) bool {
	switch NormalizeOptimizeStrategy(strategy) {
	case OptimizeStrategyEvolutionary, OptimizeStrategyDSPY:
		return true
	default:
		return false
	}
}
