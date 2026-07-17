package durable

import (
	"context"
	"fmt"
	"io"
	"log"
	"testing"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

type benchmarkActivityHandler struct{}

func (benchmarkActivityHandler) Type() runtime.ActivityType {
	return runtime.ActivityTypeLLMCall
}

func (benchmarkActivityHandler) Execute(_ context.Context, input *runtime.ActivityInput) (*runtime.ActivityOutput, error) {
	return &runtime.ActivityOutput{
		NodeID:       input.NodeID,
		Success:      true,
		Output:       "benchmark output",
		TokensInput:  20,
		TokensOutput: 10,
		Cost:         0.001,
	}, nil
}

func benchmarkWorkflow(nodeCount int, parallel bool) *workflow.Workflow {
	nodes := make([]*workflow.Node, 0, nodeCount+2)
	edges := make([]*workflow.Edge, 0, nodeCount*2)
	newNode := func(id string) *workflow.Node {
		return &workflow.Node{
			ID:             id,
			Type:           workflow.NodeTypePrompt,
			Model:          "benchmark-model",
			Prompt:         "benchmark prompt",
			TimeoutSeconds: 30,
			RetryPolicy:    &workflow.RetryPolicy{MaxAttempts: 1},
		}
	}

	if parallel {
		nodes = append(nodes, newNode("root"))
		for i := 0; i < nodeCount; i++ {
			id := fmt.Sprintf("branch-%04d", i)
			nodes = append(nodes, newNode(id))
			edges = append(edges, &workflow.Edge{Source: "root", Target: id})
		}
		nodes = append(nodes, newNode("join"))
		for i := 0; i < nodeCount; i++ {
			edges = append(edges, &workflow.Edge{Source: fmt.Sprintf("branch-%04d", i), Target: "join"})
		}
	} else {
		for i := 0; i < nodeCount; i++ {
			nodes = append(nodes, newNode(fmt.Sprintf("node-%04d", i)))
		}
	}

	return &workflow.Workflow{
		ID:      fmt.Sprintf("benchmark-%d-%t", nodeCount, parallel),
		Name:    "Durable runtime benchmark",
		Nodes:   nodes,
		Edges:   edges,
		Context: map[string]interface{}{"input": "benchmark input"},
	}
}

func freezeBenchmarkWorkflow(b *testing.B, wf *workflow.Workflow) *runtime.FrozenSnapshot {
	b.Helper()
	nodes := make([]runtime.NodeForFreeze, len(wf.Nodes))
	for i, node := range wf.Nodes {
		nodes[i] = runtime.NodeForFreeze{
			ID:             node.ID,
			Type:           string(node.Type),
			Model:          node.Model,
			Prompt:         node.Prompt,
			TimeoutSeconds: node.TimeoutSeconds,
			RetryPolicy:    node.RetryPolicy,
		}
	}
	edges := make([]runtime.EdgeForFreeze, len(wf.Edges))
	for i, edge := range wf.Edges {
		edges[i] = runtime.EdgeForFreeze{Source: edge.Source, Target: edge.Target}
	}
	snapshot, err := runtime.FreezeWorkflow(wf.ID, wf.Name, wf.Description, nodes, edges, wf.Context, wf.Limits)
	if err != nil {
		b.Fatalf("FreezeWorkflow: %v", err)
	}
	return snapshot
}

func benchmarkDAGRuntime(b *testing.B, nodeCount int, parallel bool) {
	oldLogOutput := log.Writer()
	log.SetOutput(io.Discard)
	b.Cleanup(func() { log.SetOutput(oldLogOutput) })

	store, err := storage.NewStorage(":memory:")
	if err != nil {
		b.Fatalf("NewStorage: %v", err)
	}
	b.Cleanup(func() { _ = store.Close() })

	registry := runtime.NewActivityHandlerRegistry()
	registry.Register(benchmarkActivityHandler{})
	dagRuntime := NewDAGRuntime(store, registry)
	wf := benchmarkWorkflow(nodeCount, parallel)
	snapshot := freezeBenchmarkWorkflow(b, wf)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		jobID := fmt.Sprintf("benchmark-job-%d", i)
		b.StopTimer()
		if err := store.CreateExecution(&storage.Job{
			ID:                  jobID,
			Description:         "durable runtime benchmark",
			Model:               "workflow",
			Status:              events.JobStatusPending,
			WorkflowID:          wf.ID,
			WorkflowExecutionID: jobID,
			RunID:               jobID,
			RunNumber:           1,
			DAGSnapshot:         string(snapshot.Definition),
			DAGHash:             snapshot.DAGHash,
		}); err != nil {
			b.Fatalf("CreateExecution: %v", err)
		}
		b.StartTimer()

		if err := dagRuntime.Start(context.Background(), &runtime.StartParams{
			Identity: &runtime.ExecutionIdentity{
				WorkflowExecutionID: jobID,
				RunID:               jobID,
				RunNumber:           1,
				DAGHash:             snapshot.DAGHash,
			},
			Snapshot: snapshot,
			Workflow: wf,
		}); err != nil {
			b.Fatalf("Start: %v", err)
		}
	}
}

func BenchmarkDAGRuntimeSequential16(b *testing.B) {
	benchmarkDAGRuntime(b, 16, false)
}

func BenchmarkDAGRuntimeParallel32(b *testing.B) {
	benchmarkDAGRuntime(b, 32, true)
}

func BenchmarkSchedulerReplay1000Nodes(b *testing.B) {
	const nodeCount = 1000
	nodeIDs := make([]string, nodeCount)
	events := make([]*runtime.HistoryEvent, 0, nodeCount*3+2)
	events = append(events, &runtime.HistoryEvent{Type: runtime.HistoryWorkflowStarted})
	for i := 0; i < nodeCount; i++ {
		nodeID := fmt.Sprintf("node-%04d", i)
		activityID := fmt.Sprintf("activity-%04d", i)
		nodeIDs[i] = nodeID
		events = append(events,
			&runtime.HistoryEvent{
				Type:       runtime.HistoryScheduleActivity,
				NodeID:     nodeID,
				ActivityID: activityID,
				Attributes: map[string]interface{}{"attempt": float64(1)},
			},
			&runtime.HistoryEvent{Type: runtime.HistoryActivityStarted, NodeID: nodeID, ActivityID: activityID},
			&runtime.HistoryEvent{
				Type:       runtime.HistoryActivityCompleted,
				NodeID:     nodeID,
				ActivityID: activityID,
				Attributes: map[string]interface{}{"output": "benchmark output"},
			},
		)
	}
	events = append(events, &runtime.HistoryEvent{Type: runtime.HistoryWorkflowCompleted})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state := Replay(events, nodeIDs)
		if !state.Completed || len(state.NodeOutputs) != nodeCount {
			b.Fatal("replay produced incorrect terminal state")
		}
	}
}

func BenchmarkSchedulerDeepGraph1000Nodes(b *testing.B) {
	const nodeCount = 1000
	nodeIDs := make([]string, nodeCount)
	deps := make(map[string][]string, nodeCount)
	for i := 0; i < nodeCount; i++ {
		nodeIDs[i] = fmt.Sprintf("node-%04d", i)
		if i == 0 {
			deps[nodeIDs[i]] = nil
		} else {
			deps[nodeIDs[i]] = []string{nodeIDs[i-1]}
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state := NewSchedulerState(nodeIDs)
		readyQueue := newReadyQueue(state, deps, nodeIDs)
		for completed := 0; completed < nodeCount; completed++ {
			ready := readyQueue.ReadySet()
			if len(ready) != 1 {
				b.Fatalf("ready count = %d, want 1", len(ready))
			}
			state.Nodes[ready[0]] = NodeStateCompleted
		}
	}
}

func BenchmarkSchedulerWideGraph1000Nodes(b *testing.B) {
	const nodeCount = 1000
	nodeIDs := make([]string, nodeCount)
	deps := make(map[string][]string, nodeCount)
	for i := 0; i < nodeCount; i++ {
		nodeIDs[i] = fmt.Sprintf("node-%04d", i)
		deps[nodeIDs[i]] = nil
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state := NewSchedulerState(nodeIDs)
		ready := newReadyQueue(state, deps, nodeIDs).ReadySet()
		if len(ready) != nodeCount {
			b.Fatalf("ready count = %d, want %d", len(ready), nodeCount)
		}
	}
}
