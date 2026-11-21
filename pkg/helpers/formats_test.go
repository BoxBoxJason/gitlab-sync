package helpers

import (
	"testing"
)

func TestMatchPathAgainstFilters(t *testing.T) {
	t.Parallel()

	allowList := map[string]struct{}{
		"/allowed/path": {},
	}
	prefixList := map[string]struct{}{
		"/prefix/": {},
	}

	tests := []struct {
		path     string
		expected string
		matched  bool
	}{
		{"/allowed/path", "", true},
		{"/prefix/something", "/prefix/", true},
		{"/not/matching", "", false},
	}

	for _, test := range tests {
		prefix, matched := MatchPathAgainstFilters(test.path, allowList, prefixList)
		if prefix != test.expected || matched != test.matched {
			t.Errorf("For path %s: expected (%s, %t), got (%s, %t)", test.path, test.expected, test.matched, prefix, matched)
		}
	}
}

func TestEnvBool(t *testing.T) {
	const name = "GITLAB_SYNC_TEST_ENV_BOOL"

	tests := []struct {
		value        string
		defaultValue bool
		want         bool
	}{
		{"", false, false},
		{"", true, true},
		{"  ", false, false},
		{"true", false, true},
		{"1", false, true},
		{"false", true, false},
		{"0", true, false},
		{" true ", false, true},
		{"nonsense", false, false},
		{"nonsense", true, true},
	}

	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv(name, test.value)

			if got := EnvBool(name, test.defaultValue); got != test.want {
				t.Errorf("envBool(%q, %v) = %v, want %v", test.value, test.defaultValue, got, test.want)
			}
		})
	}
}
