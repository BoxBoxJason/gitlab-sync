package utils

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

const (
	FAKE_VALID_PROJECT = "namespace/project"
	FAKE_VALID_GROUP   = "group1"
)

var (
	FAKE_VALID_MAPPPING = &MirrorMapping{
		Projects: map[string]*MirroringOptions{
			FAKE_VALID_PROJECT: {
				DestinationPath:     FAKE_VALID_PROJECT,
				CI_CD_Catalog:       true,
				Issues:              true,
				MirrorTriggerBuilds: false,
				Visibility:          "private",
			},
		},
		Groups: map[string]*MirroringOptions{
			FAKE_VALID_GROUP: {
				DestinationPath:     FAKE_VALID_GROUP,
				CI_CD_Catalog:       true,
				Issues:              true,
				MirrorTriggerBuilds: false,
				Visibility:          "private",
			},
		},
	}

	FAKE_VALID_MAPPING_RAW = fmt.Sprintf(`{
		"projects": {
			"%s": {
				"destination_path": "%s",
				"ci_cd_catalog": true,
				"issues": true,
				"mirror_trigger_builds": false,
				"visibility": "private"
			}
		},
		"groups": {
			"%s": {
				"destination_path": "%s",
				"ci_cd_catalog": true,
				"issues": true,
				"mirror_trigger_builds": false,
				"visibility": "private"
			}
		}
	}`, FAKE_VALID_PROJECT, FAKE_VALID_PROJECT, FAKE_VALID_GROUP, FAKE_VALID_GROUP)
)

// testMirroringOptions is a helper function to create a MirroringOptions instance for testing
func testMirroringOptions() *MirroringOptions {
	return &MirroringOptions{
		DestinationPath: "project",
		CI_CD_Catalog:   true,
		Issues:          true,
	}
}

// TestAddProject tests adding a project to the MirrorMapping
func TestAddProject(t *testing.T) {
	m := &MirrorMapping{
		Projects: make(map[string]*MirroringOptions),
	}

	project := "test-project"
	options := testMirroringOptions()

	m.AddProject(project, options)

	if got, exists := m.Projects[project]; !exists {
		t.Fatalf("expected project %s to be added", project)
	} else if !reflect.DeepEqual(got, options) {
		t.Errorf("expected project options %v, got %v", options, got)
	}
}

// TestAddGroup tests adding a group to the MirrorMapping
func TestAddGroup(t *testing.T) {
	m := &MirrorMapping{
		Groups: make(map[string]*MirroringOptions),
	}

	group := "test-group"
	options := testMirroringOptions()

	m.AddGroup(group, options)

	if got, exists := m.Groups[group]; !exists {
		t.Fatalf("expected group %s to be added", group)
	} else if !reflect.DeepEqual(got, options) {
		t.Errorf("expected group options %v, got %v", options, got)
	}
}

// TestOpenMirrorMapping tests opening and parsing a JSON file into a MirrorMapping
func TestOpenMirrorMapping(t *testing.T) {

	file := createTempTestFile(FAKE_VALID_MAPPING_RAW, t)
	defer os.Remove(file.Name())

	mapping, err := OpenMirrorMapping(file.Name())
	if err != nil {
		t.Fatalf("OpenMirrorMapping() error = %v", err)
	}

	// Check if projects and groups are equal
	if len(mapping.Projects) != len(FAKE_VALID_MAPPPING.Projects) || len(mapping.Groups) != len(FAKE_VALID_MAPPPING.Groups) {
		t.Fatalf("expected mapping to have %d projects and %d groups, got %d projects and %d groups", len(FAKE_VALID_MAPPPING.Projects), len(FAKE_VALID_MAPPPING.Groups), len(mapping.Projects), len(mapping.Groups))
	}
	for k, v := range mapping.Projects {
		if expected, ok := FAKE_VALID_MAPPPING.Projects[k]; ok {
			if !reflect.DeepEqual(v, expected) {
				t.Errorf("expected project %s options %v, got %v", k, expected, v)
			}
		} else {
			t.Errorf("unexpected project %s in mapping", k)
		}
	}
	for k, v := range mapping.Groups {
		if expected, ok := FAKE_VALID_MAPPPING.Groups[k]; ok {
			if !reflect.DeepEqual(v, expected) {
				t.Errorf("expected group %s options %v, got %v", k, expected, v)
			}
		} else {
			t.Errorf("unexpected group %s in mapping", k)
		}
	}
}

func createTempTestFile(fileContent string, t *testing.T) *os.File {
	file, err := os.CreateTemp("", "testfile")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	if _, err := file.Write([]byte(fileContent)); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}

	err = file.Close()
	if err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}
	return file
}

// TestCheck tests the check method of MirrorMapping
func TestCheck(t *testing.T) {
	tests := []struct {
		name     string
		mapping  *MirrorMapping
		wantMsgs []string
	}{
		{
			name: "ValidMapping",
			mapping: &MirrorMapping{
				Projects: map[string]*MirroringOptions{
					FAKE_VALID_PROJECT: {
						DestinationPath: FAKE_VALID_PROJECT,
						CI_CD_Catalog:   true,
						Issues:          true,
					},
				},
				Groups: map[string]*MirroringOptions{
					FAKE_VALID_GROUP: {
						DestinationPath: FAKE_VALID_GROUP,
						CI_CD_Catalog:   true,
						Issues:          true,
					},
				},
			},
			// no errors expected
			wantMsgs: nil,
		},
		{
			name: "InvalidMappingNoProjectsOrGroups",
			mapping: &MirrorMapping{
				Projects: map[string]*MirroringOptions{},
				Groups:   map[string]*MirroringOptions{},
			},
			wantMsgs: []string{
				"no projects or groups defined in the mapping",
			},
		},
		{
			name: "InvalidProjectMapping",
			mapping: &MirrorMapping{
				Projects: map[string]*MirroringOptions{
					"": {
						DestinationPath: "",
					},
				},
				Groups: map[string]*MirroringOptions{},
			},
			wantMsgs: []string{
				"invalid (empty) string in project mapping",
			},
		},
		{
			name: "InvalidGroupMapping",
			mapping: &MirrorMapping{
				Projects: map[string]*MirroringOptions{},
				Groups: map[string]*MirroringOptions{
					"": {
						DestinationPath: "",
					},
				},
			},
			wantMsgs: []string{
				"invalid (empty) string in group mapping",
			},
		},
		{
			name: "MultipleErrors",
			mapping: &MirrorMapping{
				Projects: map[string]*MirroringOptions{
					"": {
						DestinationPath: "",
					},
				},
				Groups: map[string]*MirroringOptions{
					"": {
						DestinationPath: "",
					},
				},
			},
			wantMsgs: []string{
				"invalid (empty) string in project mapping",
				"invalid (empty) string in group mapping",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.mapping.check() // now returns []error
			got := toStrings(errs)
			if !reflect.DeepEqual(got, tt.wantMsgs) {
				t.Errorf("check() = %v, want %v", got, tt.wantMsgs)
			}
		})
	}
}

func TestStringArraysMatchValues(t *testing.T) {
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
		tc := tc // capture range variable
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
	tests := []struct {
		name  string
		input string
		want  gitlab.VisibilityValue
	}{
		{
			name:  "public visibility",
			input: string(gitlab.PublicVisibility),
			want:  gitlab.PublicVisibility,
		},
		{
			name:  "internal visibility",
			input: string(gitlab.InternalVisibility),
			want:  gitlab.InternalVisibility,
		},
		{
			name:  "private visibility",
			input: string(gitlab.PrivateVisibility),
			want:  gitlab.PrivateVisibility,
		},
		{
			name:  "unknown defaults to public",
			input: "something-else",
			want:  gitlab.PublicVisibility,
		},
	}

	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ConvertVisibility(tc.input)
			if got != tc.want {
				t.Errorf("ConvertVisibility(%q) = %v; want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestCheckVisibility(t *testing.T) {
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkVisibility(tc.input)
			if got != tc.want {
				t.Errorf("checkVisibility(%q) = %v; want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestMirrorMapping_GetProject(t *testing.T) {
	// Prepare a mirror mapping with some project entries
	opts1 := &MirroringOptions{
		DestinationPath:     "dest1",
		CI_CD_Catalog:       true,
		Issues:              false,
		MirrorTriggerBuilds: true,
		Visibility:          "public",
		MirrorReleases:      false,
	}
	opts2 := &MirroringOptions{
		DestinationPath:     "dest2",
		CI_CD_Catalog:       false,
		Issues:              true,
		MirrorTriggerBuilds: false,
		Visibility:          "private",
		MirrorReleases:      true,
	}
	mm := &MirrorMapping{
		Projects: map[string]*MirroringOptions{
			"project-one": opts1,
			"project-two": opts2,
		},
		Groups: map[string]*MirroringOptions{},
	}

	tests := []struct {
		name   string
		key    string
		want   *MirroringOptions
		wantOk bool
	}{
		{
			name:   "existing project-one",
			key:    "project-one",
			want:   opts1,
			wantOk: true,
		},
		{
			name:   "existing project-two",
			key:    "project-two",
			want:   opts2,
			wantOk: true,
		},
		{
			name:   "nonexistent project",
			key:    "no-such-project",
			want:   nil,
			wantOk: false,
		},
	}

	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := mm.GetProject(tc.key)
			if ok != tc.wantOk {
				t.Errorf("GetProject(%q) returned ok=%v, want %v", tc.key, ok, tc.wantOk)
			}
			if got != tc.want {
				t.Errorf("GetProject(%q) returned %+v, want %+v", tc.key, got, tc.want)
			}
		})
	}
}

func TestMirrorMapping_GetGroup(t *testing.T) {
	// Prepare a mirror mapping with some group entries
	optsA := &MirroringOptions{
		DestinationPath:     "groupDestA",
		CI_CD_Catalog:       true,
		Issues:              true,
		MirrorTriggerBuilds: false,
		Visibility:          "internal",
		MirrorReleases:      true,
	}
	optsB := &MirroringOptions{
		DestinationPath:     "groupDestB",
		CI_CD_Catalog:       false,
		Issues:              false,
		MirrorTriggerBuilds: true,
		Visibility:          "private",
		MirrorReleases:      false,
	}
	mm := &MirrorMapping{
		Projects: map[string]*MirroringOptions{},
		Groups: map[string]*MirroringOptions{
			"group-A": optsA,
			"group-B": optsB,
		},
	}

	tests := []struct {
		name   string
		key    string
		want   *MirroringOptions
		wantOk bool
	}{
		{
			name:   "existing group-A",
			key:    "group-A",
			want:   optsA,
			wantOk: true,
		},
		{
			name:   "existing group-B",
			key:    "group-B",
			want:   optsB,
			wantOk: true,
		},
		{
			name:   "nonexistent group",
			key:    "no-such-group",
			want:   nil,
			wantOk: false,
		},
	}

	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := mm.GetGroup(tc.key)
			if ok != tc.wantOk {
				t.Errorf("GetGroup(%q) returned ok=%v, want %v", tc.key, ok, tc.wantOk)
			}
			if got != tc.want {
				t.Errorf("GetGroup(%q) returned %+v, want %+v", tc.key, got, tc.want)
			}
		})
	}
}
