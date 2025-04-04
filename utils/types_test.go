package utils

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
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
	expectedMapping := &MirrorMapping{
		Projects: map[string]*MirroringOptions{
			"project1": {
				DestinationPath:     "http://example.com/project",
				CI_CD_Catalog:       true,
				Issues:              true,
				MirrorTriggerBuilds: false,
				Visibility:          "private",
			},
		},
		Groups: map[string]*MirroringOptions{
			"group1": {
				DestinationPath:     "http://example.com/group",
				CI_CD_Catalog:       true,
				Issues:              true,
				MirrorTriggerBuilds: false,
				Visibility:          "private",
			},
		},
	}

	fileContent := `{
		"projects": {
			"project1": {
				"destination_path": "http://example.com/project",
				"ci_cd_catalog": true,
				"issues": true,
				"mirror_trigger_builds": false,
				"visibility": "private"
			}
		},
		"groups": {
			"group1": {
				"destination_path": "http://example.com/group",
				"ci_cd_catalog": true,
				"issues": true,
				"mirror_trigger_builds": false,
				"visibility": "private"
			}
		}
	}`

	file, err := os.CreateTemp("", "mirror_mapping_test.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(file.Name())

	if _, err := file.Write([]byte(fileContent)); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}

	file.Close()

	mapping, err := OpenMirrorMapping(file.Name())
	if err != nil {
		t.Fatalf("OpenMirrorMapping() error = %v", err)
	}

	// Check if projects and groups are equal
	if len(mapping.Projects) != len(expectedMapping.Projects) || len(mapping.Groups) != len(expectedMapping.Groups) {
		t.Fatalf("expected mapping to have %d projects and %d groups, got %d projects and %d groups", len(expectedMapping.Projects), len(expectedMapping.Groups), len(mapping.Projects), len(mapping.Groups))
	}
	for k, v := range mapping.Projects {
		if expected, ok := expectedMapping.Projects[k]; ok {
			if !reflect.DeepEqual(v, expected) {
				t.Errorf("expected project %s options %v, got %v", k, expected, v)
			}
		} else {
			t.Errorf("unexpected project %s in mapping", k)
		}
	}
	for k, v := range mapping.Groups {
		if expected, ok := expectedMapping.Groups[k]; ok {
			if !reflect.DeepEqual(v, expected) {
				t.Errorf("expected group %s options %v, got %v", k, expected, v)
			}
		} else {
			t.Errorf("unexpected group %s in mapping", k)
		}
	}
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
					"project1": {
						DestinationPath: "http://example.com/project",
						CI_CD_Catalog:   true,
						Issues:          true,
					},
				},
				Groups: map[string]*MirroringOptions{
					"group1": {
						DestinationPath: "http://example.com/group",
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
			expectedErr: "\n  - invalid (empty) string in project mapping: \n  - invalid project destination path (must be in a namespace): \n",
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
			expectedErr: "\n  - invalid (empty) string in group mapping: \n",
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

// TestSendRequest tests sending a GraphQL request to GitLab
func TestSendRequest(t *testing.T) {
	client := NewGitlabGraphQLClient("test-token", "http://example.com")

	request := &GraphQLRequest{
		Query:     "query { test }",
		Variables: "",
	}

	server := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data": "response"}`))
	})
	testServer := httptest.NewServer(server)
	defer testServer.Close()

	client.URL = testServer.URL

	response, err := client.SendRequest(request, http.MethodPost)
	if err != nil {
		t.Fatalf("SendRequest() error = %v", err)
	}

	expectedResponse := `{"data": "response"}`
	if response != expectedResponse {
		t.Errorf("expected response %v, got %v", expectedResponse, response)
	}
}
