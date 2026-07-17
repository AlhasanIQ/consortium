package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"testing"
	"time"
)

func suppressBenchmarkLogs(b *testing.B) {
	b.Helper()
	oldOutput := log.Writer()
	log.SetOutput(io.Discard)
	b.Cleanup(func() { log.SetOutput(oldOutput) })
}

func BenchmarkAppendHistoryEvent(b *testing.B) {
	suppressBenchmarkLogs(b)
	store, err := NewStorage(":memory:")
	if err != nil {
		b.Fatalf("NewStorage: %v", err)
	}
	b.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := &HistoryEvent{
			RunID:      fmt.Sprintf("run-%d", i),
			Type:       "activity_completed",
			NodeID:     "node-1",
			ActivityID: "activity-1",
			Timestamp:  time.Unix(1_700_000_000, 0),
			Attributes: map[string]interface{}{
				"output":        "benchmark output",
				"tokens_input":  20,
				"tokens_output": 10,
				"cost":          0.001,
			},
		}
		if err := store.AppendHistoryEvent(ctx, event); err != nil {
			b.Fatalf("AppendHistoryEvent: %v", err)
		}
	}
}

func BenchmarkAppendHistoryEventBatch(b *testing.B) {
	for _, size := range []int{2, 32} {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			suppressBenchmarkLogs(b)
			store, err := NewStorage(":memory:")
			if err != nil {
				b.Fatalf("NewStorage: %v", err)
			}
			b.Cleanup(func() { _ = store.Close() })

			ctx := context.Background()
			when := time.Unix(1_700_000_000, 0)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				runID := fmt.Sprintf("run-%d", i)
				events := make([]*HistoryEvent, size)
				for j := range events {
					events[j] = &HistoryEvent{
						RunID:      runID,
						Type:       "activity_completed",
						NodeID:     fmt.Sprintf("node-%d", j),
						ActivityID: fmt.Sprintf("activity-%d", j),
						Timestamp:  when,
						Attributes: map[string]interface{}{"output": "benchmark output"},
					}
				}
				if err := store.AppendHistoryEventBatch(ctx, events); err != nil {
					b.Fatalf("AppendHistoryEventBatch: %v", err)
				}
			}
		})
	}
}
