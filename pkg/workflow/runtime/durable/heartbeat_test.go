package durable

import (
	"context"
	"testing"
	"time"
)

func TestHeartbeatReporter_BeatAndDetails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reporter := NewHeartbeatReporter("act-1", "node-1", 100*time.Millisecond, cancel)
	initial := reporter.LastBeat()

	time.Sleep(2 * time.Millisecond)
	reporter.Beat(map[string]interface{}{"progress": "50%"})

	if !reporter.LastBeat().After(initial) {
		t.Fatalf("expected last beat to advance")
	}
	details := reporter.Details()
	if details == nil || details["progress"] != "50%" {
		t.Fatalf("unexpected heartbeat details: %#v", details)
	}

	hbCtx := ContextWithHeartbeat(ctx, reporter)
	if HeartbeatFromContext(hbCtx) != reporter {
		t.Fatal("expected heartbeat reporter in context")
	}
}

func TestMonitorHeartbeats_TimeoutCancelsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reporter := NewHeartbeatReporter("act-2", "node-2", 20*time.Millisecond, cancel)

	timedOut := make(chan struct{}, 1)
	go MonitorHeartbeats(ctx, reporter, func() { timedOut <- struct{}{} })

	select {
	case <-timedOut:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected heartbeat timeout callback")
	}

	select {
	case <-ctx.Done():
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected context cancellation after heartbeat timeout")
	}
}

func TestMonitorHeartbeats_BeatsPreventTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reporter := NewHeartbeatReporter("act-3", "node-3", 40*time.Millisecond, cancel)
	go MonitorHeartbeats(ctx, reporter, nil)

	deadline := time.Now().Add(140 * time.Millisecond)
	for time.Now().Before(deadline) {
		reporter.Beat(map[string]interface{}{"alive": true})
		time.Sleep(10 * time.Millisecond)
		if ctx.Err() != nil {
			t.Fatal("context cancelled despite regular heartbeats")
		}
	}

	reporter.Stop()
	time.Sleep(30 * time.Millisecond)
	if ctx.Err() != nil {
		t.Fatal("context should remain active after stopping monitor")
	}
}
