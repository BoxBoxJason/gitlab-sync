package utils

import (
	"errors"
	"fmt"
	"strings"
)

// MergeErrors takes an error iterable and an indent level
// and returns a single error that contains all the errors from the iterable.
// The errors are formatted with the specified indent level.
func MergeErrors(errorsChan any, indent int) error {
	// Merge all errors from the channels into a single error
	mergeErrors := []error{}

	switch v := errorsChan.(type) {
	case chan error:
		// If the channel is of type chan error, we can read from it
		// and collect the errors
		for err := range v {
			if err != nil {
				mergeErrors = append(mergeErrors, err)
			}
		}
	case []error:
		// If the input is a slice of errors, we can just assign it
		// to the mergeErrors slice
		mergeErrors = v
	default:
		// If the input is not a channel or a slice of errors,
		// we return an error indicating that the input is invalid
		return fmt.Errorf("invalid input type: %T", v)
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
