package jobs

import (
	"context"
	"fmt"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/compiler"
	workflowruntime "github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

type workflowDefinitionResolver struct {
	store *storage.Storage
}

func NewWorkflowDefinitionResolver(store *storage.Storage) compiler.Resolver {
	return workflowDefinitionResolver{store: store}
}

func (r workflowDefinitionResolver) ResolveWorkflow(ctx context.Context, id string) (*workflow.Workflow, string, error) {
	select {
	case <-ctx.Done():
		return nil, "", ctx.Err()
	default:
	}
	if r.store == nil {
		return nil, "", fmt.Errorf("workflow reference %q: storage is not configured", id)
	}

	definition, err := r.store.GetWorkflow(id)
	if err != nil {
		return nil, "", fmt.Errorf("workflow reference %q: %w", id, err)
	}

	wf, err := WorkflowFromDefinition(definition.Definition, nil)
	if err != nil {
		return nil, "", fmt.Errorf("workflow reference %q: parse stored definition: %w", id, err)
	}
	if wf.ID == "" {
		wf.ID = definition.ID
	}

	snapshot, err := workflowruntime.FreezeWorkflowDefinition(wf)
	if err != nil {
		return nil, "", fmt.Errorf("workflow reference %q: hash stored definition: %w", id, err)
	}
	return wf, snapshot.DAGHash, nil
}
