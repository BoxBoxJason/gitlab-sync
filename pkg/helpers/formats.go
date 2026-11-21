package helpers

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// MatchPathAgainstFilters determines if a given path matches any of the specified filters.
//   - Returns true if the path is an exact match in the allowList.
//   - Returns true if the path has a prefix matching any entry in the prefixList, and returns the matching prefix.
//
// If a prefix match is found, the matching prefix is returned. Otherwise, an empty string is returned.
func MatchPathAgainstFilters(path string, allowList, prefixList map[string]struct{}) (string, bool) {
	if _, ok := allowList[path]; ok {
		return "", true
	}

	for prefix := range prefixList {
		if strings.HasPrefix(path, prefix) {
			return prefix, true
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

// EnvString reads a trimmed string from the environment, falling back to
// defaultValue when the variable is unset or blank.
func EnvString(name, defaultValue string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}

	return defaultValue
}

// EnvDuration reads a Go duration (for example "720h" or "45m") from the
// environment, falling back to defaultValue when the variable is unset or does
// not parse. An unparsable value yields defaultValue and a non-nil error, so the
// caller can report it once its logger is ready: this runs while the flags are
// registered, before the logger is installed. The flag can still override it.
func EnvDuration(name string, defaultValue time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue, nil
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return defaultValue, fmt.Errorf("ignoring invalid duration %q in %s: %w", raw, name, err)
	}

	return parsed, nil
}

// EnvInt reads an integer from the environment, falling back to defaultValue
// when the variable is unset, blank or does not parse. An unparsable value
// yields defaultValue and a non-nil error, so the caller can report it once its
// logger is ready. The flag can still override it.
func EnvInt(name string, defaultValue int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue, fmt.Errorf("ignoring invalid integer %q in %s: %w", raw, name, err)
	}

	return parsed, nil
}

// EnvBool reads a boolean (anything strconv.ParseBool accepts) from the
// environment, falling back to defaultValue when the variable is unset, blank
// or does not parse. An unparsable value yields defaultValue and a non-nil
// error, so the caller can report it once its logger is ready. The flag can
// still override it.
func EnvBool(name string, defaultValue bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return defaultValue, fmt.Errorf("ignoring invalid boolean %q in %s: %w", raw, name, err)
	}

	return parsed, nil
}
