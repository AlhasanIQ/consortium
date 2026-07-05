package optimize

import (
	"fmt"
	"strings"
)

// ValidateRunConfiguration enforces strategy-specific optimizer constraints.
func ValidateRunConfiguration(strategy string, mutatorMode string, spec *OptimizeSpec) error {
	if NormalizeOptimizeStrategy(strategy) != OptimizeStrategyDSPY {
		return nil
	}
	if err := ValidateDSPYSpec(spec); err != nil {
		return err
	}
	optimizer, err := ResolveDSPYOptimizerMode(mutatorMode, spec)
	if err != nil {
		return err
	}
	if err := validateDSPYBudgetConfig(optimizer, spec); err != nil {
		return err
	}
	if err := validateDSPYOptimizerConfig(optimizer, spec); err != nil {
		return err
	}
	return nil
}

// ValidateDSPYSpec keeps the DSPy path aligned with instruction-optimization
// semantics by requiring prompt-only optimize params.
func ValidateDSPYSpec(spec *OptimizeSpec) error {
	if spec == nil {
		return fmt.Errorf("dspy strategy requires optimize spec")
	}
	if len(spec.Params) == 0 {
		return fmt.Errorf("dspy strategy requires at least one optimize param")
	}
	promptCount := 0
	nonPrompt := make([]string, 0)
	for _, declaration := range spec.Params {
		if declaration.Type == ParamTypePrompt {
			promptCount++
			continue
		}
		nonPrompt = append(nonPrompt, fmt.Sprintf("%s (%s)", declaration.Path, declaration.Type))
	}
	if promptCount == 0 {
		return fmt.Errorf("dspy strategy requires at least one prompt param")
	}
	if len(nonPrompt) > 0 {
		return fmt.Errorf("dspy strategy supports prompt params only; found non-prompt params: %s", strings.Join(nonPrompt, ", "))
	}
	return nil
}

// ResolveDSPYOptimizerMode resolves the effective optimizer family used by the
// DSPy strategy (MIPROv2 or GEPA).
func ResolveDSPYOptimizerMode(mutatorMode string, spec *OptimizeSpec) (string, error) {
	mode := NormalizeMutatorMode(mutatorMode)
	if mode == MutatorModeAuto {
		mode = resolveDSPYOptimizerFromSpec(spec)
	}
	switch mode {
	case MutatorModeMIPROv2, MutatorModeGEPA:
		return mode, nil
	default:
		return "", fmt.Errorf("dspy strategy requires mutator_mode miprov2, gepa, or auto")
	}
}

func resolveDSPYOptimizerFromSpec(spec *OptimizeSpec) string {
	if spec != nil && spec.DSPY != nil {
		switch strings.ToLower(strings.TrimSpace(spec.DSPY.Optimizer)) {
		case MutatorModeMIPROv2, MutatorModeGEPA:
			return strings.ToLower(strings.TrimSpace(spec.DSPY.Optimizer))
		}
	}
	// Default to MIPROv2 when family is unspecified, matching DSPy's
	// "explicit optimizer class" style while still allowing a safe auto fallback.
	return MutatorModeMIPROv2
}

func validateDSPYBudgetConfig(optimizer string, spec *OptimizeSpec) error {
	if optimizer != MutatorModeGEPA || spec == nil || spec.DSPY == nil {
		return nil
	}
	cfg := spec.DSPY
	configured := 0
	if strings.TrimSpace(cfg.Auto) != "" {
		configured++
	}
	if cfg.MaxFullEvals > 0 {
		configured++
	}
	if cfg.MaxMetricCalls > 0 {
		configured++
	}
	if configured > 1 {
		return fmt.Errorf("dspy gepa budget requires at most one of auto, max_full_evals, max_metric_calls")
	}
	return nil
}

func validateDSPYOptimizerConfig(optimizer string, spec *OptimizeSpec) error {
	var cfg *DSPYConfig
	if spec != nil {
		cfg = spec.DSPY
	}
	switch optimizer {
	case MutatorModeMIPROv2:
		return validateMIPROConfig(cfg)
	case MutatorModeGEPA:
		if cfg == nil {
			return fmt.Errorf("dspy gepa config: set exactly one of auto, max_full_evals, max_metric_calls")
		}
		return validateGEPAConfig(cfg)
	default:
		return nil
	}
}

func validateMIPROConfig(cfg *DSPYConfig) error {
	if cfg == nil {
		return nil
	}
	autoSet := strings.TrimSpace(cfg.Auto) != ""
	numCandidatesSet := cfg.NumCandidates > 0
	numTrialsSet := cfg.NumTrials > 0

	if autoSet && (numCandidatesSet || numTrialsSet) {
		return fmt.Errorf("dspy miprov2 config: when auto is set, num_candidates and num_trials must be unset")
	}
	if !autoSet && (numCandidatesSet != numTrialsSet) {
		return fmt.Errorf("dspy miprov2 config: when auto is unset, num_candidates and num_trials must either both be set or both be unset")
	}
	return nil
}

func validateGEPAConfig(cfg *DSPYConfig) error {
	if cfg == nil {
		return nil
	}
	configured := 0
	if strings.TrimSpace(cfg.Auto) != "" {
		configured++
	}
	if cfg.MaxFullEvals > 0 {
		configured++
	}
	if cfg.MaxMetricCalls > 0 {
		configured++
	}
	if configured != 1 {
		return fmt.Errorf("dspy gepa config: set exactly one of auto, max_full_evals, max_metric_calls")
	}
	return nil
}
