package conctl

import (
	"math/rand"
	"testing"

	"github.com/alhasaniq/consortium/pkg/optimize"
)

func TestBuildOptimizeMutatorMIPROv2Mode(t *testing.T) {
	run := &optimize.OptimizationRun{
		MutatorMode: optimize.MutatorModeMIPROv2,
		ClaudeModel: "claude-model",
	}
	mutator, err := buildOptimizeMutator(run, rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatalf("buildOptimizeMutator: %v", err)
	}
	if _, ok := mutator.(*optimize.MIPROv2Mutator); !ok {
		t.Fatalf("expected MIPROv2Mutator, got %T", mutator)
	}
}

func TestBuildOptimizeMutatorGEPAMode(t *testing.T) {
	run := &optimize.OptimizationRun{
		MutatorMode: optimize.MutatorModeGEPA,
		ClaudeModel: "claude-model",
	}
	mutator, err := buildOptimizeMutator(run, rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatalf("buildOptimizeMutator: %v", err)
	}
	if _, ok := mutator.(*optimize.GEPAMutator); !ok {
		t.Fatalf("expected GEPAMutator, got %T", mutator)
	}
}

func TestBuildOptimizeMutatorAutoMode(t *testing.T) {
	promptOnly := &optimize.OptimizationRun{
		MutatorMode: optimize.MutatorModeAuto,
		Spec: &optimize.OptimizeSpec{
			Params: []optimize.ParamDeclaration{{
				Path: "nodes[a].data.config.prompt",
				Type: optimize.ParamTypePrompt,
			}},
		},
	}
	mutator, err := buildOptimizeMutator(promptOnly, rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatalf("buildOptimizeMutator(promptOnly): %v", err)
	}
	if _, ok := mutator.(*optimize.GEPAMutator); !ok {
		t.Fatalf("expected GEPAMutator for prompt-only auto mode, got %T", mutator)
	}

	combinatorialOnly := &optimize.OptimizationRun{
		MutatorMode: optimize.MutatorModeAuto,
		Spec: &optimize.OptimizeSpec{
			Params: []optimize.ParamDeclaration{{
				Path:       "nodes[a].data.config.model",
				Type:       optimize.ParamTypeModel,
				Candidates: []string{"m1", "m2"},
			}},
		},
	}
	mutator, err = buildOptimizeMutator(combinatorialOnly, rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatalf("buildOptimizeMutator(combinatorialOnly): %v", err)
	}
	if _, ok := mutator.(*optimize.CombinatorialMutator); !ok {
		t.Fatalf("expected CombinatorialMutator for combinatorial-only auto mode, got %T", mutator)
	}

	mixed := &optimize.OptimizationRun{
		MutatorMode: optimize.MutatorModeAuto,
		Spec: &optimize.OptimizeSpec{
			Params: []optimize.ParamDeclaration{
				{
					Path: "nodes[a].data.config.prompt",
					Type: optimize.ParamTypePrompt,
				},
				{
					Path:  "nodes[a].data.config.temperature",
					Type:  optimize.ParamTypeFloat,
					Range: &optimize.ParamRange{Min: 0, Max: 1, Step: 0.1},
				},
			},
		},
	}
	mutator, err = buildOptimizeMutator(mixed, rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatalf("buildOptimizeMutator(mixed): %v", err)
	}
	mixedMutator, ok := mutator.(*optimize.AdaptiveMixMutator)
	if !ok {
		t.Fatalf("expected AdaptiveMixMutator for mixed auto mode, got %T", mutator)
	}
	if _, ok := mixedMutator.LLM.(*optimize.MIPROv2Mutator); !ok {
		t.Fatalf("expected MIPROv2 prompt branch in mixed auto mode, got %T", mixedMutator.LLM)
	}
}

func TestBuildOptimizeMutatorDSPYUsesDirectOptimizer(t *testing.T) {
	run := &optimize.OptimizationRun{
		Strategy:    optimize.OptimizeStrategyDSPY,
		MutatorMode: optimize.MutatorModeMIPROv2,
		ClaudeModel: "claude-model",
		Spec: &optimize.OptimizeSpec{
			Params: []optimize.ParamDeclaration{{
				Path: "nodes[a].data.config.prompt",
				Type: optimize.ParamTypePrompt,
			}},
		},
	}
	mutator, err := buildOptimizeMutator(run, rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatalf("buildOptimizeMutator: %v", err)
	}
	if _, ok := mutator.(*optimize.MIPROv2Mutator); !ok {
		t.Fatalf("expected direct MIPROv2Mutator for dspy strategy, got %T", mutator)
	}
}

func TestBuildOptimizeMutatorDSPYRejectsLLMMode(t *testing.T) {
	run := &optimize.OptimizationRun{
		Strategy:    optimize.OptimizeStrategyDSPY,
		MutatorMode: optimize.MutatorModeLLM,
		Spec: &optimize.OptimizeSpec{
			Params: []optimize.ParamDeclaration{{
				Path: "nodes[a].data.config.prompt",
				Type: optimize.ParamTypePrompt,
			}},
		},
	}
	if _, err := buildOptimizeMutator(run, rand.New(rand.NewSource(1))); err == nil {
		t.Fatal("expected error for dspy strategy with llm mutator mode")
	}
}
