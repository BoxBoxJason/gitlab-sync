package utils

import (
	"errors"
	"fmt"
	"strings"
)

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
