package check

import "fmt"

// ExecutionError represents a fatal failure while loading or running a check, as opposed to a
// lint finding. A broken check (for example a CEL expression that fails to compile or evaluate)
// must never be reported as a lint diagnostic: it invalidates the whole lint run and therefore
// fails it outright.
type ExecutionError struct {
	// CheckName is the name of the check whose execution failed. It may be empty when the
	// failure happens before a check could be registered under a name.
	CheckName string
	// ObjectName identifies the object being processed when the failure happened. It may be
	// empty for failures that are not tied to a specific object (such as compile errors).
	ObjectName string
	Err        error
}

func (e *ExecutionError) Error() string {
	switch {
	case e.CheckName != "" && e.ObjectName != "":
		return fmt.Sprintf("check %q failed on object %s: %v", e.CheckName, e.ObjectName, e.Err)
	case e.CheckName != "":
		return fmt.Sprintf("check %q failed: %v", e.CheckName, e.Err)
	default:
		return e.Err.Error()
	}
}

func (e *ExecutionError) Unwrap() error {
	return e.Err
}

// FatalErrorProvider is implemented by checks that can fail fatally (independently of the
// diagnostics they emit) while processing objects. The returned error, if any, is expected to
// wrap an ExecutionError. FatalError must be safe to call after the check has been instantiated,
// including when the check never ran (in which case it must return nil).
type FatalErrorProvider interface {
	FatalError() error
}
