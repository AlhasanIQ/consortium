package runtime

import (
	"testing"
	"time"
)

func TestDefaultActivityTimeoutConfig(t *testing.T) {
	cfg := DefaultActivityTimeoutConfig()

	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	if cfg.ScheduleToStart != DefaultScheduleToStart {
		t.Errorf("ScheduleToStart = %v, want %v", cfg.ScheduleToStart, DefaultScheduleToStart)
	}

	if cfg.StartToClose != DefaultStartToClose {
		t.Errorf("StartToClose = %v, want %v", cfg.StartToClose, DefaultStartToClose)
	}

	if cfg.HeartbeatTimeout != DefaultHeartbeatTimeout {
		t.Errorf("HeartbeatTimeout = %v, want %v", cfg.HeartbeatTimeout, DefaultHeartbeatTimeout)
	}
}

func TestEffectiveStartToClose(t *testing.T) {
	tests := []struct {
		name                 string
		cfg                  *ActivityTimeoutConfig
		legacyTimeoutSeconds int
		expectedStartToClose time.Duration
	}{
		{
			name: "config provided takes precedence",
			cfg: &ActivityTimeoutConfig{
				StartToClose: 60 * time.Second,
			},
			legacyTimeoutSeconds: 30,
			expectedStartToClose: 60 * time.Second,
		},
		{
			name:                 "nil config falls back to legacy",
			cfg:                  nil,
			legacyTimeoutSeconds: 45,
			expectedStartToClose: 45 * time.Second,
		},
		{
			name: "config with zero StartToClose uses legacy",
			cfg: &ActivityTimeoutConfig{
				StartToClose: 0,
			},
			legacyTimeoutSeconds: 50,
			expectedStartToClose: 50 * time.Second,
		},
		{
			name: "config with zero StartToClose and zero legacy uses default",
			cfg: &ActivityTimeoutConfig{
				StartToClose: 0,
			},
			legacyTimeoutSeconds: 0,
			expectedStartToClose: DefaultStartToClose,
		},
		{
			name:                 "nil config and zero legacy uses default",
			cfg:                  nil,
			legacyTimeoutSeconds: 0,
			expectedStartToClose: DefaultStartToClose,
		},
		{
			name: "config takes precedence over legacy even if legacy is larger",
			cfg: &ActivityTimeoutConfig{
				StartToClose: 30 * time.Second,
			},
			legacyTimeoutSeconds: 120,
			expectedStartToClose: 30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EffectiveStartToClose(tt.cfg, tt.legacyTimeoutSeconds)
			if result != tt.expectedStartToClose {
				t.Errorf("EffectiveStartToClose() = %v, want %v", result, tt.expectedStartToClose)
			}
		})
	}
}

func TestTimeoutConfigStructs(t *testing.T) {
	// Test TimeoutConfig
	tc := TimeoutConfig{
		ExecutionTimeout: 5 * time.Minute,
		RunTimeout:       2 * time.Minute,
	}

	if tc.ExecutionTimeout != 5*time.Minute {
		t.Errorf("ExecutionTimeout = %v, want %v", tc.ExecutionTimeout, 5*time.Minute)
	}

	if tc.RunTimeout != 2*time.Minute {
		t.Errorf("RunTimeout = %v, want %v", tc.RunTimeout, 2*time.Minute)
	}

	// Test ActivityTimeoutConfig
	atc := ActivityTimeoutConfig{
		ScheduleToStart:  15 * time.Second,
		StartToClose:     90 * time.Second,
		ScheduleToClose:  120 * time.Second,
		HeartbeatTimeout: 20 * time.Second,
	}

	if atc.ScheduleToStart != 15*time.Second {
		t.Errorf("ScheduleToStart = %v, want %v", atc.ScheduleToStart, 15*time.Second)
	}

	if atc.StartToClose != 90*time.Second {
		t.Errorf("StartToClose = %v, want %v", atc.StartToClose, 90*time.Second)
	}

	if atc.ScheduleToClose != 120*time.Second {
		t.Errorf("ScheduleToClose = %v, want %v", atc.ScheduleToClose, 120*time.Second)
	}

	if atc.HeartbeatTimeout != 20*time.Second {
		t.Errorf("HeartbeatTimeout = %v, want %v", atc.HeartbeatTimeout, 20*time.Second)
	}
}

func TestDefaultTimeoutConstants(t *testing.T) {
	if DefaultScheduleToStart != 30*time.Second {
		t.Errorf("DefaultScheduleToStart = %v, want %v", DefaultScheduleToStart, 30*time.Second)
	}

	if DefaultStartToClose != 120*time.Second {
		t.Errorf("DefaultStartToClose = %v, want %v", DefaultStartToClose, 120*time.Second)
	}

	if DefaultHeartbeatTimeout != 30*time.Second {
		t.Errorf("DefaultHeartbeatTimeout = %v, want %v", DefaultHeartbeatTimeout, 30*time.Second)
	}
}
