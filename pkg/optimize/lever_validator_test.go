package optimize

import (
	"strings"
	"testing"
)

func TestValidateParamScope(t *testing.T) {
	tests := []struct {
		name    string
		param   ParamDeclaration
		wantErr string
	}{
		{
			name:    "empty path",
			param:   ParamDeclaration{Path: ""},
			wantErr: "param path is empty",
		},
		{
			name:    "outside workflow scope",
			param:   ParamDeclaration{Path: "config.api_timeout"},
			wantErr: "outside workflow node config scope",
		},
		{
			name:    "no data.config segment",
			param:   ParamDeclaration{Path: "nodes[a].settings.model"},
			wantErr: "outside workflow node config scope",
		},
		{
			name:    "forbidden: benchmark_runner",
			param:   ParamDeclaration{Path: "nodes[a].data.config.benchmark_runner"},
			wantErr: "forbidden infra scope",
		},
		{
			name:    "forbidden: api_key",
			param:   ParamDeclaration{Path: "nodes[a].data.config.api_key"},
			wantErr: "forbidden infra scope",
		},
		{
			name:    "forbidden: rate_limit",
			param:   ParamDeclaration{Path: "nodes[a].data.config.rate_limit"},
			wantErr: "forbidden infra scope",
		},
		{
			name:    "forbidden: worker_count",
			param:   ParamDeclaration{Path: "nodes[a].data.config.worker_count"},
			wantErr: "forbidden infra scope",
		},
		{
			name:    "forbidden: admission",
			param:   ParamDeclaration{Path: "nodes[a].data.config.admission"},
			wantErr: "forbidden infra scope",
		},
		{
			name:    "forbidden: split",
			param:   ParamDeclaration{Path: "nodes[a].data.config.split"},
			wantErr: "forbidden infra scope",
		},
		{
			name:    "forbidden: dataset",
			param:   ParamDeclaration{Path: "nodes[a].data.config.dataset"},
			wantErr: "forbidden infra scope",
		},
		{
			name:  "valid workflow param",
			param: ParamDeclaration{Path: "nodes[agent-a].data.config.model"},
		},
		{
			name:  "valid nested config param",
			param: ParamDeclaration{Path: "nodes[agent-a].data.config.temperature"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateParamScope(tc.param)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestValidatePromptIsolation(t *testing.T) {
	tests := []struct {
		name       string
		workflowID string
		paramPath  string
		prompt     string
		wantErr    bool
	}{
		{
			name:       "non-reasoning workflow allowed",
			workflowID: "benchmark-test",
			paramPath:  "nodes[a].data.config.prompt",
			prompt:     "choose a/b/c/d from the options",
		},
		{
			name:       "reasoning workflow non-prompt param",
			workflowID: "reasoning-test",
			paramPath:  "nodes[a].data.config.model",
			prompt:     "choose a/b/c/d",
		},
		{
			name:       "reasoning workflow clean prompt",
			workflowID: "reasoning-test",
			paramPath:  "nodes[a].data.config.prompt",
			prompt:     "Analyze the following question carefully and reason step by step.",
		},
		{
			name:       "reasoning workflow benchmark signal choose a/b/c/d",
			workflowID: "reasoning-test",
			paramPath:  "nodes[a].data.config.prompt",
			prompt:     "You must choose a/b/c/d from the multiple choice options.",
			wantErr:    true,
		},
		{
			name:       "reasoning workflow benchmark signal single letter",
			workflowID: "reasoning-test",
			paramPath:  "nodes[a].data.config.prompt",
			prompt:     "Respond with a single letter answer.",
			wantErr:    true,
		},
		{
			name:       "reasoning workflow benchmark signal multiple-choice",
			workflowID: "reasoning-test",
			paramPath:  "nodes[a].data.config.prompt",
			prompt:     "This is a multiple-choice question.",
			wantErr:    true,
		},
		{
			name:       "case insensitive detection",
			workflowID: "Reasoning-Test",
			paramPath:  "nodes[a].data.config.systemPrompt",
			prompt:     "CHOOSE A, B, C, OR D from the given options.",
			wantErr:    true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePromptIsolation(tc.workflowID, tc.paramPath, tc.prompt)
			if tc.wantErr {
				if err == nil {
					t.Error("expected error")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
