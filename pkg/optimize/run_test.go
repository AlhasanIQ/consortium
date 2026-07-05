package optimize

import (
	"testing"
	"time"
)

func TestOptimizationRun_IsTerminal_AllStatuses(t *testing.T) {
	terminal := []string{"completed", "failed", "cancelled"}
	nonTerminal := []string{"running", "paused", "pending", ""}

	for _, s := range terminal {
		r := &OptimizationRun{Status: s}
		if !r.IsTerminal() {
			t.Errorf("IsTerminal(%q) = false, want true", s)
		}
	}
	for _, s := range nonTerminal {
		r := &OptimizationRun{Status: s}
		if r.IsTerminal() {
			t.Errorf("IsTerminal(%q) = true, want false", s)
		}
	}
}

func TestOptimizationRun_IsOwnedAndActive_EdgeCases(t *testing.T) {
	now := time.Now()
	ttl := 5 * time.Minute
	recent := now.Add(-1 * time.Minute)
	stale := now.Add(-10 * time.Minute)
	exact := now.Add(-ttl) // exactly at TTL boundary

	tests := []struct {
		name string
		run  *OptimizationRun
		want bool
	}{
		{
			name: "nil run",
			run:  nil,
			want: false,
		},
		{
			name: "no heartbeat",
			run:  &OptimizationRun{OwnerPID: 1, OwnerHostname: "host"},
			want: false,
		},
		{
			name: "zero PID",
			run:  &OptimizationRun{OwnerPID: 0, OwnerHostname: "host", LastHeartbeatAt: &recent},
			want: false,
		},
		{
			name: "empty hostname",
			run:  &OptimizationRun{OwnerPID: 1, OwnerHostname: "", LastHeartbeatAt: &recent},
			want: false,
		},
		{
			name: "active and owned",
			run:  &OptimizationRun{OwnerPID: 1234, OwnerHostname: "host", LastHeartbeatAt: &recent},
			want: true,
		},
		{
			name: "stale heartbeat",
			run:  &OptimizationRun{OwnerPID: 1234, OwnerHostname: "host", LastHeartbeatAt: &stale},
			want: false,
		},
		{
			name: "exactly at TTL boundary",
			run:  &OptimizationRun{OwnerPID: 1234, OwnerHostname: "host", LastHeartbeatAt: &exact},
			want: true, // <= ttl
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.run.IsOwnedAndActive(now, ttl)
			if got != tt.want {
				t.Errorf("IsOwnedAndActive() = %v, want %v", got, tt.want)
			}
		})
	}
}
