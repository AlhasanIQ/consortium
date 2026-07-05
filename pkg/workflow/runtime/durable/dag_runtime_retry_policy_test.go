package durable

import (
	"testing"

	"github.com/alhasaniq/consortium/pkg/workflow"
)

func TestResolveNodeRetryPolicy_RequiresExplicitPolicy(t *testing.T) {
	tests := []struct {
		name string
		node *workflow.Node
	}{
		{
			name: "prompt without retry policy",
			node: &workflow.Node{Type: workflow.NodeTypePrompt},
		},
		{
			name: "child workflow without retry policy",
			node: &workflow.Node{Type: workflow.NodeTypeChildWorkflow},
		},
		{
			name: "contract extract without retry policy",
			node: &workflow.Node{Type: workflow.NodeTypeContractExtract},
		},
		{
			name: "result without retry policy",
			node: &workflow.Node{Type: workflow.NodeTypeResult},
		},
		{
			name: "conditional without retry policy",
			node: &workflow.Node{Type: workflow.NodeTypeConditional},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveNodeRetryPolicy(tt.node)
			if got != nil {
				t.Fatalf("expected nil retry policy when node has none, got %+v", got)
			}
		})
	}
}

func TestResolveNodeRetryPolicy_ExplicitPolicyPreserved(t *testing.T) {
	explicit := &workflow.RetryPolicy{
		MaxAttempts:     2,
		BackoffMs:       250,
		BackoffMultiply: 1.5,
		MaxBackoffMs:    2000,
		RetryableErrors: []string{"TEMPORARY"},
	}
	got := resolveNodeRetryPolicy(&workflow.Node{
		Type:        workflow.NodeTypeContractExtract,
		RetryPolicy: explicit,
	})
	if got != explicit {
		t.Fatalf("expected explicit retry policy pointer to be preserved")
	}
}
