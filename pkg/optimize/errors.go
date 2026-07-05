package optimize

import "errors"

// Sentinel errors for mutator and optimization operations.
var (
	// Parameter validation errors
	ErrNoPromptParams        = errors.New("no prompt params declared")
	ErrNoCombinatorialParams = errors.New("no combinatorial params declared")
	ErrNoMutableParams       = errors.New("no mutable params found")

	// Component selection errors
	ErrNoComponentsSelected = errors.New("gepa component selector produced no components")
	ErrNoMergedCandidate    = errors.New("no merged candidate returned")

	// Mutation strategy errors
	ErrStrategyMissingBuilder = errors.New("prompt mutation strategy requires prompt builder")

	// Request validation errors
	ErrMutationRequestRequired    = errors.New("mutation request is required")
	ErrMutationRequestInvalid     = errors.New("mutation request, parent, and spec are required")
	ErrHybridMutatorIncompatible  = errors.New("adaptive mix mutator found no compatible params in optimize spec")
	ErrHybridMutatorNotConfigured = errors.New("adaptive mix mutator has no underlying mutators configured")

	// Parameter type validation errors
	ErrMissingCandidates  = errors.New("missing candidates")
	ErrMissingRange       = errors.New("missing range")
	ErrInvalidIntRange    = errors.New("invalid int range")
	ErrMissingPromptValue = errors.New("missing prompt value")

	// Unsupported operation errors
	ErrUnsupportedCombinatorialType = errors.New("unsupported combinatorial type")

	// Loop validation errors
	ErrRunRequired         = errors.New("run is required")
	ErrMissingDependencies = errors.New("loop requires evaluator, store, and workflows")
	ErrMissingSpec         = errors.New("run optimize spec is required")
	ErrMutatorRequired     = errors.New("loop requires a mutator for non-DSPy strategies")
)
