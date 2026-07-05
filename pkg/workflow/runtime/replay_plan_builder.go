package runtime

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// ReplayPlanOptions controls replay-plan generation.
type ReplayPlanOptions struct {
	// ForceDirtyNodeIDs marks specific candidate nodes as dirty regardless of DAG diff.
	ForceDirtyNodeIDs []string

	// ForceReasonByNode optionally overrides the reason for force-dirty nodes.
	ForceReasonByNode map[string]string
}

// BuildReplayPlan compares a baseline and candidate frozen DAG and computes a
// deterministic replay plan:
// - Reuse nodes: can be replay-seeded from the baseline
// - Execute nodes: must run in the candidate execution
//
// Any dirty node forces all downstream nodes to execute.
func BuildReplayPlan(base, candidate *FrozenSnapshot, opts *ReplayPlanOptions) (*ReplayPlan, error) {
	if base == nil || candidate == nil {
		return nil, fmt.Errorf("base and candidate snapshots are required")
	}

	baseWF, err := parseCanonicalSnapshot(base)
	if err != nil {
		return nil, fmt.Errorf("parse base snapshot: %w", err)
	}
	candidateWF, err := parseCanonicalSnapshot(candidate)
	if err != nil {
		return nil, fmt.Errorf("parse candidate snapshot: %w", err)
	}

	baseNodeByID := indexCanonicalNodes(baseWF.Nodes)
	candidateNodeByID := indexCanonicalNodes(candidateWF.Nodes)

	baseDeps := buildCanonicalDeps(baseWF)
	candidateDeps := buildCanonicalDeps(candidateWF)
	candidateForward := buildForwardFromDeps(candidateDeps)

	dirty := make(map[string]struct{}, len(candidateWF.Nodes))
	reasonByNode := make(map[string]string, len(candidateWF.Nodes))
	markDirty := func(nodeID, reason string) {
		nodeID = strings.TrimSpace(nodeID)
		if nodeID == "" {
			return
		}
		if _, exists := candidateNodeByID[nodeID]; !exists {
			return
		}
		if _, exists := dirty[nodeID]; exists {
			return
		}
		dirty[nodeID] = struct{}{}
		reasonByNode[nodeID] = reason
	}

	// Node-level semantic diff.
	for _, node := range candidateWF.Nodes {
		baseNode, ok := baseNodeByID[node.ID]
		if !ok {
			markDirty(node.ID, "new_node")
			continue
		}
		equal, cmpErr := canonicalNodeEqual(baseNode, node)
		if cmpErr != nil {
			return nil, fmt.Errorf("compare node %s: %w", node.ID, cmpErr)
		}
		if !equal {
			markDirty(node.ID, "node_changed")
		}
	}

	// Dependency-level diff.
	for _, node := range candidateWF.Nodes {
		if _, ok := baseNodeByID[node.ID]; !ok {
			continue
		}
		if !slices.Equal(baseDeps[node.ID], candidateDeps[node.ID]) {
			markDirty(node.ID, "dependencies_changed")
		}
	}

	// Optional force-dirty overrides.
	if opts != nil {
		for _, nodeID := range opts.ForceDirtyNodeIDs {
			reason := "forced_dirty"
			if opts.ForceReasonByNode != nil {
				if override := strings.TrimSpace(opts.ForceReasonByNode[nodeID]); override != "" {
					reason = override
				}
			}
			markDirty(nodeID, reason)
		}
	}

	// Dirty closure: any downstream node must execute.
	queue := make([]string, 0, len(dirty))
	for nodeID := range dirty {
		queue = append(queue, nodeID)
	}
	sort.Strings(queue)

	for len(queue) > 0 {
		nodeID := queue[0]
		queue = queue[1:]
		for _, childID := range candidateForward[nodeID] {
			if _, exists := dirty[childID]; exists {
				continue
			}
			dirty[childID] = struct{}{}
			reasonByNode[childID] = "upstream_dirty:" + nodeID
			queue = append(queue, childID)
		}
	}

	reuse := make([]string, 0, len(candidateWF.Nodes))
	execute := make([]string, 0, len(candidateWF.Nodes))
	dirtyNodeIDs := make([]string, 0, len(dirty))
	for _, node := range candidateWF.Nodes {
		if _, isDirty := dirty[node.ID]; isDirty {
			execute = append(execute, node.ID)
			dirtyNodeIDs = append(dirtyNodeIDs, node.ID)
			continue
		}
		if _, inBase := baseNodeByID[node.ID]; inBase {
			reuse = append(reuse, node.ID)
			continue
		}
		// Safety fallback for candidate-only nodes.
		execute = append(execute, node.ID)
		dirtyNodeIDs = append(dirtyNodeIDs, node.ID)
		if _, hasReason := reasonByNode[node.ID]; !hasReason {
			reasonByNode[node.ID] = "new_node"
		}
	}

	return &ReplayPlan{
		BaseDAGHash:      coalesceHash(base.DAGHash, base.Definition),
		CandidateDAGHash: coalesceHash(candidate.DAGHash, candidate.Definition),
		ReuseNodeIDs:     reuse,
		ExecuteNodeIDs:   execute,
		DirtyNodeIDs:     dirtyNodeIDs,
		ReasonByNode:     reasonByNode,
	}, nil
}

func parseCanonicalSnapshot(snapshot *FrozenSnapshot) (*CanonicalWorkflow, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("snapshot is nil")
	}
	if len(snapshot.Definition) == 0 {
		return nil, fmt.Errorf("snapshot definition is empty")
	}

	var wf CanonicalWorkflow
	if err := json.Unmarshal(snapshot.Definition, &wf); err != nil {
		return nil, err
	}
	return &wf, nil
}

func indexCanonicalNodes(nodes []CanonicalNode) map[string]CanonicalNode {
	out := make(map[string]CanonicalNode, len(nodes))
	for _, node := range nodes {
		if strings.TrimSpace(node.ID) == "" {
			continue
		}
		out[node.ID] = node
	}
	return out
}

func buildCanonicalDeps(wf *CanonicalWorkflow) map[string][]string {
	deps := make(map[string][]string, len(wf.Nodes))
	for _, node := range wf.Nodes {
		if strings.TrimSpace(node.ID) == "" {
			continue
		}
		deps[node.ID] = nil
	}

	for _, edge := range wf.Edges {
		source := strings.TrimSpace(edge.Source)
		target := strings.TrimSpace(edge.Target)
		if source == "" || target == "" {
			continue
		}
		if _, ok := deps[target]; !ok {
			continue
		}
		if _, ok := deps[source]; !ok {
			continue
		}
		deps[target] = append(deps[target], source)
	}

	for nodeID := range deps {
		sort.Strings(deps[nodeID])
		deps[nodeID] = dedupeStrings(deps[nodeID])
	}

	return deps
}

func buildForwardFromDeps(deps map[string][]string) map[string][]string {
	forward := make(map[string][]string, len(deps))
	for nodeID := range deps {
		forward[nodeID] = nil
	}
	for nodeID, sources := range deps {
		for _, source := range sources {
			forward[source] = append(forward[source], nodeID)
		}
	}
	for nodeID := range forward {
		sort.Strings(forward[nodeID])
		forward[nodeID] = dedupeStrings(forward[nodeID])
	}
	return forward
}

func canonicalNodeEqual(a, b CanonicalNode) (bool, error) {
	normalizedA := normalizeCanonicalNode(a)
	normalizedB := normalizeCanonicalNode(b)

	aJSON, err := json.Marshal(normalizedA)
	if err != nil {
		return false, err
	}
	bJSON, err := json.Marshal(normalizedB)
	if err != nil {
		return false, err
	}
	return string(aJSON) == string(bJSON), nil
}

func normalizeCanonicalNode(node CanonicalNode) CanonicalNode {
	if len(node.AggregationConfig) == 0 {
		node.AggregationConfig = nil
	}
	if len(node.ChildInputTemplate) == 0 {
		node.ChildInputTemplate = nil
	}
	if len(node.Metadata) == 0 {
		node.Metadata = nil
	}
	return node
}

func dedupeStrings(items []string) []string {
	if len(items) <= 1 {
		return items
	}
	out := items[:1]
	for i := 1; i < len(items); i++ {
		if items[i] == items[i-1] {
			continue
		}
		out = append(out, items[i])
	}
	return out
}

func coalesceHash(hash string, definition []byte) string {
	if strings.TrimSpace(hash) != "" {
		return hash
	}
	return ComputeDAGHash(definition)
}
