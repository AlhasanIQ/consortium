package workflow

import (
	"fmt"
	"strings"
)

const (
	NovomoHandoffKindJobRun  = "job_run"
	NovomoHandoffKindJob     = "job"
	NovomoHandoffKindNovoRun = "novo_run"
	NovomoHandoffKindTask    = "task"
)

// NormalizeNovomoHandoff trims fields and returns nil when no handle is
// configured. It deliberately does not validate policy names because Novomo
// owns policy semantics.
func NormalizeNovomoHandoff(ref *NovomoHandoffRef) *NovomoHandoffRef {
	if ref == nil {
		return nil
	}
	out := &NovomoHandoffRef{
		Kind:   strings.TrimSpace(ref.Kind),
		ID:     strings.TrimSpace(ref.ID),
		Policy: strings.TrimSpace(ref.Policy),
	}
	if out.Kind == "" && out.ID == "" && out.Policy == "" {
		return nil
	}
	return out
}

func ValidateNovomoHandoffRef(field string, ref *NovomoHandoffRef) error {
	ref = NormalizeNovomoHandoff(ref)
	if ref == nil {
		return nil
	}
	if !IsSupportedNovomoHandoffKind(ref.Kind) {
		return fmt.Errorf("%s.kind %q is not supported", field, ref.Kind)
	}
	if ref.ID == "" {
		return fmt.Errorf("%s.id is required", field)
	}
	if strings.ContainsAny(ref.ID, "\r\n\t") {
		return fmt.Errorf("%s.id contains invalid whitespace", field)
	}
	if len(ref.ID) > 128 {
		return fmt.Errorf("%s.id exceeds 128 characters", field)
	}
	if strings.ContainsAny(ref.Policy, "\r\n\t") {
		return fmt.Errorf("%s.policy contains invalid whitespace", field)
	}
	if len(ref.Policy) > 64 {
		return fmt.Errorf("%s.policy exceeds 64 characters", field)
	}
	return nil
}

func IsSupportedNovomoHandoffKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case NovomoHandoffKindJobRun, NovomoHandoffKindJob, NovomoHandoffKindNovoRun, NovomoHandoffKindTask:
		return true
	default:
		return false
	}
}
