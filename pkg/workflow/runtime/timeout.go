package runtime

import "time"

// TimeoutConfig specifies timeout constraints for a workflow execution.
type TimeoutConfig struct {
	// ExecutionTimeout is the total time allowed across all runs of a logical execution.
	ExecutionTimeout time.Duration `json:"execution_timeout,omitempty"`
	// RunTimeout is the maximum time for a single run attempt.
	RunTimeout time.Duration `json:"run_timeout,omitempty"`
}

// ActivityTimeoutConfig specifies timeout constraints for a single activity.
type ActivityTimeoutConfig struct {
	// ScheduleToStart is the max time between scheduling and starting an activity.
	ScheduleToStart time.Duration `json:"schedule_to_start,omitempty"`
	// StartToClose is the max time for an activity after it starts.
	StartToClose time.Duration `json:"start_to_close,omitempty"`
	// ScheduleToClose is the combined ceiling (schedule to completion).
	ScheduleToClose time.Duration `json:"schedule_to_close,omitempty"`
	// HeartbeatTimeout is the max time between heartbeat reports.
	HeartbeatTimeout time.Duration `json:"heartbeat_timeout,omitempty"`
}

// Default timeout values.
const (
	DefaultScheduleToStart  = 30 * time.Second
	DefaultStartToClose     = 120 * time.Second
	DefaultHeartbeatTimeout = 30 * time.Second
)

// DefaultActivityTimeoutConfig returns the default activity timeout configuration.
func DefaultActivityTimeoutConfig() *ActivityTimeoutConfig {
	return &ActivityTimeoutConfig{
		ScheduleToStart:  DefaultScheduleToStart,
		StartToClose:     DefaultStartToClose,
		HeartbeatTimeout: DefaultHeartbeatTimeout,
	}
}

// EffectiveStartToClose returns the start-to-close timeout, using the default if not set.
// Also respects legacy TimeoutSeconds from the node if the config doesn't override.
func EffectiveStartToClose(cfg *ActivityTimeoutConfig, legacyTimeoutSeconds int) time.Duration {
	if cfg != nil && cfg.StartToClose > 0 {
		return cfg.StartToClose
	}
	if legacyTimeoutSeconds > 0 {
		return time.Duration(legacyTimeoutSeconds) * time.Second
	}
	return DefaultStartToClose
}
