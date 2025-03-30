package utils

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

// testProjectMirroringOptions is a helper function to create a ProjectMirroringOptions instance for testing
func testProjectMirroringOptions() *ProjectMirroringOptions {
	return &ProjectMirroringOptions{
		DestinationPath: "project",
		CI_CD_Catalog:   true,
		Issues:          true,
	}
}

// testGroupMirroringOptions is a helper function to create a GroupMirroringOptions instance for testing
func testGroupMirroringOptions() *GroupMirroringOptions {
	return &GroupMirroringOptions{
		DestinationPath: "group",
		CI_CD_Catalog:   true,
		Issues:          true,
	}
}

// TestAddProject tests adding a project to the MirrorMapping
func TestAddProject(t *testing.T) {
	m := &MirrorMapping{
		Projects: make(map[string]*ProjectMirroringOptions),
	}

	project := "test-project"
	options := testProjectMirroringOptions()

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
		Groups: make(map[string]*GroupMirroringOptions),
	}

	group := "test-group"
	options := testGroupMirroringOptions()

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
		Projects: map[string]*ProjectMirroringOptions{
			"project1": {
				DestinationPath: "http://example.com/project",
				CI_CD_Catalog:   true,
				Issues:          true,
			},
		},
		Groups: map[string]*GroupMirroringOptions{
			"group1": {
				DestinationPath: "http://example.com/group",
				CI_CD_Catalog:   true,
				Issues:          true,
			},
		},
	}

	fileContent := `{
		"projects": {
			"project1": {
				"destination_path": "http://example.com/project",
				"ci_cd_catalog": true,
				"issues": true
			}
		},
		"groups": {
			"group1": {
				"destination_path": "http://example.com/group",
				"ci_cd_catalog": true,
				"issues": true
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

	if !reflect.DeepEqual(mapping, expectedMapping) {
		t.Errorf("expected mapping %v, got %v", expectedMapping, mapping)
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
				Projects: map[string]*ProjectMirroringOptions{
					"project1": {
						DestinationPath: "http://example.com/project",
						CI_CD_Catalog:   true,
						Issues:          true,
					},
				},
				Groups: map[string]*GroupMirroringOptions{
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
				Projects: map[string]*ProjectMirroringOptions{},
				Groups:   map[string]*GroupMirroringOptions{},
			},
			expectedErr: "\n  - no projects or groups defined in the mapping\n",
		},
		{
			name: "InvalidProjectMapping",
			mapping: &MirrorMapping{
				Projects: map[string]*ProjectMirroringOptions{
					"": {
						DestinationPath: "",
					},
				},
				Groups: map[string]*GroupMirroringOptions{},
			},
			expectedErr: "\n  - invalid (empty) string in project mapping: \n  - invalid project destination path (must be in a namespace): \n",
		},
		{
			name: "InvalidGroupMapping",
			mapping: &MirrorMapping{
				Projects: map[string]*ProjectMirroringOptions{},
				Groups: map[string]*GroupMirroringOptions{
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
