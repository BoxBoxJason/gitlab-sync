package utils

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

const (
	ERROR_1 = "Error 1"
	ERROR_2 = "Error 2"

	EXPECT_GOT_MESSAGE     = "Expected: %q, Got: %q"
	EXPECT_NIL_GOT_MESSAGE = "Expected nil, Got: %q"
)

func TestMergeErrors(t *testing.T) {
	tests := []struct {
		name          string
		input         any
		indent        int
		expectedError string
	}{
		{
			name: "Valid channel of errors",
			input: func() chan error {
				errChan := make(chan error, 3)
				errChan <- errors.New(ERROR_1)
				errChan <- errors.New(ERROR_2)
				errChan <- nil
				close(errChan)
				return errChan
			}(),
			indent:        2,
			expectedError: "\n  - Error 1\n  - Error 2\n",
		},
		{
			name: "Unbuffered channel of errors (with goroutine)",
			input: func() chan error {
				errChan := make(chan error)
				go func() {
					defer close(errChan)
					errChan <- errors.New(ERROR_1)
					errChan <- errors.New(ERROR_2)
				}()
				return errChan
			}(),
			indent:        2,
			expectedError: "\n  - Error 1\n  - Error 2\n",
		},
		{
			name:          "Empty channel of errors",
			input:         func() chan error { ch := make(chan error); close(ch); return ch }(),
			indent:        2,
			expectedError: "",
		},
		{
			name:          "Valid slice of errors",
			input:         []error{errors.New(ERROR_1), errors.New(ERROR_2)},
			indent:        4,
			expectedError: "\n    - Error 1\n    - Error 2\n",
		},
		{
			name:          "Empty slice of errors",
			input:         []error{},
			indent:        4,
			expectedError: "",
		},
		{
			name:          "Invalid input type",
			input:         "invalid type",
			indent:        2,
			expectedError: fmt.Sprintf("invalid input type: %T", "invalid type"),
		},
		{
			name:          "Nil slice",
			input:         ([]error)(nil),
			indent:        4,
			expectedError: "",
		},
		{
			name:          "Single error in slice",
			input:         []error{errors.New("Single error")},
			indent:        3,
			expectedError: "\n   - Single error\n",
		},
		{
			name:          "High indentation level",
			input:         []error{errors.New(ERROR_1), errors.New(ERROR_2)},
			indent:        10,
			expectedError: "\n          - Error 1\n          - Error 2\n",
		},
		{
			name:          "Zero indentation level",
			input:         []error{errors.New(ERROR_1), errors.New(ERROR_2)},
			indent:        0,
			expectedError: "\n- Error 1\n- Error 2\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			done := make(chan struct{})
			var result error

			go func() {
				defer close(done)
				result = MergeErrors(tc.input, tc.indent)
			}()

			select {
			case <-done:
				if tc.expectedError == "" {
					// Expected nil result
					if result != nil {
						t.Errorf(EXPECT_NIL_GOT_MESSAGE, result)
					}
				} else {
					// Expected a specific error message
					if result == nil || result.Error() != tc.expectedError {
						t.Errorf(EXPECT_GOT_MESSAGE, tc.expectedError, result)
					}
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("Test case '%s' timed out", tc.name)
			}
		})
	}
}
