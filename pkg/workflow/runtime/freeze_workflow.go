package runtime

import "github.com/alhasaniq/consortium/pkg/workflow"

// FreezeWorkflowDefinition converts a workflow.Workflow into the canonical
// frozen snapshot used by the durable runtime.
func FreezeWorkflowDefinition(wf *workflow.Workflow) (*FrozenSnapshot, error) {
	nodes := make([]NodeForFreeze, len(wf.Nodes))
	for i, n := range wf.Nodes {
		nodes[i] = NodeForFreeze{
			ID:                      n.ID,
			Type:                    string(n.Type),
			Model:                   n.Model,
			Prompt:                  n.Prompt,
			Harness:                 n.Harness,
			SystemPrompt:            n.SystemPrompt,
			Temperature:             n.Temperature,
			MaxTokens:               n.MaxTokens,
			TimeoutSeconds:          n.TimeoutSeconds,
			TaskID:                  n.TaskID,
			TaskSummary:             n.TaskSummary,
			Identity:                n.Identity,
			Image:                   n.Image,
			Sandbox:                 n.Sandbox,
			RuntimeURL:              n.RuntimeURL,
			GraceSeconds:            n.GraceSeconds,
			RepoSpecs:               n.RepoSpecs,
			WorkSource:              n.WorkSource,
			InheritFrom:             runtimeHandoffRef(n.InheritFrom),
			InheritFromNodeID:       n.InheritFromNodeID,
			InheritFromPolicy:       n.InheritFromPolicy,
			InheritFromWorkflowTask: n.InheritFromWorkflowTask,
			Condition:               n.Condition,
			OutputName:              n.OutputName,
			OperationType:           n.OperationType,
			OperationConfig:         n.OperationConfig,
			AggregationMethod:       string(n.AggregationMethod),
			AggregationConfig:       n.AggregationConfig,
			WorkflowRefID:           n.WorkflowRefID,
			InputTemplate:           n.InputTemplate,
			OutputKey:               n.OutputKey,
			ChildWorkflowID:         n.ChildWorkflowID,
			ChildInputTemplate:      n.ChildInputTemplate,
			ChildAwait:              n.ChildAwait,
			ChildOutputKey:          n.ChildOutputKey,
			Metadata:                n.Metadata,
		}
	}

	edges := make([]EdgeForFreeze, len(wf.Edges))
	for i, e := range wf.Edges {
		edges[i] = EdgeForFreeze{
			Source: e.Source,
			Target: e.Target,
		}
	}

	return FreezeWorkflow(
		wf.ID, wf.Name, wf.Description,
		nodes, edges,
		wf.Context, wf.Limits,
	)
}

func runtimeHandoffRef(ref *workflow.NovomoHandoffRef) *CanonicalNovomoHandoffRef {
	normalized := workflow.NormalizeNovomoHandoff(ref)
	if normalized == nil {
		return nil
	}
	return &CanonicalNovomoHandoffRef{
		Kind:   normalized.Kind,
		ID:     normalized.ID,
		Policy: normalized.Policy,
	}
}
