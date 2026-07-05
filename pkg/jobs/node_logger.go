package jobs

import (
	"fmt"

	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
)

// storageNodeLogger adapts *storage.Storage to the providers.NodeLogger interface.
type storageNodeLogger struct {
	store *storage.Storage
}

// NewStorageNodeLogger wraps a *storage.Storage as a providers.NodeLogger.
func NewStorageNodeLogger(store *storage.Storage) *storageNodeLogger {
	return &storageNodeLogger{store: store}
}

func (l *storageNodeLogger) LogLLMRequestFull(log *storage.LLMRequestLog) error {
	return l.store.LogLLMRequestFull(log)
}

func (l *storageNodeLogger) AddSubNode(r providers.SubNodeRecord) error {
	node := &storage.WorkflowNode{
		ExecutionID:   r.JobID,
		RunID:         r.RunID,
		NodeID:        r.NodeID,
		NodeType:      r.NodeType,
		Status:        "completed",
		NodeLabel:     r.Label,
		NodeName:      r.Name,
		Output:        r.Output,
		LatencyMs:     r.LatencyMs,
		Metadata:      "{}",
		ExecutionUID:  fmt.Sprintf("%s:%s:1", r.JobID, r.NodeID),
		AttemptNumber: 1,
		ParentNodeID:  r.ParentNodeID,
		StartedAt:     r.StartedAt,
		CompletedAt:   r.CompletedAt,
	}
	return l.store.AddWorkflowNode(node)
}
