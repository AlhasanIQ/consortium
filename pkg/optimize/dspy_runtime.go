package optimize

import (
	"math"
	"strings"
)

type dspyAutoRunSetting struct {
	N       int
	ValSize int
}

var dspyAutoRunSettings = map[string]dspyAutoRunSetting{
	"light":  {N: 6, ValSize: 100},
	"medium": {N: 12, ValSize: 300},
	"heavy":  {N: 18, ValSize: 1000},
}

type DSPYRuntimeSettings struct {
	Optimizer                  string
	Auto                       string
	ManualMode                 bool
	UseMinibatch               bool
	NumCandidates              int
	NumTrials                  int
	BatchSize                  int
	MinibatchSize              int
	MinibatchFullEvalSteps     int
	ReflectionMinibatchSize    int
	CandidateSelectionStrategy string
	ComponentSelector          string
	UseMerge                   bool
	MaxMergeInvocations        int
	MaxFullEvals               int
	MaxMetricCalls             int
}

func ResolveDSPYRuntimeSettings(spec *OptimizeSpec, optimizer string, itemLimit int) DSPYRuntimeSettings {
	runtime := DSPYRuntimeSettings{
		Optimizer:                  strings.ToLower(strings.TrimSpace(optimizer)),
		Auto:                       "",
		ManualMode:                 false,
		UseMinibatch:               true,
		NumCandidates:              0,
		NumTrials:                  0,
		MinibatchSize:              35,
		MinibatchFullEvalSteps:     5,
		ReflectionMinibatchSize:    3,
		CandidateSelectionStrategy: "pareto",
		ComponentSelector:          "round_robin",
		UseMerge:                   true,
		MaxMergeInvocations:        5,
		MaxFullEvals:               0,
		MaxMetricCalls:             0,
	}
	if runtime.Optimizer != MutatorModeMIPROv2 && runtime.Optimizer != MutatorModeGEPA {
		runtime.Optimizer = MutatorModeMIPROv2
	}

	if spec == nil || spec.DSPY == nil {
		return finalizeDSPYRuntime(runtime, spec, itemLimit)
	}
	cfg := spec.DSPY
	switch auto := strings.ToLower(strings.TrimSpace(cfg.Auto)); auto {
	case "light", "medium", "heavy":
		runtime.Auto = auto
	}
	if cfg.NumCandidates > 0 {
		runtime.NumCandidates = cfg.NumCandidates
	}
	if cfg.NumTrials > 0 {
		runtime.NumTrials = cfg.NumTrials
	}
	if cfg.BatchSize > 0 {
		runtime.BatchSize = cfg.BatchSize
	}
	if cfg.MinibatchSize > 0 {
		runtime.MinibatchSize = cfg.MinibatchSize
	}
	if cfg.MinibatchFullEvalSteps > 0 {
		runtime.MinibatchFullEvalSteps = cfg.MinibatchFullEvalSteps
	}
	if cfg.ReflectionMinibatchSize > 0 {
		runtime.ReflectionMinibatchSize = cfg.ReflectionMinibatchSize
	}
	switch strategy := strings.ToLower(strings.TrimSpace(cfg.CandidateSelection)); strategy {
	case "pareto", "current_best":
		runtime.CandidateSelectionStrategy = strategy
	}
	switch selector := strings.ToLower(strings.TrimSpace(cfg.ComponentSelector)); selector {
	case "round_robin", "all":
		runtime.ComponentSelector = selector
	}
	if cfg.UseMerge != nil {
		runtime.UseMerge = *cfg.UseMerge
	}
	if cfg.MaxMergeInvocations > 0 {
		runtime.MaxMergeInvocations = cfg.MaxMergeInvocations
	}
	if cfg.MaxFullEvals > 0 {
		runtime.MaxFullEvals = cfg.MaxFullEvals
	}
	if cfg.MaxMetricCalls > 0 {
		runtime.MaxMetricCalls = cfg.MaxMetricCalls
	}
	if runtime.Auto == "" && runtime.NumCandidates > 0 && runtime.NumTrials > 0 {
		runtime.ManualMode = true
	}
	return finalizeDSPYRuntime(runtime, spec, itemLimit)
}

func finalizeDSPYRuntime(runtime DSPYRuntimeSettings, spec *OptimizeSpec, itemLimit int) DSPYRuntimeSettings {
	numPredictors := max(dspyPromptGroupCount(spec), 1)

	numCandidates := runtime.NumCandidates
	numTrials := runtime.NumTrials
	if !runtime.ManualMode {
		if runtime.Auto == "" {
			runtime.Auto = "light"
		}
		autoSetting, ok := dspyAutoRunSettings[runtime.Auto]
		if !ok {
			autoSetting = dspyAutoRunSettings["light"]
		}
		if numCandidates <= 0 {
			numCandidates = autoSetting.N
		}
		if numTrials <= 0 {
			numVars := numPredictors * 2
			fTrials := math.Max(2*float64(numVars)*math.Log2(float64(max(numCandidates, 2))), 1.5*float64(numCandidates))
			numTrials = max(int(math.Round(fTrials)), 1)
		}
	} else {
		numCandidates = max(numCandidates, 1)
		numTrials = max(numTrials, 1)
	}
	runtime.NumCandidates = numCandidates
	runtime.NumTrials = numTrials
	if runtime.BatchSize <= 0 {
		runtime.BatchSize = 3
	}
	if runtime.BatchSize > numTrials {
		runtime.BatchSize = max(numTrials, 1)
	}
	if runtime.MinibatchSize <= 0 {
		runtime.MinibatchSize = 35
	}
	if runtime.MinibatchFullEvalSteps <= 0 {
		runtime.MinibatchFullEvalSteps = 5
	}
	if runtime.ReflectionMinibatchSize <= 0 {
		runtime.ReflectionMinibatchSize = 3
	}
	valsetSize := resolveDSPYValsetSize(runtime, itemLimit)
	if itemLimit > 0 && runtime.MinibatchSize > itemLimit {
		runtime.MinibatchSize = itemLimit
	}
	// In DSPy auto mode, minibatching is enabled only when valset size is
	// large enough (MIN_MINIBATCH_SIZE threshold).
	if runtime.Auto != "" && !runtime.ManualMode {
		runtime.UseMinibatch = valsetSize > 50
	} else {
		runtime.UseMinibatch = runtime.MinibatchSize > 0
	}
	if valsetSize > 0 && runtime.MinibatchSize >= valsetSize {
		runtime.UseMinibatch = false
	}
	if runtime.MaxMergeInvocations < 0 {
		runtime.MaxMergeInvocations = 0
	}
	if runtime.Optimizer == MutatorModeGEPA {
		runtime.MaxMetricCalls = resolveGEPAMaxMetricCalls(runtime, numPredictors, itemLimit)
	}
	return runtime
}

func resolveDSPYValsetSize(runtime DSPYRuntimeSettings, itemLimit int) int {
	if itemLimit > 0 {
		if runtime.Auto != "" {
			if autoSetting, ok := dspyAutoRunSettings[runtime.Auto]; ok && autoSetting.ValSize < itemLimit {
				return autoSetting.ValSize
			}
		}
		return itemLimit
	}
	if runtime.Auto != "" {
		if autoSetting, ok := dspyAutoRunSettings[runtime.Auto]; ok {
			return autoSetting.ValSize
		}
	}
	return 0
}

func resolveGEPAMaxMetricCalls(runtime DSPYRuntimeSettings, numPredictors int, itemLimit int) int {
	if runtime.MaxMetricCalls > 0 {
		return runtime.MaxMetricCalls
	}
	if runtime.MaxFullEvals > 0 {
		if itemLimit <= 0 {
			itemLimit = 1
		}
		return runtime.MaxFullEvals * itemLimit
	}
	autoSetting, ok := dspyAutoRunSettings[runtime.Auto]
	if !ok {
		autoSetting = dspyAutoRunSettings["light"]
	}
	// Mirrors DSPy's GEPA auto_budget approximation.
	valsetSize := resolveDSPYValsetSize(runtime, itemLimit)
	if valsetSize <= 0 {
		valsetSize = autoSetting.ValSize
	}
	minibatchSize := max(runtime.MinibatchSize, 1)
	fullEvalSteps := max(runtime.MinibatchFullEvalSteps, 1)
	numCandidates := max(autoSetting.N, runtime.NumCandidates)
	numTrials := int(math.Max(2*float64(numPredictors*2)*math.Log2(float64(max(numCandidates, 2))), 1.5*float64(numCandidates)))
	total := valsetSize
	total += numCandidates * 5
	total += numTrials * minibatchSize
	if numTrials > 0 {
		periodicFulls := (numTrials+1)/fullEvalSteps + 1
		extraFinal := 0
		if numTrials < fullEvalSteps {
			extraFinal = 1
		}
		total += (periodicFulls + extraFinal) * valsetSize
	}
	return max(total, valsetSize)
}

func dspyPromptGroupCount(spec *OptimizeSpec) int {
	if spec == nil {
		return 0
	}
	grouped := collectPromptGroups(spec)
	return len(grouped)
}
