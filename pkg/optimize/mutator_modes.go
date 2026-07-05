package optimize

import "strings"

const (
	MutatorModeCombinatorial = "combinatorial"
	MutatorModeLLM           = "llm"
	MutatorModeMIPROv2       = "miprov2"
	MutatorModeGEPA          = "gepa"
	MutatorModeAuto          = "auto"
)

func NormalizeMutatorMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return MutatorModeAuto
	}
	return mode
}

func IsSupportedMutatorMode(mode string) bool {
	switch NormalizeMutatorMode(mode) {
	case MutatorModeCombinatorial, MutatorModeLLM, MutatorModeMIPROv2, MutatorModeGEPA, MutatorModeAuto:
		return true
	default:
		return false
	}
}

func ResolveAutoMutatorMode(spec *OptimizeSpec) string {
	hasPrompt := false
	hasCombinatorial := false
	if spec != nil {
		for _, declaration := range spec.Params {
			switch declaration.Type {
			case ParamTypePrompt:
				hasPrompt = true
			case ParamTypeModel, ParamTypeEnum, ParamTypeFloat, ParamTypeInt:
				hasCombinatorial = true
			}
		}
	}
	switch {
	case hasPrompt && hasCombinatorial:
		// Use a MIPROv2-style prompt operator as the default prompt branch in mixed spaces.
		return MutatorModeMIPROv2
	case hasPrompt:
		// Pure prompt space benefits more from reflective GEPA-style mutations.
		return MutatorModeGEPA
	case hasCombinatorial:
		return MutatorModeCombinatorial
	default:
		return MutatorModeAuto
	}
}
