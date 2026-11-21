package mirrorcfg

import (
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

func TestStringArraysMatchValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b []string
		want bool
	}{
		{
			name: "both empty",
			a:    []string{},
			b:    []string{},
			want: true,
		},
		{
			name: "same order",
			a:    []string{"foo", "bar", "baz"},
			b:    []string{"foo", "bar", "baz"},
			want: true,
		},
		{
			name: "different order",
			a:    []string{"foo", "bar", "baz"},
			b:    []string{"baz", "foo", "bar"},
			want: true,
		},
		{
			name: "duplicate values",
			a:    []string{"x", "x", "y"},
			b:    []string{"y", "x", "x"},
			want: true,
		},
		{
			name: "different lengths",
			a:    []string{"one", "two"},
			b:    []string{"one"},
			want: false,
		},
		{
			name: "mismatched values",
			a:    []string{"a", "b", "c"},
			b:    []string{"a", "b", "d"},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := StringArraysMatchValues(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("StringArraysMatchValues(%v, %v) = %v; want %v",
					tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestConvertVisibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input *string
		want  gitlab.VisibilityValue
	}{
		{
			name:  "public visibility",
			input: new(string(gitlab.PublicVisibility)),
			want:  gitlab.PublicVisibility,
		},
		{
			name:  "internal visibility",
			input: new(string(gitlab.InternalVisibility)),
			want:  gitlab.InternalVisibility,
		},
		{
			name:  "private visibility",
			input: new(string(gitlab.PrivateVisibility)),
			want:  gitlab.PrivateVisibility,
		},
		{
			name:  "unknown defaults to public",
			input: new("something-else"),
			want:  gitlab.PublicVisibility,
		},
		{
			name:  "nil defaults to public",
			input: nil,
			want:  gitlab.PublicVisibility,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ConvertVisibility(tc.input)
			if got != tc.want {
				t.Errorf("ConvertVisibility(%v) = %v; want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestCheckVisibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "public visibility is valid",
			input: string(gitlab.PublicVisibility),
			want:  true,
		},
		{
			name:  "internal visibility is valid",
			input: string(gitlab.InternalVisibility),
			want:  true,
		},
		{
			name:  "private visibility is valid",
			input: string(gitlab.PrivateVisibility),
			want:  true,
		},
		{
			name:  "unknown visibility is invalid",
			input: "some-other",
			want:  false,
		},
		{
			name:  "empty string is invalid",
			input: "",
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkVisibility(tc.input)
			if got != tc.want {
				t.Errorf("checkVisibility(%q) = %v; want %v", tc.input, got, tc.want)
			}
		})
	}
}
