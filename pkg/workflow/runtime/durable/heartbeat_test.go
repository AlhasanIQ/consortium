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

	reporter.Beat(map[string]interface{}{"progress": "50%"})

	if reporter.LastBeat().Before(initial) {
		t.Fatalf("heartbeat timestamp moved backwards")
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
	case <-time.After(time.Second):
		t.Fatal("expected heartbeat timeout callback")
	}

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected context cancellation after heartbeat timeout")
	}
}

func TestMonitorHeartbeats_BeatsPreventTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reporter := NewHeartbeatReporter("act-3", "node-3", 40*time.Millisecond, cancel)
	timedOut := make(chan struct{}, 1)
	go MonitorHeartbeats(ctx, reporter, func() { timedOut <- struct{}{} })

	beats := time.NewTicker(5 * time.Millisecond)
	defer beats.Stop()
	deadline := time.NewTimer(5 * reporter.timeout)
	defer deadline.Stop()
	for {
		select {
		case <-timedOut:
			t.Fatal("context cancelled despite regular heartbeats")
		case <-beats.C:
			reporter.Beat(map[string]interface{}{"alive": true})
		case <-deadline.C:
			reporter.Stop()
			if ctx.Err() != nil {
				t.Fatal("context cancelled despite regular heartbeats")
			}
			return
		}
	}
}

func TestMonitorHeartbeats_StopPreventsTimeoutCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reporter := NewHeartbeatReporter("act-4", "node-4", 10*time.Millisecond, cancel)
	reporter.Stop()
	timedOut := make(chan struct{}, 1)
	go MonitorHeartbeats(ctx, reporter, func() { timedOut <- struct{}{} })

	select {
	case <-timedOut:
		t.Fatal("stopped reporter must not trigger a heartbeat timeout")
	case <-time.After(100 * time.Millisecond):
		if ctx.Err() != nil {
			t.Fatal("context should remain active after stopping monitor")
		}
	}
}
