package mirroring

import (
	"net/http"
	"net/http/httptest"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

const (
	expectedErrorMessage = "expected error: %v, got: %v"
)

func TestNewGitlabInstance(t *testing.T) {
	t.Parallel()

	// Create a test server to mock the CurrentUser API
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	// Add mock handler for current user
	mux.HandleFunc("/api/v4/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(headerContentType, headerAccept)
		w.WriteHeader(http.StatusOK)
		writeBody(w, `{"id": 1, "username": "testuser", "name": "Test User", "state": "active"}`)
	})

	instance, err := NewGitlabInstance(&GitlabInstanceOpts{
		GitlabURL:    server.URL,
		GitlabToken:  "test-token",
		Role:         RoleSource,
		MaxRetries:   3,
		InstanceSize: InstanceSizeSmall,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if instance.Gitlab == nil {
		t.Error("expected Gitlab client to be initialized")
	}

	if instance.Projects == nil {
		t.Error("expected Projects map to be initialized")
	}

	if instance.Groups == nil {
		t.Error("expected Groups map to be initialized")
	}

	if instance.UserID != 1 {
		t.Errorf("expected UserID to be 1, got %d", instance.UserID)
	}
}

func TestNewGitlabInstanceUserAgent(t *testing.T) {
	t.Parallel()

	const userAgent = "gitlab-sync/test"

	var got string

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/api/v4/user", func(w http.ResponseWriter, r *http.Request) {
		got = r.UserAgent()
		w.Header().Set(headerContentType, headerAccept)
		writeBody(w, `{"id": 1}`)
	})

	_, err := NewGitlabInstance(&GitlabInstanceOpts{
		GitlabURL:   server.URL,
		GitlabToken: "test-token",
		Role:        RoleSource,
		MaxRetries:  1,
		UserAgent:   userAgent,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got != userAgent {
		t.Errorf("expected User-Agent %q, got %q", userAgent, got)
	}
}

func TestAddProject(t *testing.T) {
	t.Parallel()

	instance := &GitlabInstance{
		Projects: make(map[string]*gitlab.Project),
	}

	instance.AddProject(testProject)

	if got, exists := instance.Projects[testProject.PathWithNamespace]; !exists {
		t.Fatalf("expected project %s to be added", testProject.PathWithNamespace)
	} else if got != testProject {
		t.Errorf("expected project %v, got %v", testProject, got)
	}
}

func TestGetProject(t *testing.T) {
	t.Parallel()

	instance := &GitlabInstance{
		Projects: make(map[string]*gitlab.Project),
	}

	instance.Projects[testProject.PathWithNamespace] = testProject

	got := instance.GetProject(testProject.PathWithNamespace)
	if got != testProject {
		t.Errorf("expected project %v, got %v", testProject, got)
	}

	nonExistentPath := "non/existent"
	if got = instance.GetProject(nonExistentPath); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestAddGroup(t *testing.T) {
	t.Parallel()

	instance := &GitlabInstance{
		Groups: make(map[string]*gitlab.Group),
	}

	instance.AddGroup(testGroup)

	if got, exists := instance.Groups[testGroup.FullPath]; !exists {
		t.Fatalf("expected group %s to be added", testGroup.FullPath)
	} else if got != testGroup {
		t.Errorf("expected group %v, got %v", testGroup, got)
	}
}

func TestGetGroup(t *testing.T) {
	t.Parallel()

	instance := &GitlabInstance{
		Groups: make(map[string]*gitlab.Group),
	}

	instance.Groups[testGroup.FullPath] = testGroup

	got := instance.GetGroup(testGroup.FullPath)
	if got != testGroup {
		t.Errorf("expected group %v, got %v", testGroup, got)
	}

	nonExistentPath := "non/existent"
	if got = instance.GetGroup(nonExistentPath); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestGetParentNamespaceID(t *testing.T) {
	t.Parallel()

	_, gitlabInstance := setupEmptyTestServer(t, RoleDestination)
	gitlabInstance.AddGroup(testGroup)
	gitlabInstance.AddProject(testProject)

	tests := []struct {
		name          string
		path          string
		expectedID    int64
		expectedError bool
	}{
		{
			name:          "Valid parent path",
			path:          testProject.PathWithNamespace,
			expectedID:    testGroup.ID,
			expectedError: false,
		},
		{
			name:          "Invalid parent path",
			path:          "invalid/path",
			expectedID:    -1,
			expectedError: true,
		},
		{
			name:          "Existing resource with no parent path",
			path:          testGroup.FullPath,
			expectedID:    -1,
			expectedError: true,
		},
	}

	// Iterate over the test cases
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Call the function with the test case parameters
			gotID, err := gitlabInstance.GetParentNamespaceID(test.path)

			// Check if the result matches the expected value
			if gotID != test.expectedID {
				t.Errorf("expected %d, got %d", test.expectedID, gotID)
			}

			// Check if an error was expected
			if (err != nil) != test.expectedError {
				t.Errorf(expectedErrorMessage, test.expectedError, err)
			}
		})
	}
}
