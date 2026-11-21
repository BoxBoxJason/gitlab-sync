package helpers

import (
	"os"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
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
// not parse. An unparsable value is reported rather than silently ignored, but
// it does not stop the run: the flag can still override it.
func EnvDuration(name string, defaultValue time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		zap.L().Warn("Ignoring an invalid duration in the environment", zap.String("variable", name), zap.String("value", raw), zap.Error(err))

		return defaultValue
	}

	return parsed
}

// EnvBool reads a boolean (anything strconv.ParseBool accepts) from the
// environment, falling back to defaultValue when the variable is unset, blank
// or does not parse. An unparsable value is reported rather than silently
// ignored, but it does not stop the run: the flag can still override it.
func EnvBool(name string, defaultValue bool) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue
	}

	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		zap.L().Warn("Ignoring an invalid boolean in the environment", zap.String("variable", name), zap.String("value", raw), zap.Error(err))

		return defaultValue
	}

	return parsed
}
