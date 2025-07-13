package helpers

import "testing"

func TestMatchPathAgainstFilters(t *testing.T) {
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
		prefix, matched := MatchPathAgainstFilters(test.path, &allowList, &prefixList)
		if prefix != test.expected || matched != test.matched {
			t.Errorf("For path %s: expected (%s, %t), got (%s, %t)", test.path, test.expected, test.matched, prefix, matched)
		}
	}
}
