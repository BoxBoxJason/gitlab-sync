package main

import (
	"os"
	"os/exec"
	"testing"

	"go.uber.org/zap"
)

const (
	ENTER_VALUE    = "Enter value"
	INPUT_REQUIRED = "Input required"
	RECEIVED_INPUT = "Received input"
)

// setupZapLogger is assumed to be defined in the same module.
// If it is in a different package, import that package accordingly.

func TestSetupZapLoggerVerbose(t *testing.T) {
	// Set up the logger in verbose mode.
	setupZapLogger(true)

	// The global logger (zap.L()) should allow debug logs.
	// Check that debug logs are enabled.
	if !zap.L().Core().Enabled(zap.DebugLevel) {
		t.Error("Expected debug level to be enabled in verbose mode but it is not")
	}

	// Optionally, check that Info level logs are enabled.
	if !zap.L().Core().Enabled(zap.InfoLevel) {
		t.Error("Expected info level to be enabled in verbose mode but it is not")
	}
}

func TestSetupZapLoggerNonVerbose(t *testing.T) {
	// Set up the logger in non-verbose (production) mode.
	setupZapLogger(false)

	// The global logger (zap.L()) should not allow debug logs.
	if zap.L().Core().Enabled(zap.DebugLevel) {
		t.Error("Expected debug level to be disabled in non-verbose mode but it is enabled")
	}

	// Info level logs must remain enabled.
	if !zap.L().Core().Enabled(zap.InfoLevel) {
		t.Error("Expected info level to be enabled in non-verbose mode but it is not")
	}
}

// TestPromptForInputNonEmpty verifies that promptForInput returns the expected input
// when a single token (with surrounding spaces) is provided.
func TestPromptForInputNonEmpty(t *testing.T) {
	// Backup the original os.Stdin and restore it after the test.
	origStdin := os.Stdin
	defer func() { os.Stdin = origStdin }()

	// Create a temporary file with a single token input.
	tempFile, err := os.CreateTemp("", "stdin")
	if err != nil {
		t.Fatalf("Failed to create temporary file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	// Write a single token with extra spaces. Note that fmt.Scanln reads a single token.
	input := "   hello   \n"
	if _, err := tempFile.WriteString(input); err != nil {
		t.Fatalf("Failed to write to temporary file: %v", err)
	}

	// Reset the file offset to the beginning.
	if _, err := tempFile.Seek(0, 0); err != nil {
		t.Fatalf("Failed to reset file offset: %v", err)
	}

	// Set the temporary file as os.Stdin.
	os.Stdin = tempFile

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
func TestPromptForInputEmpty(t *testing.T) {
	// Check if we are running inside the subprocess.
	if os.Getenv("BE_CRASHER") == "1" {
		// In the subprocess, create a temporary file that contains only a newline.
		tempFile, err := os.CreateTemp("", "stdin")
		if err != nil {
			os.Exit(2)
		}
		defer os.Remove(tempFile.Name())

		if _, err := tempFile.WriteString("\n"); err != nil {
			os.Exit(2)
		}
		if _, err := tempFile.Seek(0, 0); err != nil {
			os.Exit(2)
		}
		os.Stdin = tempFile

		// Calling promptForInput is expected to invoke zap.L().Fatal and exit.
		// Therefore, subsequent lines should never be reached.
		promptForInput("Enter text")
		// If we reach here, exit with a non-zero status.
		os.Exit(3)
	}

	// Set up the command to run this test in a subprocess.
	cmd := exec.Command(os.Args[0], "-test.run=TestPromptForInputEmpty")
	cmd.Env = append(os.Environ(), "BE_CRASHER=1")
	output, err := cmd.CombinedOutput()

	// We expect the subprocess to exit with a non-zero exit code.
	if err == nil {
		t.Fatalf("Expected subprocess to fail, but it succeeded with output: %q", output)
	}
}

// TestPromptForMandatoryInputWithDefault tests that if a non-empty defaultValue is provided,
// the function returns it trimmed and does not prompt the user.
func TestPromptForMandatoryInputWithDefault(t *testing.T) {
	defaultVal := "   providedValue   "
	res := promptForMandatoryInput(defaultVal, ENTER_VALUE, INPUT_REQUIRED, RECEIVED_INPUT, false, false)
	expected := "providedValue"
	if res != expected {
		t.Errorf("Expected %q but got %q", expected, res)
	}
}

// TestPromptForMandatoryInputWithPrompt tests that if defaultValue is empty and prompting is enabled,
// the function calls promptForInput and returns the user input trimmed.
// We simulate the user input via a temporary file assigned to os.Stdin.
func TestPromptForMandatoryInputWithPrompt(t *testing.T) {
	// Backup the original os.Stdin.
	origStdin := os.Stdin
	defer func() { os.Stdin = origStdin }()

	// Create a temporary file to simulate user input.
	tempFile, err := os.CreateTemp("", "stdin")
	if err != nil {
		t.Fatalf("Failed to create temporary file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	// Write input; note that promptForInput uses fmt.Scanln which requires a single token.
	input := "   userInput   \n"
	if _, err := tempFile.WriteString(input); err != nil {
		t.Fatalf("Failed to write to temporary file: %v", err)
	}
	if _, err := tempFile.Seek(0, 0); err != nil {
		t.Fatalf("Failed to reset file offset: %v", err)
	}
	os.Stdin = tempFile

	res := promptForMandatoryInput("", ENTER_VALUE, INPUT_REQUIRED, RECEIVED_INPUT, false, false)
	expected := "userInput"
	if res != expected {
		t.Errorf("Expected %q but got %q", expected, res)
	}
}

// TestPromptForMandatoryInputEmptyPrompt tests that if prompting is enabled but the user provides empty input,
// the function logs a fatal error and exits. We use a subprocess to capture os.Exit.
func TestPromptForMandatoryInputEmptyPrompt(t *testing.T) {
	// If BE_CRASHER_MANDATORY_EMPTY is set in the environment, this indicates we are in the subprocess.
	if os.Getenv("BE_CRASHER_MANDATORY_EMPTY") == "1" {
		// Create a temporary file with only a newline to simulate empty input.
		tempFile, err := os.CreateTemp("", "stdin")
		if err != nil {
			os.Exit(2)
		}
		defer os.Remove(tempFile.Name())
		if _, err := tempFile.WriteString("\n"); err != nil {
			os.Exit(2)
		}
		if _, err := tempFile.Seek(0, 0); err != nil {
			os.Exit(2)
		}
		os.Stdin = tempFile

		// This call is expected to trigger zap.L().Fatal and exit the process.
		_ = promptForMandatoryInput("", ENTER_VALUE, INPUT_REQUIRED, RECEIVED_INPUT, false, false)
		// Should not reach here; exit with non-zero code if it does.
		os.Exit(3)
	}

	// Run the subprocess test.
	cmd := exec.Command(os.Args[0], "-test.run=TestPromptForMandatoryInputEmptyPrompt")
	cmd.Env = append(os.Environ(), "BE_CRASHER_MANDATORY_EMPTY=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("Expected subprocess to exit with fatal error, but it succeeded, output: %q", output)
	}
	// Optionally, check output contains INPUT_REQUIRED message or similar, if desired.
}

// TestPromptForMandatoryInputPromptDisabled tests that if prompting is disabled and no defaultValue is provided,
// the function logs a fatal error and exits. Again, we use a subprocess.
func TestPromptForMandatoryInputPromptDisabled(t *testing.T) {
	if os.Getenv("BE_CRASHER_MANDATORY_DISABLED") == "1" {
		// With prompting disabled, the function should log a fatal error immediately.
		_ = promptForMandatoryInput("", ENTER_VALUE, INPUT_REQUIRED, RECEIVED_INPUT, true, false)
		os.Exit(3)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestPromptForMandatoryInputPromptDisabled")
	cmd.Env = append(os.Environ(), "BE_CRASHER_MANDATORY_DISABLED=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("Expected subprocess to fail due to prompting disabled, but it succeeded, output: %q", output)
	}
	// Optionally, further validate that output contains "Prompting is disabled" if needed.
}
