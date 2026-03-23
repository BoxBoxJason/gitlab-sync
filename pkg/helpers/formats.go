package helpers

import (
	"strings"
)

// ToStrings converts a []error into a []string for easy comparison.
func ToStrings(errs []error) []string {
	if errs == nil {
		return nil
	}

	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Error()
	}

	return out
}

// MatchPathAgainstFilters determines if a given path matches any of the specified filters.
//   - Returns true if the path is an exact match in the allowList.
//   - Returns true if the path has a prefix matching any entry in the prefixList, and returns the matching prefix.
//
// If a prefix match is found, the matching prefix is returned. Otherwise, an empty string is returned.
func MatchPathAgainstFilters(path string, allowList, prefixList *map[string]struct{}) (string, bool) {
	if allowList != nil {
		if _, ok := (*allowList)[path]; ok {
			return "", true
		}
	}

	if prefixList != nil {
		for prefix := range *prefixList {
			if strings.HasPrefix(path, prefix) {
				return prefix, true
			}
		}
	}

	return "", false
}

// Deref safely dereferences a pointer of any type, returning a default value if the pointer is nil.
func Deref[T any](ptr *T, defaultValue T) T {
	if ptr == nil {
		return defaultValue
	}

	return *ptr
}
