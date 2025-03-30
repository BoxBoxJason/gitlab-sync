package utils

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
)

// TestSetGetVerbose tests setting and getting the verbose flag
func TestSetGetVerbose(t *testing.T) {
	tests := []struct {
		name     string
		verbose  bool
		expected bool
	}{
		{name: "SetVerboseTrue", verbose: true, expected: true},
		{name: "SetVerboseFalse", verbose: false, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetVerbose(tt.verbose)
			if got := getVerbose(); got != tt.expected {
				t.Errorf("getVerbose() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestLogVerbose tests logging a message if verbose logging is enabled
func TestLogVerbose(t *testing.T) {
	tests := []struct {
		name        string
		verbose     bool
		message     string
		shouldLog   bool
		expectedLog string
	}{
		{name: "VerboseEnabled", verbose: true, message: "Test message", shouldLog: true, expectedLog: "Test message"},
		{name: "VerboseDisabled", verbose: false, message: "Test message", shouldLog: false, expectedLog: ""},
	}

	var mu sync.Mutex
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetVerbose(tt.verbose)

			var buf bytes.Buffer
			logger := log.New(&buf, "", 0)
			log.SetOutput(logger.Writer())
			defer log.SetOutput(nil)

			LogVerbose(tt.message)

			mu.Lock()
			defer mu.Unlock()
			if tt.shouldLog && !strings.Contains(buf.String(), tt.expectedLog) {
				t.Errorf("LogVerbose() did not log expected message. got = %v, want %v", buf.String(), tt.expectedLog)
			}
			if !tt.shouldLog && buf.String() != tt.expectedLog {
				t.Errorf("LogVerbose() logged message when it shouldn't have. got = %v, want %v", buf.String(), tt.expectedLog)
			}
		})
	}
}

// TestLogVerbosef tests logging a formatted message if verbose logging is enabled
func TestLogVerbosef(t *testing.T) {
	tests := []struct {
		name        string
		verbose     bool
		format      string
		args        []interface{}
		shouldLog   bool
		expectedLog string
	}{
		{name: "VerboseEnabled", verbose: true, format: "Test %s", args: []interface{}{"message"}, shouldLog: true, expectedLog: "Test message"},
		{name: "VerboseDisabled", verbose: false, format: "Test %s", args: []interface{}{"message"}, shouldLog: false, expectedLog: ""},
	}

	var mu sync.Mutex
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetVerbose(tt.verbose)

			var buf bytes.Buffer
			logger := log.New(&buf, "", 0)
			log.SetOutput(logger.Writer())
			defer log.SetOutput(nil)

			LogVerbosef(tt.format, tt.args...)

			mu.Lock()
			defer mu.Unlock()
			if tt.shouldLog && !strings.Contains(buf.String(), tt.expectedLog) {
				t.Errorf("LogVerbosef() did not log expected message. got = %v, want %v", buf.String(), tt.expectedLog)
			}
			if !tt.shouldLog && buf.String() != tt.expectedLog {
				t.Errorf("LogVerbosef() logged message when it shouldn't have. got = %v, want %v", buf.String(), tt.expectedLog)
			}
		})
	}
}

// TestMergeErrors tests merging errors from a channel
func TestMergeErrors(t *testing.T) {
	tests := []struct {
		name        string
		errors      []error
		indent      int
		expectedErr string
	}{
		{name: "NoErrors", errors: nil, indent: 2, expectedErr: ""},
		{name: "SingleError", errors: []error{errors.New("error 1")}, indent: 2, expectedErr: "\n  - error 1\n"},
		{name: "MultipleErrors", errors: []error{errors.New("error 1"), errors.New("error 2")}, indent: 2, expectedErr: "\n  - error 1\n  - error 2\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errorsChan := make(chan error, len(tt.errors))
			for _, err := range tt.errors {
				errorsChan <- err
			}
			close(errorsChan)

			err := MergeErrors(errorsChan, tt.indent)
			if err != nil && err.Error() != tt.expectedErr {
				t.Errorf("MergeErrors() error = %v, want %v", err.Error(), tt.expectedErr)
			}
			if err == nil && tt.expectedErr != "" {
				t.Errorf("MergeErrors() error = nil, want %v", tt.expectedErr)
			}
		})
	}
}
