package helpers

import (
	"errors"
	"testing"
)

const (
	testErrorMsg = "test error"
)

func TestNewBlocking(t *testing.T) {
	t.Parallel()

	err := errors.New(testErrorMsg)
	mirrorErr := NewBlocking(err)
	if !errors.Is(mirrorErr.Err, err) {
		t.Errorf("Expected Err to be %v, got %v", err, mirrorErr.Err)
	}
	if mirrorErr.Severity != SeverityBlocking {
		t.Errorf("Expected Severity to be %v, got %v", SeverityBlocking, mirrorErr.Severity)
	}
}

func TestNewNonBlocking(t *testing.T) {
	t.Parallel()

	err := errors.New(testErrorMsg)
	mirrorErr := NewNonBlocking(err)
	if !errors.Is(mirrorErr.Err, err) {
		t.Errorf("Expected Err to be %v, got %v", err, mirrorErr.Err)
	}
	if mirrorErr.Severity != SeverityNonBlocking {
		t.Errorf("Expected Severity to be %v, got %v", SeverityNonBlocking, mirrorErr.Severity)
	}
}

func TestMirrorErrorError(t *testing.T) {
	t.Parallel()

	err := errors.New(testErrorMsg)
	mirrorErr := MirrorError{Err: err, Severity: SeverityBlocking}
	if mirrorErr.Error() != testErrorMsg {
		t.Errorf("Expected Error() to return '%s', got '%s'", testErrorMsg, mirrorErr.Error())
	}
}

func TestMirrorErrorUnwrap(t *testing.T) {
	t.Parallel()

	err := errors.New(testErrorMsg)
	mirrorErr := MirrorError{Err: err, Severity: SeverityBlocking}
	if !errors.Is(mirrorErr.Unwrap(), err) {
		t.Errorf("Expected Unwrap() to return %v, got %v", err, mirrorErr.Unwrap())
	}
}

func TestSeverityOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err      error
		name     string
		expected Severity
	}{
		{
			name:     "blocking error",
			err:      NewBlocking(errors.New("blocking")),
			expected: SeverityBlocking,
		},
		{
			name:     "non-blocking error",
			err:      NewNonBlocking(errors.New("non-blocking")),
			expected: SeverityNonBlocking,
		},
		{
			name:     "plain error defaults to non-blocking",
			err:      errors.New("plain error"),
			expected: SeverityNonBlocking,
		},
		{
			name:     "nil error defaults to non-blocking",
			err:      nil,
			expected: SeverityNonBlocking,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := SeverityOf(tt.err); got != tt.expected {
				t.Errorf("SeverityOf() = %v, want %v", got, tt.expected)
			}
		})
	}
}
