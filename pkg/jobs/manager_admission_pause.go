package jobs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
)

const (
	admissionPauseCodeManualPause = "MANUAL_PAUSE"
)

// AdmissionPauseReason captures why root-workflow admission is paused.
type AdmissionPauseReason struct {
	Code         string    `json:"code"`
	Reason       string    `json:"reason"`
	Message      string    `json:"message"`
	TriggerJobID string    `json:"trigger_job_id,omitempty"`
	TriggeredAt  time.Time `json:"triggered_at"`
}

// AdmissionPausedError indicates root submission is paused until operator resume.
type AdmissionPausedError struct {
	Reason AdmissionPauseReason
}

func (e *AdmissionPausedError) Error() string {
	msg := strings.TrimSpace(e.Reason.Message)
	if msg == "" {
		msg = "admission paused due to terminal failure"
	}
	code := strings.TrimSpace(e.Reason.Code)
	if code == "" {
		return msg
	}
	return fmt.Sprintf("admission paused (%s): %s", code, msg)
}

// IsAdmissionPausedError reports whether err indicates admission is paused.
func IsAdmissionPausedError(err error) bool {
	var pausedErr *AdmissionPausedError
	return errors.As(err, &pausedErr)
}

// AdmissionPauseFromError extracts pause metadata from an admission-paused error.
func AdmissionPauseFromError(err error) (*AdmissionPauseReason, bool) {
	var pausedErr *AdmissionPausedError
	if !errors.As(err, &pausedErr) || pausedErr == nil {
		return nil, false
	}
	reasonCopy := pausedErr.Reason
	return &reasonCopy, true
}

// AdmissionState returns whether root submissions are paused and the pause reason.
func (m *Manager) AdmissionState() (bool, *AdmissionPauseReason) {
	paused := m.admissionPaused.Load()
	if !paused {
		return false, nil
	}
	m.admissionMu.RLock()
	defer m.admissionMu.RUnlock()
	if m.admissionReason == nil {
		return true, nil
	}
	reasonCopy := *m.admissionReason
	return true, &reasonCopy
}

func (m *Manager) manualPauseReason(message string) AdmissionPauseReason {
	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = "Admission paused by operator request"
	}
	return AdmissionPauseReason{
		Code:        admissionPauseCodeManualPause,
		Reason:      "manual_pause",
		Message:     msg,
		TriggeredAt: time.Now(),
	}
}

func (m *Manager) pauseAdmission(reason AdmissionPauseReason) bool {
	if reason.TriggeredAt.IsZero() {
		reason.TriggeredAt = time.Now()
	}
	if strings.TrimSpace(reason.Message) == "" {
		reason.Message = "admission paused due to terminal failure"
	}
	if !m.admissionPaused.CompareAndSwap(false, true) {
		return false
	}

	m.admissionMu.Lock()
	defer m.admissionMu.Unlock()
	reasonCopy := reason
	m.admissionReason = &reasonCopy
	return true
}

func (m *Manager) clearAdmissionPause() {
	m.admissionMu.Lock()
	m.admissionReason = nil
	m.admissionMu.Unlock()
	m.admissionPaused.Store(false)
}

// PauseAdmission pauses root admission with a manual/operator reason.
// Returns true only when this call transitions the gate from accepting -> paused.
func (m *Manager) PauseAdmission(message string) bool {
	return m.pauseAdmission(m.manualPauseReason(message))
}

// ResumeAdmission re-opens root admission.
// Returns true only when this call transitions paused -> accepting.
func (m *Manager) ResumeAdmission() bool {
	if !m.admissionPaused.Load() {
		return false
	}
	m.clearAdmissionPause()
	return true
}

func (m *Manager) maybePauseAdmissionForFailure(jobID, errorCode, errorMessage string) {
	if m == nil || m.config == nil || !m.config.PauseAdmissionOnTerminalFailure {
		return
	}

	reason, shouldPause := classifyAdmissionPauseReason(errorCode, errorMessage)
	if !shouldPause {
		return
	}
	reason.TriggerJobID = strings.TrimSpace(jobID)

	if !m.pauseAdmission(reason) {
		return
	}

	// Admission pause blocks new root submissions. Queue roots are moved to paused
	// for explicit operator control; running jobs are left untouched.
	pausedCount, err := m.storage.TransitionPendingRootExecutions(
		context.Background(),
		events.JobStatusPaused,
		nil,
	)
	if err != nil {
		log.Printf("⚠️ [AdmissionPause] failed to pause pending root jobs: %v", err)
	} else {
		log.Printf(
			"⏸️ [AdmissionPause] root admission paused code=%s reason=%s trigger_job=%s paused_roots=%d message=%s",
			reason.Code,
			reason.Reason,
			reason.TriggerJobID,
			pausedCount,
			reason.Message,
		)
	}
}

// ClassifyAdmissionPauseReason classifies failures that should trigger admission pause.
// Current systemic classes are auth/access-denied and insufficient credits.
func ClassifyAdmissionPauseReason(errorCode, errorMessage string) (AdmissionPauseReason, bool) {
	return classifyAdmissionPauseReason(errorCode, errorMessage)
}

func classifyAdmissionPauseReason(errorCode, errorMessage string) (AdmissionPauseReason, bool) {
	code := strings.ToUpper(strings.TrimSpace(errorCode))
	message := strings.TrimSpace(errorMessage)

	if reason, ok := admissionPauseReasonForCode(code); ok {
		return AdmissionPauseReason{
			Code:    code,
			Reason:  reason,
			Message: defaultPauseMessage(message, reason),
		}, true
	}

	if inferredCode, reason, ok := admissionPauseReasonForMessage(message); ok {
		if code == "" {
			code = inferredCode
		}
		return AdmissionPauseReason{
			Code:    code,
			Reason:  reason,
			Message: defaultPauseMessage(message, reason),
		}, true
	}

	return AdmissionPauseReason{}, false
}

func admissionPauseReasonForCode(code string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "INSUFFICIENT_CREDITS":
		return "insufficient_credits", true
	case "AUTH_ERROR", "AUTHENTICATION", "AUTHORIZATION":
		return "auth_or_access_denied", true
	default:
		return "", false
	}
}

func admissionPauseReasonForMessage(errMsg string) (code string, reason string, ok bool) {
	lower := strings.ToLower(strings.TrimSpace(errMsg))
	if lower == "" {
		return "", "", false
	}

	for _, marker := range []string{
		"insufficient credits",
		"payment required",
		"no credits left",
		"insufficient balance",
		"quota exhausted",
	} {
		if strings.Contains(lower, marker) {
			return "INSUFFICIENT_CREDITS", "insufficient_credits", true
		}
	}

	for _, marker := range []string{
		"invalid api key",
		"authentication failed",
		"auth error",
		"unauthorized",
		"forbidden",
		"access denied",
		"not authorized",
		"account suspended",
		"account disabled",
		"country, region, or territory",
		"unsupported country",
		"geo restriction",
		"geographical restriction",
	} {
		if strings.Contains(lower, marker) {
			return "AUTH_ERROR", "auth_or_access_denied", true
		}
	}

	return "", "", false
}

func defaultPauseMessage(message, reason string) string {
	trimmed := strings.TrimSpace(message)
	if trimmed != "" {
		return trimmed
	}
	switch reason {
	case "insufficient_credits":
		return "Provider credits exhausted; admission paused"
	case "auth_or_access_denied":
		return "Provider authentication/access denied; admission paused"
	default:
		return "admission paused due to terminal failure"
	}
}
