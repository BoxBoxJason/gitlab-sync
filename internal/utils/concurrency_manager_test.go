package utils

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

const (
	ERROR_1 = "Error 1"
	ERROR_2 = "Error 2"

	EXPECT_GOT_MESSAGE     = "Expected: %q, Got: %q"
	EXPECT_NIL_GOT_MESSAGE = "Expected nil, Got: %q"
)

// toStrings converts a []error into a []string for easy comparison
func toStrings(errs []error) []string {
	if errs == nil {
		return nil
	}
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Error()
	}
	return out
}

func TestMergeErrors(t *testing.T) {
	type args struct {
		// factory builds the argument to pass into MergeErrors
		factory func() any
	}
	tests := []struct {
		name    string
		args    args
		want    []string
		wantNil bool // true if we expect a nil slice back
	}{
		{
			name: "single error",
			args: args{factory: func() any {
				return errors.New("an error")
			}},
			want:    []string{"an error"},
			wantNil: false,
		},
		{
			name: "nil error",
			args: args{factory: func() any {
				return error(nil)
			}},
			want:    nil,
			wantNil: true,
		},
		{
			name: "pointer to error",
			args: args{factory: func() any {
				e := errors.New("ptr err")
				return &e
			}},
			want:    []string{"ptr err"},
			wantNil: false,
		},
		{
			name: "pointer to nil error",
			args: args{factory: func() any {
				var e error
				return &e
			}},
			want:    nil,
			wantNil: true,
		},
		{
			name: "slice of errors",
			args: args{factory: func() any {
				return []error{nil, fmt.Errorf("a"), nil, fmt.Errorf("b")}
			}},
			want:    []string{"a", "b"},
			wantNil: false,
		},
		{
			name: "empty slice",
			args: args{factory: func() any {
				return []error{}
			}},
			want:    nil,
			wantNil: true,
		},
		{
			name: "pointer to slice",
			args: args{factory: func() any {
				s := []error{errors.New("x"), nil, errors.New("y")}
				return &s
			}},
			want:    []string{"x", "y"},
			wantNil: false,
		},
		{
			name: "nil *[]error",
			args: args{factory: func() any {
				var s *[]error
				return s
			}},
			want:    nil,
			wantNil: true,
		},
		{
			name: "chan error",
			args: args{factory: func() any {
				ch := make(chan error, 3)
				ch <- nil
				ch <- fmt.Errorf("one")
				ch <- fmt.Errorf("two")
				close(ch)
				return ch
			}},
			want:    []string{"one", "two"},
			wantNil: false,
		},
		{
			name: "empty chan error",
			args: args{factory: func() any {
				ch := make(chan error, 1)
				close(ch)
				return ch
			}},
			want:    nil,
			wantNil: true,
		},
		{
			name: "chan []error",
			args: args{factory: func() any {
				ch := make(chan []error, 2)
				ch <- []error{nil, errors.New("foo")}
				ch <- []error{errors.New("bar"), nil}
				close(ch)
				return ch
			}},
			want:    []string{"foo", "bar"},
			wantNil: false,
		},
		{
			name: "empty chan []error",
			args: args{factory: func() any {
				ch := make(chan []error, 1)
				close(ch)
				return ch
			}},
			want:    nil,
			wantNil: true,
		},
		{
			name: "invalid type",
			args: args{factory: func() any {
				return "bad"
			}},
			// we expect one error whose text mentions invalid input
			want:    []string{"invalid input type in MergeErrors: string"},
			wantNil: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErrs := MergeErrors(tc.args.factory())
			if tc.wantNil {
				if gotErrs != nil {
					t.Fatalf("got %v, want nil", gotErrs)
				}
				return
			}
			got := toStrings(gotErrs)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
