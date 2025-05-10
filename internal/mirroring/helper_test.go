package mirroring

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	setupTestGroupGet(mux, TEST_GROUP, TEST_GROUP_STRING)
	setupTestGroupGet(mux, TEST_GROUP_2, TEST_GROUP_2_STRING)
}

// setupTestGroupGet sets up the test HTTP server with handlers for group-related
func setupTestGroupGet(mux *http.ServeMux, group *gitlab.Group, stringResponse string) {
	// Setup the get group response from the group path
	mux.HandleFunc(fmt.Sprintf("/api/v4/groups/%s", url.QueryEscape(group.FullPath)), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
		// Set response status to 200 OK
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, stringResponse)
	})

	// Setup the get group response from the group ID
	mux.HandleFunc(fmt.Sprintf("/api/v4/groups/%d", group.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
		fmt.Fprint(w, stringResponse)
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
	setupTestProjectGet(mux, TEST_PROJECT, TEST_PROJECT_STRING)
	setupTestProjectGet(mux, TEST_PROJECT_2, TEST_PROJECT_2_STRING)

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

// setupTestProjectGet sets up the test HTTP server with handlers for project-related
func setupTestProjectGet(mux *http.ServeMux, project *gitlab.Project, stringResponse string) {
	// Setup the get project response from the project path
	mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%s", url.QueryEscape(project.PathWithNamespace)), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
		fmt.Fprint(w, stringResponse)
	})
	// Setup the get project response from the project ID
	mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d", project.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
		fmt.Fprint(w, stringResponse)
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
