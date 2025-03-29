/*
Logger utility functions for verbose logging.
This package provides functions to set verbose logging and log messages
*/
package utils

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
)

var (
	// verbose controls whether verbose logging is enabled
	// It is set to false by default.
	verbose bool
	// muVerbose is a mutex to protect access to the verbose variable
	muVerbose = &sync.RWMutex{}
)

// SetVerbose sets the verbose flag to the given value.
// It is safe to call this function from multiple goroutines.
func SetVerbose(v bool) {
	muVerbose.Lock()
	defer muVerbose.Unlock()
	verbose = v
}

// GetVerbose returns the current value of the verbose flag.
// It is safe to call this function from multiple goroutines.
func getVerbose() bool {
	muVerbose.RLock()
	defer muVerbose.RUnlock()
	return verbose
}

// LogVerbose logs a message if verbose logging is enabled.
// It is safe to call this function from multiple goroutines.
func LogVerbose(message string) {
	if getVerbose() {
		log.Println(message)
	}
}

// LogVerbosef logs a formatted message if verbose logging is enabled.
// It is safe to call this function from multiple goroutines.
func LogVerbosef(format string, args ...any) {
	if getVerbose() {
		log.Printf(format, args...)
	}
}

func MergeErrors(errorsChan chan error, indent int) error {
	// Merge all errors from the channels into a single error
	mergeErrors := []error{}
	for err := range errorsChan {
		if err != nil {
			mergeErrors = append(mergeErrors, err)
		}
	}
	var combinedError error = nil
	if len(mergeErrors) > 0 {
		combinedErrorString := "\n"
		for _, err := range mergeErrors {
			combinedErrorString += fmt.Sprintf("%s- %s\n", strings.Repeat(" ", indent), err.Error())
		}
		combinedError = errors.New(combinedErrorString)
	}
	return combinedError
}
