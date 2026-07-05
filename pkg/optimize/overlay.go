package optimize

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// SpecOverlayOptions describes runtime selector-driven changes applied to an optimize spec.
type SpecOverlayOptions struct {
	IncludeSelectors []string
	ExcludeSelectors []string
	FreezeSelectors  []string
	SetOperations    []string
}

type knobSelector struct {
	Kind  string
	Value string
}

type setOperation struct {
	Selector     knobSelector
	EncodedValue string
	DecodedValue interface{}
}

// ApplySpecOverlay applies include/exclude/freeze/set overlays and returns a new spec.
// Set operations are stored on spec.InitialValues as path -> JSON-encoded value.
func ApplySpecOverlay(spec *OptimizeSpec, opts SpecOverlayOptions) (*OptimizeSpec, error) {
	if spec == nil {
		return nil, fmt.Errorf("optimize spec is required")
	}

	cloned := cloneOptimizeSpec(spec)

	include, err := parseSelectors(opts.IncludeSelectors)
	if err != nil {
		return nil, fmt.Errorf("parse include selectors: %w", err)
	}
	exclude, err := parseSelectors(opts.ExcludeSelectors)
	if err != nil {
		return nil, fmt.Errorf("parse exclude selectors: %w", err)
	}
	freeze, err := parseSelectors(opts.FreezeSelectors)
	if err != nil {
		return nil, fmt.Errorf("parse freeze selectors: %w", err)
	}
	setOps, err := parseSetOperations(opts.SetOperations)
	if err != nil {
		return nil, fmt.Errorf("parse set operations: %w", err)
	}

	if len(include) > 0 {
		for _, selector := range include {
			cloned.Overlays = append(cloned.Overlays, OverlayEntry{
				Type:     "include",
				Selector: selector.String(),
			})
		}
		filtered := make([]ParamDeclaration, 0, len(cloned.Params))
		for _, declaration := range cloned.Params {
			if matchesAnySelector(declaration, include) {
				filtered = append(filtered, declaration)
			}
		}
		cloned.Params = filtered
	}

	if len(exclude) > 0 {
		for _, selector := range exclude {
			cloned.Overlays = append(cloned.Overlays, OverlayEntry{
				Type:     "exclude",
				Selector: selector.String(),
			})
		}
		filtered := make([]ParamDeclaration, 0, len(cloned.Params))
		for _, declaration := range cloned.Params {
			if matchesAnySelector(declaration, exclude) {
				continue
			}
			filtered = append(filtered, declaration)
		}
		cloned.Params = filtered
	}

	if len(setOps) > 0 {
		if cloned.InitialValues == nil {
			cloned.InitialValues = map[string]string{}
		}
		for _, op := range setOps {
			cloned.Overlays = append(cloned.Overlays, OverlayEntry{
				Type:     "set",
				Selector: op.Selector.String(),
				Value:    op.EncodedValue,
			})
			matched := 0
			for _, declaration := range cloned.Params {
				if !op.Selector.Match(declaration) {
					continue
				}
				if err := validateParamValue(declaration, op.DecodedValue); err != nil {
					return nil, fmt.Errorf("set-knob %s: %w", declaration.Path, err)
				}
				cloned.InitialValues[declaration.Path] = op.EncodedValue
				matched++
			}
			if matched == 0 {
				return nil, fmt.Errorf("set-knob selector %q matched no remaining params", op.Selector.String())
			}
		}
	}

	if len(freeze) > 0 {
		for _, selector := range freeze {
			cloned.Overlays = append(cloned.Overlays, OverlayEntry{
				Type:     "freeze",
				Selector: selector.String(),
			})
		}
		remaining := make([]ParamDeclaration, 0, len(cloned.Params))
		lockedSet := make(map[string]struct{}, len(cloned.Locked)+len(cloned.Params))
		for _, path := range cloned.Locked {
			lockedSet[path] = struct{}{}
		}
		for _, declaration := range cloned.Params {
			if matchesAnySelector(declaration, freeze) {
				lockedSet[declaration.Path] = struct{}{}
				continue
			}
			remaining = append(remaining, declaration)
		}
		cloned.Params = remaining
		cloned.Locked = make([]string, 0, len(lockedSet))
		for path := range lockedSet {
			cloned.Locked = append(cloned.Locked, path)
		}
		sort.Strings(cloned.Locked)
	}

	if len(cloned.Params) == 0 {
		return nil, fmt.Errorf("overlay removed all optimizable params")
	}
	if err := validateInitialValues(cloned); err != nil {
		return nil, err
	}
	if err := cloned.Validate(); err != nil {
		return nil, err
	}
	return cloned, nil
}

func cloneOptimizeSpec(spec *OptimizeSpec) *OptimizeSpec {
	if spec == nil {
		return nil
	}
	cloned := *spec
	cloned.Params = append([]ParamDeclaration(nil), spec.Params...)
	cloned.Locked = append([]string(nil), spec.Locked...)
	cloned.Objectives = append([]Objective(nil), spec.Objectives...)
	cloned.Constraints = append([]Constraint(nil), spec.Constraints...)
	cloned.PromotionPolicy.NoRegressionOn = append([]string(nil), spec.PromotionPolicy.NoRegressionOn...)
	cloned.Overlays = append([]OverlayEntry(nil), spec.Overlays...)
	if spec.ModelSwap != nil {
		swap := *spec.ModelSwap
		if len(spec.ModelSwap.CandidateCounts) > 0 {
			swap.CandidateCounts = make(map[string]int, len(spec.ModelSwap.CandidateCounts))
			for path, count := range spec.ModelSwap.CandidateCounts {
				swap.CandidateCounts[path] = count
			}
		}
		cloned.ModelSwap = &swap
	}
	if len(spec.InitialValues) > 0 {
		cloned.InitialValues = make(map[string]string, len(spec.InitialValues))
		for path, value := range spec.InitialValues {
			cloned.InitialValues[path] = value
		}
	}
	return &cloned
}

func parseSelectors(raw []string) ([]knobSelector, error) {
	selectors := make([]knobSelector, 0, len(raw))
	for _, item := range raw {
		selector, err := parseSelector(item)
		if err != nil {
			return nil, err
		}
		selectors = append(selectors, selector)
	}
	return selectors, nil
}

func parseSelector(raw string) (knobSelector, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return knobSelector{}, fmt.Errorf("selector is empty")
	}
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return knobSelector{}, fmt.Errorf("selector %q must be prefixed as path:/glob:/type:/group", raw)
	}
	kind := strings.ToLower(strings.TrimSpace(parts[0]))
	value := strings.TrimSpace(parts[1])
	if value == "" {
		return knobSelector{}, fmt.Errorf("selector %q has empty value", raw)
	}
	switch kind {
	case "path", "glob", "group":
		return knobSelector{Kind: kind, Value: value}, nil
	case "type":
		paramType := ParamType(strings.ToLower(value))
		if !paramType.Valid() {
			return knobSelector{}, fmt.Errorf("selector %q has unsupported type %q", raw, value)
		}
		return knobSelector{Kind: kind, Value: string(paramType)}, nil
	default:
		return knobSelector{}, fmt.Errorf("selector %q has unsupported kind %q", raw, kind)
	}
}

func (s knobSelector) Match(declaration ParamDeclaration) bool {
	switch s.Kind {
	case "path":
		return strings.EqualFold(strings.TrimSpace(declaration.Path), strings.TrimSpace(s.Value))
	case "type":
		return strings.EqualFold(string(declaration.Type), s.Value)
	case "group":
		return strings.EqualFold(strings.TrimSpace(declaration.Group), strings.TrimSpace(s.Value))
	case "glob":
		matched, _ := globPathMatch(s.Value, declaration.Path)
		return matched
	default:
		return false
	}
}

func (s knobSelector) String() string {
	return s.Kind + ":" + s.Value
}

func matchesAnySelector(declaration ParamDeclaration, selectors []knobSelector) bool {
	for _, selector := range selectors {
		if selector.Match(declaration) {
			return true
		}
	}
	return false
}

func parseSetOperations(raw []string) ([]setOperation, error) {
	ops := make([]setOperation, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			return nil, fmt.Errorf("set-knob entry is empty")
		}
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("set-knob %q must use <selector>=<json-value>", item)
		}
		selector, err := parseSelector(parts[0])
		if err != nil {
			return nil, err
		}
		valueRaw := strings.TrimSpace(parts[1])
		if valueRaw == "" {
			return nil, fmt.Errorf("set-knob %q has empty value", item)
		}
		var decoded interface{}
		if err := json.Unmarshal([]byte(valueRaw), &decoded); err != nil {
			return nil, fmt.Errorf("set-knob %q value must be valid JSON: %w", item, err)
		}
		encoded, err := encodeJSONValue(decoded)
		if err != nil {
			return nil, fmt.Errorf("set-knob %q encode value: %w", item, err)
		}
		ops = append(ops, setOperation{
			Selector:     selector,
			DecodedValue: decoded,
			EncodedValue: encoded,
		})
	}
	return ops, nil
}

func validateInitialValues(spec *OptimizeSpec) error {
	if spec == nil || len(spec.InitialValues) == 0 {
		return nil
	}
	declByPath := make(map[string]ParamDeclaration, len(spec.Params))
	for _, declaration := range spec.Params {
		declByPath[declaration.Path] = declaration
	}
	locked := make(map[string]struct{}, len(spec.Locked))
	for _, path := range spec.Locked {
		locked[path] = struct{}{}
	}
	for path, encoded := range spec.InitialValues {
		path = strings.TrimSpace(path)
		if path == "" {
			return fmt.Errorf("initial_values contains empty path")
		}
		if !validParamPathPattern.MatchString(path) {
			return fmt.Errorf("initial_values path %q has invalid format", path)
		}
		decoded, err := decodeParamValue(encoded)
		if err != nil {
			return fmt.Errorf("initial_values path %s has invalid JSON value: %w", path, err)
		}
		if declaration, ok := declByPath[path]; ok {
			if err := validateParamValue(declaration, decoded); err != nil {
				return fmt.Errorf("initial_values path %s: %w", path, err)
			}
			continue
		}
		if _, ok := locked[path]; !ok {
			return fmt.Errorf("initial_values path %s is neither mutable nor locked", path)
		}
	}
	return nil
}

func globPathMatch(pattern string, value string) (bool, error) {
	pattern = strings.TrimSpace(pattern)
	value = strings.TrimSpace(value)
	if pattern == "" {
		return false, nil
	}
	regex, err := globPatternToRegexp(pattern)
	if err != nil {
		return false, err
	}
	return regex.MatchString(value), nil
}

func globPatternToRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch ch {
		case '*':
			// Treat both * and ** as greedy wildcard across full path.
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
			}
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
