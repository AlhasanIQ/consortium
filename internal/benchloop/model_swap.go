package benchloop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ModelSwap captures one model field change between baseline and candidate workflows.
type ModelSwap struct {
	WorkflowID string
	Path       string
	Before     string
	After      string
}

// DetectModelSwaps compares baseline workflow snapshots vs current workflow definitions
// and returns model field changes. Baselines are read from benchmarks/loop/baselines.
func DetectModelSwaps(ctx context.Context, conctl *ConctlRunner, workdir string, workflows []string) ([]ModelSwap, error) {
	swaps := make([]ModelSwap, 0)
	var tmpPaths []string
	defer func() {
		for _, p := range tmpPaths {
			_ = os.Remove(p)
		}
	}()
	for _, wfID := range workflows {
		wfID = strings.TrimSpace(wfID)
		if wfID == "" {
			continue
		}

		baselinePath := filepath.Join(BaselinesDir(workdir), wfID+".json")
		baselineRaw, err := os.ReadFile(baselinePath)
		if err != nil {
			return nil, fmt.Errorf("read baseline %s: %w", baselinePath, err)
		}

		tmp, err := os.CreateTemp("", "benchloop-current-"+sanitizeFileID(wfID)+"-*.json")
		if err != nil {
			return nil, fmt.Errorf("create temp file for %s: %w", wfID, err)
		}
		tmpPath := tmp.Name()
		_ = tmp.Close()
		tmpPaths = append(tmpPaths, tmpPath)

		if _, err := conctl.Run(ctx, "workflows", "export", "--id", wfID, "--output", tmpPath); err != nil {
			return nil, fmt.Errorf("export current workflow %s: %w", wfID, err)
		}
		currentRaw, err := os.ReadFile(tmpPath)
		if err != nil {
			return nil, fmt.Errorf("read exported workflow %s: %w", wfID, err)
		}

		baseModels, err := collectModelValuesFromJSON(baselineRaw)
		if err != nil {
			return nil, fmt.Errorf("parse baseline models %s: %w", wfID, err)
		}
		currModels, err := collectModelValuesFromJSON(currentRaw)
		if err != nil {
			return nil, fmt.Errorf("parse current models %s: %w", wfID, err)
		}

		keysSet := make(map[string]struct{}, len(baseModels)+len(currModels))
		for k := range baseModels {
			keysSet[k] = struct{}{}
		}
		for k := range currModels {
			keysSet[k] = struct{}{}
		}
		keys := make([]string, 0, len(keysSet))
		for k := range keysSet {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, path := range keys {
			before, hasBefore := baseModels[path]
			after, hasAfter := currModels[path]
			switch {
			case hasBefore && hasAfter && before != after:
				swaps = append(swaps, ModelSwap{WorkflowID: wfID, Path: path, Before: before, After: after})
			case hasBefore && !hasAfter:
				swaps = append(swaps, ModelSwap{WorkflowID: wfID, Path: path, Before: before, After: ""})
			case !hasBefore && hasAfter:
				swaps = append(swaps, ModelSwap{WorkflowID: wfID, Path: path, Before: "", After: after})
			}
		}
	}

	return swaps, nil
}

func collectModelValuesFromJSON(raw []byte) (map[string]string, error) {
	var payload interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	out := make(map[string]string)
	collectModelValues(payload, "$", out)
	return out, nil
}

func collectModelValues(value interface{}, path string, out map[string]string) {
	switch typed := value.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			nextPath := path + "." + key
			child := typed[key]
			lower := strings.ToLower(strings.TrimSpace(key))
			if lower == "model" || strings.HasSuffix(lower, "_model") {
				if modelID, ok := child.(string); ok {
					modelID = strings.TrimSpace(modelID)
					if modelID != "" {
						out[nextPath] = modelID
					}
				}
			}
			collectModelValues(child, nextPath, out)
		}
	case []interface{}:
		for i, item := range typed {
			collectModelValues(item, fmt.Sprintf("%s[%d]", path, i), out)
		}
	}
}

func sanitizeFileID(raw string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "-")
	return replacer.Replace(strings.TrimSpace(raw))
}
