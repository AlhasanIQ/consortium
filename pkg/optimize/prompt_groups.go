package optimize

import (
	"sort"
	"strings"
)

type promptParamGroup struct {
	Key          string
	Declarations []ParamDeclaration
}

func collectPromptGroups(spec *OptimizeSpec) []promptParamGroup {
	if spec == nil || len(spec.Params) == 0 {
		return nil
	}
	groups := map[string][]ParamDeclaration{}
	order := make([]string, 0)
	for _, declaration := range spec.Params {
		if declaration.Type != ParamTypePrompt {
			continue
		}
		key := strings.TrimSpace(declaration.Group)
		if key == "" {
			key = declaration.Path
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], declaration)
	}
	if len(order) == 0 {
		return nil
	}
	sort.Strings(order)
	out := make([]promptParamGroup, 0, len(order))
	for _, key := range order {
		declarations := groups[key]
		sort.Slice(declarations, func(i, j int) bool {
			return declarations[i].Path < declarations[j].Path
		})
		out = append(out, promptParamGroup{
			Key:          key,
			Declarations: declarations,
		})
	}
	return out
}
