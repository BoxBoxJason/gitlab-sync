package helpers

import "errors"

// Severity represents the severity level of an error.
type Severity int

const (
	// SeverityNone indicates no error.
	SeverityNone Severity = iota
	// SeverityNonBlocking indicates a non-blocking error.
	SeverityNonBlocking
	// SeverityBlocking indicates a blocking error.
	SeverityBlocking
)

// MirrorError wraps an error with a severity level.
type MirrorError struct {
	Err      error
	Severity Severity
}

// Error implements the error interface.
func (e MirrorError) Error() string {
	return e.Err.Error()
}

// Unwrap returns the underlying error.
func (e MirrorError) Unwrap() error {
	return e.Err
}

// NewBlocking creates a new blocking error.
func NewBlocking(err error) MirrorError {
	return MirrorError{Err: err, Severity: SeverityBlocking}
}

// NewNonBlocking creates a new non-blocking error.
func NewNonBlocking(err error) MirrorError {
	return MirrorError{Err: err, Severity: SeverityNonBlocking}
}

// SeverityOf returns the severity of the error.
// If the error is a MirrorError, it returns its severity.
// Otherwise, it defaults to SeverityNonBlocking.
func SeverityOf(err error) Severity {
	var mirrorErr MirrorError
	if errors.As(err, &mirrorErr) {
		return mirrorErr.Severity
	}
	return SeverityNonBlocking
}
