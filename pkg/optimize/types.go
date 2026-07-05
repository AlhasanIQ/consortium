package optimize

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ParamType classifies how a parameter is mutated.
type ParamType string

const (
	ParamTypeModel    ParamType = "model"
	ParamTypePrompt   ParamType = "prompt"
	ParamTypeEnum     ParamType = "enum"
	ParamTypeFloat    ParamType = "float"
	ParamTypeInt      ParamType = "int"
	ParamTypeTopology ParamType = "topology"
)

var validParamPathPattern = regexp.MustCompile(`^nodes\[[^\]]+\]\.data\.config\.[A-Za-z0-9_.\-]+$`)

// ParamDeclaration declares a single optimizable field.
type ParamDeclaration struct {
	Path       string      `json:"path"`
	Type       ParamType   `json:"type"`
	Candidates []string    `json:"candidates,omitempty"`
	Range      *ParamRange `json:"range,omitempty"`
	Group      string      `json:"group,omitempty"`
}

type ParamRange struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Step float64 `json:"step,omitempty"`
}

type Objective struct {
	Metric    string  `json:"metric"`
	Direction string  `json:"direction"`
	Weight    float64 `json:"weight"`
}

type Constraint struct {
	Metric string  `json:"metric"`
	Op     string  `json:"op"`
	Value  float64 `json:"value"`
}

type StopPolicy struct {
	MaxGenerations     int     `json:"max_generations"`
	BudgetUSD          float64 `json:"budget_usd"`
	PlateauGenerations int     `json:"plateau_generations"`
	StabilityTopK      int     `json:"stability_top_k"`
}

type PromotionPolicy struct {
	MinAccuracyGain            float64  `json:"min_accuracy_gain"`
	RequireGeneralizationCheck bool     `json:"require_generalization_check"`
	NoRegressionOn             []string `json:"no_regression_on"`
}

type ModelSwapMetadata struct {
	Enabled         bool           `json:"enabled"`
	Track           string         `json:"track"`
	Top             int            `json:"top"`
	MinIntel        float64        `json:"min_intel,omitempty"`
	MaxCost         float64        `json:"max_cost,omitempty"`
	RepoSnapshotAt  string         `json:"repo_snapshot_at,omitempty"`
	TotalSuggested  int            `json:"total_suggested"`
	CandidateCounts map[string]int `json:"candidate_counts,omitempty"`
}

type OverlayEntry struct {
	Type     string `json:"type"`            // include|exclude|freeze|set
	Selector string `json:"selector"`        // selector expression
	Value    string `json:"value,omitempty"` // JSON value for set
}

// DSPYConfig carries DSPy-style optimizer controls for strategy=dspy runs.
type DSPYConfig struct {
	// Optimizer chooses the DSPy optimizer family when mutator_mode=auto.
	// Allowed values: miprov2, gepa.
	Optimizer string `json:"optimizer,omitempty"`
	// Auto controls run budget preset similar to DSPy auto modes.
	// Allowed values: light, medium, heavy.
	Auto string `json:"auto,omitempty"`
	// NumCandidates controls candidate prompt/demo set breadth.
	NumCandidates int `json:"num_candidates,omitempty"`
	// NumTrials controls trial count per generation in dspy strategy.
	NumTrials int `json:"num_trials,omitempty"`
	// MinibatchSize controls minibatch item limit used before periodic full eval.
	MinibatchSize int `json:"minibatch_size,omitempty"`
	// MinibatchFullEvalSteps controls periodic full-eval cadence.
	MinibatchFullEvalSteps int `json:"minibatch_full_eval_steps,omitempty"`
	// BatchSize controls how many trials are staged/verified/evaluated together.
	BatchSize int `json:"batch_size,omitempty"`

	// GEPA-specific controls.
	ReflectionMinibatchSize int    `json:"reflection_minibatch_size,omitempty"`
	CandidateSelection      string `json:"candidate_selection_strategy,omitempty"` // pareto|current_best
	ComponentSelector       string `json:"component_selector,omitempty"`           // round_robin|all
	UseMerge                *bool  `json:"use_merge,omitempty"`
	MaxMergeInvocations     int    `json:"max_merge_invocations,omitempty"`
	MaxFullEvals            int    `json:"max_full_evals,omitempty"`
	MaxMetricCalls          int    `json:"max_metric_calls,omitempty"`
}

type OptimizeSpec struct {
	Params []ParamDeclaration `json:"params"`
	Locked []string           `json:"locked"`
	// InitialValues pins startup values for selected params without mutating seed files.
	// Values are JSON-encoded literals keyed by param path.
	InitialValues map[string]string `json:"initial_values,omitempty"`
	// ModelSwap captures runtime model-suggestion metadata for this run.
	ModelSwap *ModelSwapMetadata `json:"model_swap,omitempty"`
	// Overlays records runtime CLI overlay operations applied before run creation.
	Overlays        []OverlayEntry  `json:"overlays,omitempty"`
	DSPY            *DSPYConfig     `json:"dspy,omitempty"`
	Objectives      []Objective     `json:"objectives"`
	Constraints     []Constraint    `json:"constraints,omitempty"`
	StopPolicy      StopPolicy      `json:"stop_policy"`
	PromotionPolicy PromotionPolicy `json:"promotion_policy"`
}

func (s *OptimizeSpec) Validate() error {
	if s == nil {
		return fmt.Errorf("optimize spec is required")
	}
	if len(s.Params) == 0 {
		return fmt.Errorf("optimize spec requires at least one param")
	}

	seenPaths := make(map[string]struct{}, len(s.Params))
	for i := range s.Params {
		p := s.Params[i]
		if err := p.Validate(); err != nil {
			return fmt.Errorf("param[%d]: %w", i, err)
		}
		if _, exists := seenPaths[p.Path]; exists {
			return fmt.Errorf("duplicate param path: %s", p.Path)
		}
		seenPaths[p.Path] = struct{}{}
	}

	lockedSeen := make(map[string]struct{}, len(s.Locked))
	for i, path := range s.Locked {
		path = strings.TrimSpace(path)
		if path == "" {
			return fmt.Errorf("locked[%d] path is empty", i)
		}
		if !validParamPathPattern.MatchString(path) {
			return fmt.Errorf("locked[%d] invalid path format: %s", i, path)
		}
		if _, exists := lockedSeen[path]; exists {
			return fmt.Errorf("duplicate locked path: %s", path)
		}
		lockedSeen[path] = struct{}{}
	}

	if len(s.Objectives) == 0 {
		return fmt.Errorf("optimize spec requires at least one objective")
	}
	for i := range s.Objectives {
		if err := s.Objectives[i].Validate(); err != nil {
			return fmt.Errorf("objective[%d]: %w", i, err)
		}
	}
	for i := range s.Constraints {
		if err := s.Constraints[i].Validate(); err != nil {
			return fmt.Errorf("constraint[%d]: %w", i, err)
		}
	}
	if err := s.StopPolicy.Validate(); err != nil {
		return fmt.Errorf("stop_policy: %w", err)
	}
	if err := s.PromotionPolicy.Validate(); err != nil {
		return fmt.Errorf("promotion_policy: %w", err)
	}
	if err := validateInitialValues(s); err != nil {
		return fmt.Errorf("initial_values: %w", err)
	}
	if s.DSPY != nil {
		if err := s.DSPY.Validate(); err != nil {
			return fmt.Errorf("dspy: %w", err)
		}
	}
	return nil
}

func (c *DSPYConfig) Validate() error {
	if c == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(c.Optimizer)) {
	case "", MutatorModeMIPROv2, MutatorModeGEPA:
	default:
		return fmt.Errorf("optimizer must be miprov2 or gepa")
	}
	switch strings.ToLower(strings.TrimSpace(c.Auto)) {
	case "", "light", "medium", "heavy":
	default:
		return fmt.Errorf("auto must be one of light, medium, heavy")
	}
	if c.NumCandidates < 0 {
		return fmt.Errorf("num_candidates must be >= 0")
	}
	if c.NumTrials < 0 {
		return fmt.Errorf("num_trials must be >= 0")
	}
	if c.MinibatchSize < 0 {
		return fmt.Errorf("minibatch_size must be >= 0")
	}
	if c.MinibatchFullEvalSteps < 0 {
		return fmt.Errorf("minibatch_full_eval_steps must be >= 0")
	}
	if c.ReflectionMinibatchSize < 0 {
		return fmt.Errorf("reflection_minibatch_size must be >= 0")
	}
	switch strings.ToLower(strings.TrimSpace(c.CandidateSelection)) {
	case "", "pareto", "current_best":
	default:
		return fmt.Errorf("candidate_selection_strategy must be pareto or current_best")
	}
	switch strings.ToLower(strings.TrimSpace(c.ComponentSelector)) {
	case "", "round_robin", "all":
	default:
		return fmt.Errorf("component_selector must be round_robin or all")
	}
	if c.MaxMergeInvocations < 0 {
		return fmt.Errorf("max_merge_invocations must be >= 0")
	}
	if c.MaxFullEvals < 0 {
		return fmt.Errorf("max_full_evals must be >= 0")
	}
	if c.MaxMetricCalls < 0 {
		return fmt.Errorf("max_metric_calls must be >= 0")
	}
	return nil
}

func (p ParamDeclaration) Validate() error {
	path := strings.TrimSpace(p.Path)
	if path == "" {
		return fmt.Errorf("path is required")
	}
	if !validParamPathPattern.MatchString(path) {
		return fmt.Errorf("invalid path format %q (expected nodes[<id>].data.config.<field>)", path)
	}
	if !p.Type.Valid() {
		return fmt.Errorf("unsupported type %q", p.Type)
	}

	switch p.Type {
	case ParamTypeModel, ParamTypeEnum:
		if len(p.Candidates) == 0 {
			return fmt.Errorf("candidates are required for type %q", p.Type)
		}
		seen := make(map[string]struct{}, len(p.Candidates))
		for _, candidate := range p.Candidates {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				return fmt.Errorf("candidates must be non-empty")
			}
			if _, exists := seen[candidate]; exists {
				return fmt.Errorf("duplicate candidate %q", candidate)
			}
			seen[candidate] = struct{}{}
		}
	case ParamTypeFloat, ParamTypeInt:
		if p.Range == nil {
			return fmt.Errorf("range is required for type %q", p.Type)
		}
		if err := p.Range.Validate(); err != nil {
			return err
		}
	case ParamTypePrompt:
		// Free-form text rewritten by mutator.
	case ParamTypeTopology:
		// Reserved for v2. Allowed in schema for forward compatibility.
	}
	return nil
}

func (r *ParamRange) Validate() error {
	if r == nil {
		return fmt.Errorf("range is required")
	}
	if r.Max < r.Min {
		return fmt.Errorf("max %.4f must be >= min %.4f", r.Max, r.Min)
	}
	if r.Step < 0 {
		return fmt.Errorf("step %.4f must be >= 0", r.Step)
	}
	return nil
}

func (o Objective) Validate() error {
	if strings.TrimSpace(o.Metric) == "" {
		return fmt.Errorf("metric is required")
	}
	switch o.Direction {
	case "maximize", "minimize":
	default:
		return fmt.Errorf("direction must be maximize or minimize")
	}
	if o.Weight == 0 {
		return fmt.Errorf("weight must be non-zero")
	}
	return nil
}

func (c Constraint) Validate() error {
	if strings.TrimSpace(c.Metric) == "" {
		return fmt.Errorf("metric is required")
	}
	switch c.Op {
	case "<=", ">=", "<", ">":
	default:
		return fmt.Errorf("unsupported op %q", c.Op)
	}
	return nil
}

func (p StopPolicy) Validate() error {
	if p.MaxGenerations < 1 {
		return fmt.Errorf("max_generations must be >= 1")
	}
	if p.BudgetUSD <= 0 {
		return fmt.Errorf("budget_usd must be > 0")
	}
	if p.PlateauGenerations < 0 {
		return fmt.Errorf("plateau_generations must be >= 0")
	}
	if p.StabilityTopK < 0 {
		return fmt.Errorf("stability_top_k must be >= 0")
	}
	return nil
}

func (p PromotionPolicy) Validate() error {
	if p.MinAccuracyGain < 0 {
		return fmt.Errorf("min_accuracy_gain must be >= 0")
	}
	seen := make(map[string]struct{}, len(p.NoRegressionOn))
	for _, metric := range p.NoRegressionOn {
		metric = strings.TrimSpace(metric)
		if metric == "" {
			return fmt.Errorf("no_regression_on must not include empty metric")
		}
		if _, exists := seen[metric]; exists {
			return fmt.Errorf("duplicate no_regression_on metric %q", metric)
		}
		seen[metric] = struct{}{}
	}
	return nil
}

func (t ParamType) Valid() bool {
	switch t {
	case ParamTypeModel, ParamTypePrompt, ParamTypeEnum, ParamTypeFloat, ParamTypeInt, ParamTypeTopology:
		return true
	default:
		return false
	}
}

func SortParamPaths(params []ParamDeclaration) []string {
	paths := make([]string, 0, len(params))
	for _, param := range params {
		paths = append(paths, param.Path)
	}
	sort.Strings(paths)
	return paths
}
