package utils

import (
	"fmt"
	"os"
	"reflect"
	"testing"
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
		name        string
		mapping     *MirrorMapping
		expectedErr string
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
			expectedErr: "",
		},
		{
			name: "InvalidMappingNoProjectsOrGroups",
			mapping: &MirrorMapping{
				Projects: map[string]*MirroringOptions{},
				Groups:   map[string]*MirroringOptions{},
			},
			expectedErr: "\n  - no projects or groups defined in the mapping\n",
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
			expectedErr: "\n  - invalid (empty) string in project mapping\n",
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
			expectedErr: "\n  - invalid (empty) string in group mapping\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mapping.check()
			if (err != nil) && err.Error() != tt.expectedErr {
				t.Errorf("expected error %v, got %v", tt.expectedErr, err)
			}
			if (err == nil) && tt.expectedErr != "" {
				t.Errorf("expected error %v, got nil", tt.expectedErr)
			}
		})
	}
}
