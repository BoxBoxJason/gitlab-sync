package mirroring

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

const (
	headerContentType = "Content-Type"
	headerAccept      = "application/json"
)

// writeBytes writes body to w and aborts the handler if the write fails.
func writeBytes(w http.ResponseWriter, body []byte) {
	if _, err := w.Write(body); err != nil {
		panic(err)
	}
}

// writeBody writes body to w and aborts the handler if the write fails.
func writeBody(w http.ResponseWriter, body string) {
	writeBytes(w, []byte(body))
}

// requireGet reports whether r is a GET request, and answers 405 otherwise.
func requireGet(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)

		return false
	}

	return true
}

// Helper functions for common HTTP responses.
func writeJSONResponse(w http.ResponseWriter, status int, body string) {
	w.Header().Set(headerContentType, headerAccept)
	w.WriteHeader(status)
	writeBody(w, body)
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	w.WriteHeader(http.StatusMethodNotAllowed)
}

var (
	// testProject is a test project used in unit tests.
	testProject = &gitlab.Project{
		Name:              "Test Project",
		PathWithNamespace: "test/group/project",
		Path:              "project",
		ID:                1,
	}

	// testProjectString is the string representation of testProject.
	// It is used for testing purposes.
	// It is a JSON string that contains the ID, name, path_with_namespace, and path of the project.
	testProjectString = fmt.Sprintf(`{
		"id": %d,
		"name": "%s",
		"path_with_namespace": "%s",
		"path": "%s"
		}`, testProject.ID, testProject.Name, testProject.PathWithNamespace, testProject.Path)

	// testProject2 is a second test project used for testing.
	testProject2 = &gitlab.Project{
		Name:              "Test Project 2",
		PathWithNamespace: "test/group/group2/project2",
		Path:              "project2",
		ID:                2,
	}

	// testProject2String is the string representation of testProject2.
	// It is used for testing purposes.
	// It is a JSON string that contains the ID, name, path_with_namespace, and path of the project.
	testProject2String = fmt.Sprintf(`{
		"id": %d,
		"name": "%s",
		"path_with_namespace": "%s",
		"path": "%s"
		}`, testProject2.ID, testProject2.Name, testProject2.PathWithNamespace, testProject2.Path)

	// testProjectsString is a string representation of multiple test projects.
	// It is used for testing purposes.
	// It is a JSON string that contains an array of project strings.
	testProjectsString = fmt.Sprintf(`[
		%s,
		%s
	]`, testProjectString, testProject2String)

	// invalidProject is an invalid project used for testing.
	invalidProject = &gitlab.Project{
		Name:              "Invalid Project",
		PathWithNamespace: "test/group/invalid_project",
		ID:                -1,
		Path:              "invalid_project",
	}

	// testGroup is a test group used in unit tests.
	testGroup = &gitlab.Group{
		Name:     "Test Group",
		FullPath: "test/group",
		ID:       1,
	}

	// testGroupString is the string representation of testGroup.
	// It is used for testing purposes.
	// It is a JSON string that contains the ID, name, and full_path of the group.
	testGroupString = fmt.Sprintf(`{
		"id": %d,
		"name": "%s",
		"full_path": "%s"
		}`, testGroup.ID, testGroup.Name, testGroup.FullPath)

	// testGroup2 is a second test group used for testing.
	testGroup2 = &gitlab.Group{
		Name:     "Test Group 2",
		FullPath: "test/group/group2",
		ID:       2,
	}

	// testGroup2String is the string representation of testGroup2.
	// It is used for testing purposes.
	// It is a JSON string that contains the ID, name, and full_path of the group.
	testGroup2String = fmt.Sprintf(`{
		"id": %d,
		"name": "%s",
		"full_path": "%s"
		}`, testGroup2.ID, testGroup2.Name, testGroup2.FullPath)

	// testGroupsString is a string representation of multiple test groups.
	// It is used for testing purposes.
	// It is a JSON string that contains an array of group strings.
	testGroupsString = fmt.Sprintf(`[
		%s,
		%s
	]`, testGroupString, testGroup2String)
)

func setupEmptyTestServer(t *testing.T, role string) (*http.ServeMux, *GitlabInstance) {
	t.Helper()

	// mux is the HTTP request multiplexer used with the test server.
	mux := http.NewServeMux()

	// server is a test HTTP server used to provide mock API responses.
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	// Add test handler for current user
	mux.HandleFunc("/api/v4/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(headerContentType, headerAccept)
		w.WriteHeader(http.StatusOK)
		// Return mock current user with ID 1
		writeBody(w, `{"id": 1, "username": "testuser", "name": "Test User", "state": "active"}`)
	})

	gitlabInstance, err := NewGitlabInstance(&GitlabInstanceOpts{
		GitlabURL:    server.URL,
		GitlabToken:  "test-token",
		Role:         role,
		InstanceSize: InstanceSizeSmall,
		MaxRetries:   0,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	return mux, gitlabInstance
}

// setup sets up a test HTTP server along with a gitlab.Client that is
// configured to talk to that test server.  Tests should register handlers on
// mux which provide mock responses for the API method being tested.
func setupTestServer(t *testing.T, role, instanceSize string) *GitlabInstance {
	t.Helper()

	// mux is the HTTP request multiplexer used with the test server.
	mux := http.NewServeMux()

	// server is a test HTTP server used to provide mock API responses.
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	// Add test handlers for the projects and groups endpoints.
	setupTestProjects(mux)
	setupTestGroups(mux)

	// Add test handler for current user
	mux.HandleFunc("/api/v4/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(headerContentType, headerAccept)
		w.WriteHeader(http.StatusOK)
		// Return mock current user with ID 1
		writeBody(w, `{"id": 1, "username": "testuser", "name": "Test User", "state": "active"}`)
	})

	// Catch-all handler for undefined routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, fmt.Sprintf("Undefined route accessed: %s %s", r.Method, r.URL.Path), http.StatusNotFound)
	})

	gitlabInstance, err := NewGitlabInstance(&GitlabInstanceOpts{
		GitlabURL:    server.URL,
		GitlabToken:  "test-token",
		Role:         role,
		InstanceSize: instanceSize,
		MaxRetries:   0,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	return gitlabInstance
}
