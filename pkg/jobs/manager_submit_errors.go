package jobs

import "errors"

type WorkflowSubmitValidationError struct {
	Message string
	Err     error
}

func (e *WorkflowSubmitValidationError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Message
	}
	return e.Message + ": " + e.Err.Error()
}

func (e *WorkflowSubmitValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewWorkflowSubmitValidationError(message string, err error) error {
	return &WorkflowSubmitValidationError{Message: message, Err: err}
}

func IsWorkflowSubmitValidationError(err error) bool {
	var target *WorkflowSubmitValidationError
	return errors.As(err, &target)
}
