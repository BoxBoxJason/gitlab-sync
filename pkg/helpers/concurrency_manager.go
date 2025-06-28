package helpers

import (
	"fmt"
)

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
	var merged []error

	switch v := input.(type) {
	case nil:
		return nil

	case chan []error:
		for errs := range v {
			for _, err := range errs {
				if err != nil {
					merged = append(merged, err)
				}
			}
		}

	case chan error:
		for err := range v {
			if err != nil {
				merged = append(merged, err)
			}
		}

	case []error:
		for _, err := range v {
			if err != nil {
				merged = append(merged, err)
			}
		}

	case *[]error:
		if v != nil {
			for _, err := range *v {
				if err != nil {
					merged = append(merged, err)
				}
			}
		}

	case error:
		if v != nil {
			merged = append(merged, v)
		}

	case *error:
		if v != nil && *v != nil {
			merged = append(merged, *v)
		}

	default:
		// unsupported type
		return []error{fmt.Errorf("invalid input type in MergeErrors: %T", v)}
	}

	if len(merged) == 0 {
		return nil
	}
	return merged
}
