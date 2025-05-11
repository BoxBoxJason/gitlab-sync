package mirroring

import (
	"fmt"
	"gitlab-sync/internal/utils"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

const (
	HEADER_CONTENT_TYPE = "Content-Type"
	HEADER_ACCEPT       = "application/json"
)

var (
	// TEST_PROJECT is a test project used in unit tests.
	TEST_PROJECT = &gitlab.Project{
		Name:              "Test Project",
		PathWithNamespace: "test/group/project",
		Path:              "project",
		ID:                1,
	}

	// TEST_PROJECT_STRING is the string representation of TEST_PROJECT.
	// It is used for testing purposes.
	// It is a JSON string that contains the ID, name, path_with_namespace, and path of the project.
	TEST_PROJECT_STRING = fmt.Sprintf(`{
		"id": %d,
		"name": "%s",
		"path_with_namespace": "%s",
		"path": "%s"
		}`, TEST_PROJECT.ID, TEST_PROJECT.Name, TEST_PROJECT.PathWithNamespace, TEST_PROJECT.Path)

	// TEST_PROJECT_2 is a second test project used for testing.
	TEST_PROJECT_2 = &gitlab.Project{
		Name:              "Test Project 2",
		PathWithNamespace: "test/group/group2/project2",
		Path:              "project2",
		ID:                2,
	}

	// TEST_PROJECT_2_STRING is the string representation of TEST_PROJECT_2.
	// It is used for testing purposes.
	// It is a JSON string that contains the ID, name, path_with_namespace, and path of the project.
	TEST_PROJECT_2_STRING = fmt.Sprintf(`{
		"id": %d,
		"name": "%s",
		"path_with_namespace": "%s",
		"path": "%s"
		}`, TEST_PROJECT_2.ID, TEST_PROJECT_2.Name, TEST_PROJECT_2.PathWithNamespace, TEST_PROJECT_2.Path)

	// TEST_PROJECTS_STRING is a string representation of multiple test projects.
	// It is used for testing purposes.
	// It is a JSON string that contains an array of project strings.
	TEST_PROJECTS_STRING = fmt.Sprintf(`[
		%s,
		%s
	]`, TEST_PROJECT_STRING, TEST_PROJECT_2_STRING)

	// INVALID_PROJECT is an invalid project used for testing.
	INVALID_PROJECT = &gitlab.Project{
		Name:              "Invalid Project",
		PathWithNamespace: "test/group/invalid_project",
		ID:                -1,
		Path:              "invalid_project",
	}

	// TEST_GROUP is a test group used in unit tests.
	TEST_GROUP = &gitlab.Group{
		Name:     "Test Group",
		FullPath: "test/group",
		ID:       1,
	}

	// TEST_GROUP_STRING is the string representation of TEST_GROUP.
	// It is used for testing purposes.
	// It is a JSON string that contains the ID, name, and full_path of the group.
	TEST_GROUP_STRING = fmt.Sprintf(`{
		"id": %d,
		"name": "%s",
		"full_path": "%s"
		}`, TEST_GROUP.ID, TEST_GROUP.Name, TEST_GROUP.FullPath)

	// TEST_GROUP_2 is a second test group used for testing.
	TEST_GROUP_2 = &gitlab.Group{
		Name:     "Test Group 2",
		FullPath: "test/group/group2",
		ID:       2,
	}

	// TEST_GROUP_2_STRING is the string representation of TEST_GROUP_2.
	// It is used for testing purposes.
	// It is a JSON string that contains the ID, name, and full_path of the group.
	TEST_GROUP_2_STRING = fmt.Sprintf(`{
		"id": %d,
		"name": "%s",
		"full_path": "%s"
		}`, TEST_GROUP_2.ID, TEST_GROUP_2.Name, TEST_GROUP_2.FullPath)

	// TEST_GROUPS_STRING is a string representation of multiple test groups.
	// It is used for testing purposes.
	// It is a JSON string that contains an array of group strings.
	TEST_GROUPS_STRING = fmt.Sprintf(`[
		%s,
		%s
	]`, TEST_GROUP_STRING, TEST_GROUP_2_STRING)

	// INVALID_GROUP is an invalid group used for testing.
	INVALID_GROUP = &gitlab.Group{
		Name:     "Invalid Group",
		FullPath: "test/group/invalid_group",
	}

	// TEST_RELEASE is a test release used in unit tests.
	TEST_RELEASE_STRING = `{
		"tag_name": "v0.1",
		"description": "description",
		"name": "Awesome app v0.1 alpha",
		"description_html": "description_html",
		"created_at": "2019-01-03T01:55:18.203Z",
		"author": {
			"id": 1,
			"name": "Administrator",
			"username": "root",
			"state": "active",
			"avatar_url": "https://www.gravatar.com/avatar/",
			"web_url": "http://localhost:3000/root"
		},
		"commit": {
			"id": "f8d3d94cbd347e924aa7b715845e439d00e80ca4",
			"short_id": "f8d3d94c",
			"title": "Initial commit",
			"created_at": "2019-01-03T01:53:28.000Z",
			"parent_ids": [],
			"message": "Initial commit",
			"author_name": "Administrator",
			"author_email": "admin@example.com",
			"authored_date": "2019-01-03T01:53:28.000Z",
			"committer_name": "Administrator",
			"committer_email": "admin@example.com",
			"committed_date": "2019-01-03T01:53:28.000Z"
		},
		"assets": {
			"count": 2,
			"sources": [
			{
				"format": "zip",
				"url": "http://localhost:3000/archive/v0.1/awesome-app-v0.1.zip"
			},
			{
				"format": "tar.gz",
				"url": "http://localhost:3000/archive/v0.1/awesome-app-v0.1.tar.gz"
			}
			],
			"links": []
		}
	}`

	// TEST_RELEASES_STRING is a string representation of multiple test releases.
	TEST_RELEASES_STRING = []string{`{
			"tag_name": "v0.2",
			"description": "description",
			"name": "Awesome app v0.2 beta",
			"description_html": "html",
			"created_at": "2019-01-03T01:56:19.539Z",
			"author": {
			"id": 1,
			"name": "Administrator",
			"username": "root",
			"state": "active",
			"avatar_url": "https://www.gravatar.com/avatar",
			"web_url": "http://localhost:3000/root"
			},
			"commit": {
			"id": "079e90101242458910cccd35eab0e211dfc359c0",
			"short_id": "079e9010",
			"title": "Update README.md",
			"created_at": "2019-01-03T01:55:38.000Z",
			"parent_ids": [
				"f8d3d94cbd347e924aa7b715845e439d00e80ca4"
			],
			"message": "Update README.md",
			"author_name": "Administrator",
			"author_email": "admin@example.com",
			"authored_date": "2019-01-03T01:55:38.000Z",
			"committer_name": "Administrator",
			"committer_email": "admin@example.com",
			"committed_date": "2019-01-03T01:55:38.000Z"
			},
			"assets": {
			"count": 4,
			"sources": [
				{
				"format": "zip",
				"url": "http://localhost:3000/archive/v0.2/awesome-app-v0.2.zip"
				},
				{
				"format": "tar.gz",
				"url": "http://localhost:3000/archive/v0.2/awesome-app-v0.2.tar.gz"
				}
			],
			"links": [
				{
				"id": 2,
				"name": "awesome-v0.2.msi",
				"url": "http://192.168.10.15:3000/msi",
				"external": true
				},
				{
				"id": 1,
				"name": "awesome-v0.2.dmg",
				"url": "http://192.168.10.15:3000",
				"external": true
				}
			]
			}
		}`,
		`{
			"tag_name": "v0.1",
			"description": "description",
			"name": "Awesome app v0.1 alpha",
			"description_html": "description_html",
			"created_at": "2019-01-03T01:55:18.203Z",
			"author": {
			"id": 1,
			"name": "Administrator",
			"username": "root",
			"state": "active",
			"avatar_url": "https://www.gravatar.com/avatar",
			"web_url": "http://localhost:3000/root"
			},
			"commit": {
			"id": "f8d3d94cbd347e924aa7b715845e439d00e80ca4",
			"short_id": "f8d3d94c",
			"title": "Initial commit",
			"created_at": "2019-01-03T01:53:28.000Z",
			"parent_ids": [],
			"message": "Initial commit",
			"author_name": "Administrator",
			"author_email": "admin@example.com",
			"authored_date": "2019-01-03T01:53:28.000Z",
			"committer_name": "Administrator",
			"committer_email": "admin@example.com",
			"committed_date": "2019-01-03T01:53:28.000Z"
			},
			"assets": {
			"count": 2,
			"sources": [
				{
				"format": "zip",
				"url": "http://localhost:3000/archive/v0.1/awesome-app-v0.1.zip"
				},
				{
				"format": "tar.gz",
				"url": "http://localhost:3000/archive/v0.1/awesome-app-v0.1.tar.gz"
				}
			],
			"links": []
			}
		}`}
)

// setup sets up a test HTTP server along with a gitlab.Client that is
// configured to talk to that test server.  Tests should register handlers on
// mux which provide mock responses for the API method being tested.
func setupTestServer(t *testing.T, role string, instanceSize string) (*http.ServeMux, *GitlabInstance) {
	// mux is the HTTP request multiplexer used with the test server.
	mux := http.NewServeMux()

	// server is a test HTTP server used to provide mock API responses.
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	gitlabInstance, err := newGitlabInstance(&GitlabInstanceOpts{
		GitlabURL:    server.URL,
		GitlabToken:  "test-token",
		Role:         role,
		InstanceSize: instanceSize,
		MaxRetries:   0,
	})

	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Add test handlers for the projects and groups endpoints.
	setupTestProjects(mux)
	setupTestGroups(mux)

	// Catch-all handler for undefined routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, fmt.Sprintf("Undefined route accessed: %s %s", r.Method, r.URL.Path), http.StatusNotFound)
	})

	return mux, gitlabInstance
}

// setupTestGroups sets up the test HTTP server with handlers for group-related
func setupTestGroups(mux *http.ServeMux) {
	// Setup the get groups endpoint to return a list of groups.
	mux.HandleFunc("/api/v4/groups", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
			// Set response status to 200 OK
			w.WriteHeader(http.StatusOK)
			// Return a mock response for the list of groups
			fmt.Fprint(w, TEST_GROUPS_STRING)
		case http.MethodPost:
			// Set response status to 201 Created
			w.WriteHeader(http.StatusCreated)
			w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
			// Return a mock response for the created group
			fmt.Fprint(w, TEST_GROUP_STRING)
		default:
			// Set response status to 405 Method Not Allowed
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	// Setup the get group for each group endpoint to return the group details.
	setupTestGroup(mux, TEST_GROUP, TEST_GROUP_STRING)
	setupTestGroup(mux, TEST_GROUP_2, TEST_GROUP_2_STRING)
}

// setupTestGroupGet sets up the test HTTP server with handlers for group-related
func setupTestGroup(mux *http.ServeMux, group *gitlab.Group, stringResponse string) {
	// Setup the get group response from the group path
	mux.HandleFunc(fmt.Sprintf("/api/v4/groups/%s", url.QueryEscape(group.FullPath)), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, stringResponse)
	})
	// Setup the get group response from the group ID
	mux.HandleFunc(fmt.Sprintf("/api/v4/groups/%d", group.ID), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, stringResponse)
		case http.MethodPut:
			w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, stringResponse)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
	})
	// Setup the get group avatar response from the group ID
	mux.HandleFunc(fmt.Sprintf("/api/v4/groups/%d/avatar", group.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// Write a fake image response
		w.Header().Set(HEADER_CONTENT_TYPE, "image/png")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) // PNG header
	})
}

// setupTestProjects sets up the test HTTP server with handlers for project-related
func setupTestProjects(mux *http.ServeMux) {
	// Setup the get projects endpoint to return a list of projects.
	mux.HandleFunc("/api/v4/projects", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
			// Set response status to 200 OK
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, TEST_PROJECTS_STRING)
		case http.MethodPost:
			// Set response status to 201 Created
			w.WriteHeader(http.StatusCreated)
			w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
			// Return a mock response for the created project
			fmt.Fprint(w, TEST_PROJECT_STRING)
		default:
			// Set response status to 405 Method Not Allowed
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	// Setup the get project for each project endpoint to return the project details.
	setupTestProject(mux, TEST_PROJECT, TEST_PROJECT_STRING)
	setupTestProject(mux, TEST_PROJECT_2, TEST_PROJECT_2_STRING)

	// ========== Setup the list subgroups endpoint for both groups ==========

	// Subgroups of the TEST_GROUP
	mux.HandleFunc(fmt.Sprintf("/api/v4/groups/%d/subgroups", TEST_GROUP.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
		fmt.Fprintf(w, `[%s]`, TEST_GROUP_2_STRING)
	})

	// Subgroups of the TEST_GROUP_2
	mux.HandleFunc(fmt.Sprintf("/api/v4/groups/%d/subgroups", TEST_GROUP_2.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
		fmt.Fprint(w, "[]")
	})

	// ========== Setup the list projects endpoint for each group ==========
	// Projects of the TEST_GROUP
	mux.HandleFunc(fmt.Sprintf("/api/v4/groups/%d/projects", TEST_GROUP.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
		fmt.Fprintf(w, `[%s]`, TEST_PROJECT_STRING)
	})

	// Projects of the TEST_GROUP_2
	mux.HandleFunc(fmt.Sprintf("/api/v4/groups/%d/projects", TEST_GROUP_2.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
		fmt.Fprintf(w, "[%s]", TEST_PROJECT_2_STRING)
	})
}

// setupTestProject sets up the test HTTP server with handlers for project-related actions.
func setupTestProject(mux *http.ServeMux, project *gitlab.Project, stringResponse string) {
	// Setup the get project response from the project path
	mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%s", url.QueryEscape(project.PathWithNamespace)), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, stringResponse)
	})
	// Setup the get and put project response from the project ID
	mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d", project.ID), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
			fmt.Fprint(w, stringResponse)
		case http.MethodPut:
			w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, stringResponse)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
	})
	// Setup the get project avatar response from the project ID
	mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d/avatar", project.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// Write a fake image response
		w.Header().Set(HEADER_CONTENT_TYPE, "image/png")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) // PNG header
	})
	// Setup the get project releases response from the project ID
	mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d/releases", project.ID), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "[%s]", TEST_RELEASES_STRING[project.ID%2])
		case http.MethodPost:
			w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, TEST_RELEASE_STRING)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
	})
	// setup the put project mirror pull response from the project ID
	mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d/mirror/pull", project.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "{}")
	})
}

// setupTestGitlabInstance sets up a test Gitlab instance with the given role and instance size.
func setupTestGitlabInstance(t *testing.T, role string, instanceSize string) *GitlabInstance {
	gitlabInstance, err := newGitlabInstance(&GitlabInstanceOpts{
		GitlabURL:    "https://gitlab.example.com",
		GitlabToken:  "test-token",
		Role:         role,
		InstanceSize: instanceSize,
		MaxRetries:   0,
	})
	if err != nil {
		t.Fatalf("Failed to create Gitlab instance: %v", err)
	}
	return gitlabInstance
}

func TestReverseGroupMirrorMap(t *testing.T) {
	tests := []struct {
		name                     string
		mirrorMapping            *utils.MirrorMapping
		expectedReversedMap      map[string]string
		expectedDestinationPaths []string
	}{
		{
			name: "normal mapping",
			mirrorMapping: &utils.MirrorMapping{
				Groups: map[string]*utils.MirroringOptions{
					TEST_GROUP.FullPath:   {DestinationPath: TEST_GROUP.FullPath},
					TEST_GROUP_2.FullPath: {DestinationPath: TEST_GROUP_2.FullPath},
				},
			},
			expectedReversedMap: map[string]string{
				TEST_GROUP.FullPath:   TEST_GROUP.FullPath,
				TEST_GROUP_2.FullPath: TEST_GROUP_2.FullPath,
			},
			expectedDestinationPaths: []string{TEST_GROUP.FullPath, TEST_GROUP_2.FullPath},
		},
		{
			name:                     "nil mirror mapping",
			mirrorMapping:            nil,
			expectedReversedMap:      nil,
			expectedDestinationPaths: []string{},
		},
		{
			name: "empty groups in mirror mapping",
			mirrorMapping: &utils.MirrorMapping{
				Groups: map[string]*utils.MirroringOptions{},
			},
			expectedReversedMap:      map[string]string{},
			expectedDestinationPaths: []string{},
		},
		{
			name: "unsorted input paths",
			mirrorMapping: &utils.MirrorMapping{
				Groups: map[string]*utils.MirroringOptions{
					TEST_GROUP_2.FullPath: {DestinationPath: TEST_GROUP_2.FullPath},
					TEST_GROUP.FullPath:   {DestinationPath: TEST_GROUP.FullPath},
				},
			},
			expectedReversedMap: map[string]string{
				TEST_GROUP.FullPath:   TEST_GROUP.FullPath,
				TEST_GROUP_2.FullPath: TEST_GROUP_2.FullPath,
			},
			expectedDestinationPaths: []string{TEST_GROUP.FullPath, TEST_GROUP_2.FullPath},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := &GitlabInstance{
				Groups: nil,
			}

			actualReversedMap, actualDestinationPaths := g.reverseGroupMirrorMap(tt.mirrorMapping)

			if !reflect.DeepEqual(actualReversedMap, tt.expectedReversedMap) {
				t.Errorf("reversedMirrorMap = %v, want %v", actualReversedMap, tt.expectedReversedMap)
			}

			if !reflect.DeepEqual(actualDestinationPaths, tt.expectedDestinationPaths) {
				t.Errorf("destinationGroupPaths = %v, want %v", actualDestinationPaths, tt.expectedDestinationPaths)
			}
		})
	}
}
