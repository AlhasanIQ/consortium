package workflow

import (
	"errors"
	"testing"
)

func TestPeerMatrixCertifiedEarlyStopConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      map[string]interface{}
		wantEnabled bool
		wantErr     bool
	}{
		{name: "missing is disabled", config: map[string]interface{}{}, wantEnabled: false},
		{name: "explicit false", config: map[string]interface{}{"certified_early_stop": false}, wantEnabled: false},
		{name: "explicit true", config: map[string]interface{}{"certified_early_stop": true}, wantEnabled: true},
		{name: "string true fails closed", config: map[string]interface{}{"certified_early_stop": "true"}, wantErr: true},
		{name: "numeric flag fails closed", config: map[string]interface{}{"certified_early_stop": 1}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := peerMatrixCertifiedEarlyStop(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrAggregationConfig) {
				t.Fatalf("error = %v, want ErrAggregationConfig", err)
			}
			if got != tt.wantEnabled {
				t.Fatalf("enabled = %v, want %v", got, tt.wantEnabled)
			}
		})
	}
}

func TestValidateAggregationConfigRejectsNonBooleanCertifiedFlag(t *testing.T) {
	config := map[string]interface{}{
		"eval_system_prompt":   "system",
		"eval_prompt":          "evaluate {{rubric}}",
		"normalization":        "none",
		"temperature":          0.0,
		"max_tokens":           100,
		"max_parallel":         2,
		"rubric":               []RubricCriterion{{Name: "quality", Weight: 1}},
		"certified_early_stop": "true",
	}

	err := ValidateAggregationConfig(AggMethodPeerMatrix, config)
	if err == nil {
		t.Fatal("expected non-boolean certified_early_stop to be rejected")
	}
	if !errors.Is(err, ErrAggregationConfig) {
		t.Fatalf("error = %v, want ErrAggregationConfig", err)
	}
}
