package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

const (
	stdinFilePermission = 0o600
	enterValue          = "Enter value"
	inputRequired       = "Input required"
	receivedInput       = "Received input"
)

func TestSetupZapLogger(t *testing.T) { //nolint:paralleltest // replaces the process-wide zap logger
	// Define the test cases in a table-driven approach.
	tests := []struct {
		name             string
		filename         string
		verbose          bool
		debugExpected    bool
		infoExpected     bool
		shouldCreateFile bool
	}{
		{
			name:             "Verbose without file",
			verbose:          true,
			filename:         "",
			debugExpected:    true,
			infoExpected:     true,
			shouldCreateFile: false,
		},
		{
			name:             "Verbose with file",
			verbose:          true,
			filename:         "test_verbose.log",
			debugExpected:    true,
			infoExpected:     true,
			shouldCreateFile: true,
		},
		{
			name:             "Non-Verbose without file",
			verbose:          false,
			filename:         "",
			debugExpected:    false,
			infoExpected:     true,
			shouldCreateFile: false,
		},
		{
			name:             "Non-Verbose with file",
			verbose:          false,
			filename:         "test_nonverbose.log",
			debugExpected:    false,
			infoExpected:     true,
			shouldCreateFile: true,
		},
	}

	for _, tc := range tests { //nolint:paralleltest // replaces the process-wide zap logger
		t.Run(tc.name, func(t *testing.T) {
			// Call the setup function with appropriate parameters.
			level := zap.InfoLevel
			if tc.verbose {
				level = zap.DebugLevel
			}

			filename := ""
			if tc.filename != "" {
				filename = filepath.Join(t.TempDir(), tc.filename)
			}

			SetupZapLogger(level, filename)

			// Check that the logger's log level for debug is set as expected.
			if zap.L().Core().Enabled(zap.DebugLevel) != tc.debugExpected {
				t.Errorf("For verbose=%v, expected DebugLevel enabled to be %v, got %v",
					tc.verbose, tc.debugExpected, zap.L().Core().Enabled(zap.DebugLevel))
			}

			// Check that info level is always enabled.
			if zap.L().Core().Enabled(zap.InfoLevel) != tc.infoExpected {
				t.Errorf("Expected InfoLevel enabled to be %v, got %v",
					tc.infoExpected, zap.L().Core().Enabled(zap.InfoLevel))
			}

			// If a filename is provided, verify that the file is created.
			if tc.shouldCreateFile && filename != "" {
				// Write a log entry to ensure that the logger flushes its output.
				zap.L().Info("test log entry")
				// Flush any buffered log entries.
				if err := zap.L().Sync(); err != nil {
					// On Windows, Sync may return an error, so we don't fail the test solely on that.
					t.Logf("Sync error (possibly expected on Windows): %v", err)
				}

				// Check if the file exists.
				if _, err := os.Stat(filename); os.IsNotExist(err) {
					t.Errorf("Expected log file %s to be created, but it does not exist", filename)
				} else if err != nil {
					t.Errorf("Error checking log file %s: %v", filename, err)
				}
			}
		})
	}
}

// replaceStdin points os.Stdin at a temporary file holding content until the test ends.
func replaceStdin(t *testing.T, content string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "stdin")

	if err := os.WriteFile(path, []byte(content), stdinFilePermission); err != nil {
		t.Fatalf("Failed to write the stdin file: %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Failed to open the stdin file: %v", err)
	}

	original := os.Stdin
	os.Stdin = file

	t.Cleanup(func() {
		os.Stdin = original

		if closeErr := file.Close(); closeErr != nil {
			t.Logf("Failed to close the stdin file: %v", closeErr)
		}
	})
}

// runCrasher re-runs the named test in a subprocess with envVar set, and requires that the
// subprocess terminates with an error without reporting a test failure (i.e. that it exited fatally).
func runCrasher(t *testing.T, testName, envVar string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(), envVar+"=1")

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("Expected subprocess to fail, but it succeeded with output: %q", output)
	}

	if bytes.Contains(output, []byte("--- FAIL")) {
		t.Fatalf("Expected subprocess to exit fatally, but the test failed instead: %s", output)
	}
}

// TestPromptForInputNonEmpty verifies that promptForInput returns the expected input
// when a single token (with surrounding spaces) is provided.
func TestPromptForInputNonEmpty(t *testing.T) { //nolint:paralleltest // replaces the process-wide os.Stdin
	// Note that fmt.Scanln reads a single token.
	replaceStdin(t, "   hello   \n")

	result := promptForInput("Enter text")
	expected := "hello"

	if result != expected {
		t.Errorf("Expected %q, but got %q", expected, result)
	}
}

// TestPromptForInputEmpty verifies that promptForInput calls zap.L().Fatal when no input token is provided.
// Since fmt.Scanln returns an error ("unexpected newline") for an empty line, the function will call Fatal,
// which results in os.Exit(1). To test this behavior without terminating the test run, we execute the test
// in a subprocess.
func TestPromptForInputEmpty(t *testing.T) { //nolint:paralleltest // replaces the process-wide os.Stdin
	// Check if we are running inside the subprocess.
	if os.Getenv("BE_CRASHER") == "1" {
		replaceStdin(t, "\n")

		// Calling promptForInput is expected to invoke zap.L().Fatal and exit.
		// Therefore, subsequent lines should never be reached.
		promptForInput("Enter text")
		t.Fatal("promptForInput returned instead of exiting")
	}

	runCrasher(t, "TestPromptForInputEmpty", "BE_CRASHER")
}

// TestPromptForMandatoryInputWithDefault tests that if a non-empty defaultValue is provided,
// the function returns it trimmed and does not prompt the user.
func TestPromptForMandatoryInputWithDefault(t *testing.T) {
	t.Parallel()

	defaultVal := "   providedValue   "
	res := promptForMandatoryInput(defaultVal, enterValue, inputRequired, receivedInput, promptOptions{})
	expected := "providedValue"

	if res != expected {
		t.Errorf("Expected %q but got %q", expected, res)
	}
}

// TestPromptForMandatoryInputWithPrompt tests that if defaultValue is empty and prompting is enabled,
// the function calls promptForInput and returns the user input trimmed.
// We simulate the user input via a temporary file assigned to os.Stdin.
func TestPromptForMandatoryInputWithPrompt(t *testing.T) { //nolint:paralleltest // replaces the process-wide os.Stdin
	// Note that promptForInput uses fmt.Scanln which requires a single token.
	replaceStdin(t, "   userInput   \n")

	res := promptForMandatoryInput("", enterValue, inputRequired, receivedInput, promptOptions{})
	expected := "userInput"

	if res != expected {
		t.Errorf("Expected %q but got %q", expected, res)
	}
}

// TestPromptForMandatoryInputEmptyPrompt tests that if prompting is enabled but the user provides empty input,
// the function logs a fatal error and exits. We use a subprocess to capture os.Exit.
func TestPromptForMandatoryInputEmptyPrompt(t *testing.T) { //nolint:paralleltest // replaces the process-wide os.Stdin
	// If BE_CRASHER_MANDATORY_EMPTY is set in the environment, this indicates we are in the subprocess.
	if os.Getenv("BE_CRASHER_MANDATORY_EMPTY") == "1" {
		// Simulate empty input.
		replaceStdin(t, "\n")

		// This call is expected to trigger zap.L().Fatal and exit the process.
		_ = promptForMandatoryInput("", enterValue, inputRequired, receivedInput, promptOptions{})
		t.Fatal("promptForMandatoryInput returned instead of exiting")
	}

	runCrasher(t, "TestPromptForMandatoryInputEmptyPrompt", "BE_CRASHER_MANDATORY_EMPTY")
}

// TestPromptForMandatoryInputPromptDisabled tests that if prompting is disabled and no defaultValue is provided,
// the function logs a fatal error and exits. Again, we use a subprocess.
func TestPromptForMandatoryInputPromptDisabled(t *testing.T) {
	t.Parallel()

	if os.Getenv("BE_CRASHER_MANDATORY_DISABLED") == "1" {
		// With prompting disabled, the function should log a fatal error immediately.
		_ = promptForMandatoryInput("", enterValue, inputRequired, receivedInput, promptOptions{disabled: true})
		t.Fatal("promptForMandatoryInput returned instead of exiting")
	}

	runCrasher(t, "TestPromptForMandatoryInputPromptDisabled", "BE_CRASHER_MANDATORY_DISABLED")
}
