package helpers

import (
	"fmt"
)

func appendNonNilErrors(existing, errorsToAppend []error) []error {
	for _, currentErr := range errorsToAppend {
		if currentErr != nil {
			existing = append(existing, currentErr)
		}
	}

	return existing
}

func finalizeMergedErrors(merged []error) []error {
	if len(merged) == 0 {
		return nil
	}

	return merged
}

func mergeFromChannelInput(input any) ([]error, bool) {
	merged := []error{}

	errorsChannel, channelAssertionOk := input.(chan []error)
	if channelAssertionOk {
		for errorsFromChannel := range errorsChannel {
			merged = appendNonNilErrors(merged, errorsFromChannel)
		}

		return merged, true
	}

	errorChannel, errorChannelAssertionOk := input.(chan error)
	if errorChannelAssertionOk {
		for currentErr := range errorChannel {
			if currentErr != nil {
				merged = append(merged, currentErr)
			}
		}

		return merged, true
	}

	return nil, false
}

func mergeFromSliceInput(input any) ([]error, bool) {
	errorsList, listAssertionOk := input.([]error)
	if listAssertionOk {
		return appendNonNilErrors(nil, errorsList), true
	}

	errorsPointer, pointerAssertionOk := input.(*[]error)
	if pointerAssertionOk {
		if errorsPointer == nil {
			return nil, true
		}

		return appendNonNilErrors(nil, *errorsPointer), true
	}

	return nil, false
}

func mergeFromSingleErrorInput(input any) ([]error, bool) {
	singleError, singleErrorAssertionOk := input.(error)
	if singleErrorAssertionOk {
		if singleError == nil {
			return nil, true
		}

		return []error{singleError}, true
	}

	errorPointer, errorPointerAssertionOk := input.(*error)
	if errorPointerAssertionOk {
		if errorPointer == nil || *errorPointer == nil {
			return nil, true
		}

		return []error{*errorPointer}, true
	}

	return nil, false
}

// MergeErrors collects any number of errors from various inputs:
//   - chan error         (must be closed by sender)
//   - chan []error       (must be closed by sender)
//   - []error
//   - *[]error
//   - error
//   - *error
//
// Returns nil if there are no non-nil errors.
func MergeErrors(input any) []error {
	if input == nil {
		return nil
	}

	merged, handled := mergeFromChannelInput(input)
	if handled {
		return finalizeMergedErrors(merged)
	}

	merged, handled = mergeFromSliceInput(input)
	if handled {
		return finalizeMergedErrors(merged)
	}

	merged, handled = mergeFromSingleErrorInput(input)
	if handled {
		return finalizeMergedErrors(merged)
	}

	return []error{fmt.Errorf("invalid input type in MergeErrors: %T", input)}
}
