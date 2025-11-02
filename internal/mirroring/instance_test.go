package mirroring

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

const (
	EXPECTED_ERROR_MESSAGE = "expected error: %v, got: %v"
)

func TestNewGitlabInstance(t *testing.T) {
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
		w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id": 1, "username": "testuser", "name": "Test User", "state": "active"}`)
	})

	instance, err := NewGitlabInstance(&GitlabInstanceOpts{
		GitlabURL:    server.URL,
		GitlabToken:  "test-token",
		Role:         ROLE_SOURCE,
		MaxRetries:   3,
		InstanceSize: INSTANCE_SIZE_SMALL,
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

func TestAddProject(t *testing.T) {
	instance := &GitlabInstance{
		Projects: make(map[string]*gitlab.Project),
	}

	instance.AddProject(TEST_PROJECT)

	if got, exists := instance.Projects[TEST_PROJECT.PathWithNamespace]; !exists {
		t.Fatalf("expected project %s to be added", TEST_PROJECT.PathWithNamespace)
	} else if got != TEST_PROJECT {
		t.Errorf("expected project %v, got %v", TEST_PROJECT, got)
	}
}

func TestGetProject(t *testing.T) {
	instance := &GitlabInstance{
		Projects: make(map[string]*gitlab.Project),
	}

	instance.Projects[TEST_PROJECT.PathWithNamespace] = TEST_PROJECT

	got := instance.GetProject(TEST_PROJECT.PathWithNamespace)
	if got != TEST_PROJECT {
		t.Errorf("expected project %v, got %v", TEST_PROJECT, got)
	}

	nonExistentPath := "non/existent"
	if got := instance.GetProject(nonExistentPath); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestAddGroup(t *testing.T) {
	instance := &GitlabInstance{
		Groups: make(map[string]*gitlab.Group),
	}

	instance.AddGroup(TEST_GROUP)

	if got, exists := instance.Groups[TEST_GROUP.FullPath]; !exists {
		t.Fatalf("expected group %s to be added", TEST_GROUP.FullPath)
	} else if got != TEST_GROUP {
		t.Errorf("expected group %v, got %v", TEST_GROUP, got)
	}
}

func TestGetGroup(t *testing.T) {
	instance := &GitlabInstance{
		Groups: make(map[string]*gitlab.Group),
	}

	instance.Groups[TEST_GROUP.FullPath] = TEST_GROUP

	got := instance.GetGroup(TEST_GROUP.FullPath)
	if got != TEST_GROUP {
		t.Errorf("expected group %v, got %v", TEST_GROUP, got)
	}

	nonExistentPath := "non/existent"
	if got := instance.GetGroup(nonExistentPath); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestGetParentNamespaceID(t *testing.T) {
	_, gitlabInstance := setupEmptyTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
	gitlabInstance.AddGroup(TEST_GROUP)
	gitlabInstance.AddProject(TEST_PROJECT)

	tests := []struct {
		name          string
		path          string
		expectedID    int
		expectedError bool
	}{
		{
			name:          "Valid parent path",
			path:          TEST_PROJECT.PathWithNamespace,
			expectedID:    TEST_GROUP.ID,
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
			path:          TEST_GROUP.FullPath,
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
				t.Errorf(EXPECTED_ERROR_MESSAGE, test.expectedError, err)
			}
		})
	}
}

func TestIsVersionGreaterThanThreshold(t *testing.T) {
	tests := []struct {
		name             string
		version          string
		expectedError    bool
		expectedResponse bool
		noApiResponse    bool
	}{
		{
			name:             "Valid version under threshold",
			version:          "15.0.0",
			expectedError:    false,
			expectedResponse: false,
		},
		{
			name:             "Valid version above threshold",
			version:          "17.9.3-ce.0",
			expectedError:    false,
			expectedResponse: true,
		},
		{
			name:             "Invalid version format with 1 dot",
			version:          "invalid.version",
			expectedError:    true,
			expectedResponse: false,
		},
		{
			name:             "Invalid version format with 2 dots",
			version:          "invalid.version.1",
			expectedError:    true,
			expectedResponse: false,
		},
		{
			name:             "Invalid empty version",
			version:          "",
			expectedError:    true,
			expectedResponse: false,
		},
		{
			name:             "No API response",
			version:          "",
			expectedError:    true,
			expectedResponse: false,
			noApiResponse:    true,
		},
	}

	// Iterate over the test cases
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			mux, gitlabInstance := setupEmptyTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
			if !test.noApiResponse {
				mux.HandleFunc("/api/v4/metadata", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
					w.WriteHeader(http.StatusOK)
					_, err := w.Write([]byte(`{"version": "` + test.version + `"}`))
					if err != nil {
						t.Errorf("failed to write response: %v", err)
					}
				})
			}

			thresholdOk, err := gitlabInstance.IsVersionGreaterThanThreshold()
			if (err != nil) != test.expectedError {
				t.Fatalf(EXPECTED_ERROR_MESSAGE, test.expectedError, err)
			}
			if thresholdOk != test.expectedResponse {
				t.Errorf("expected thresholdOk: %v, got: %v", test.expectedResponse, thresholdOk)
			}
		})
	}
}

func TestIsLicensePremium(t *testing.T) {
	tests := []struct {
		name             string
		license          string
		expectedError    bool
		expectedResponse bool
	}{
		{
			name:             "Ultimate tier license",
			license:          ULTIMATE_PLAN,
			expectedError:    false,
			expectedResponse: true,
		},
		{
			name:             "Premium tier license",
			license:          PREMIUM_PLAN,
			expectedError:    false,
			expectedResponse: true,
		},
		{
			name:             "Free tier license",
			license:          "free",
			expectedError:    false,
			expectedResponse: false,
		},
		{
			name:             "Invalid license",
			license:          "invalid",
			expectedError:    false,
			expectedResponse: false,
		},
		{
			name:             "Error API response",
			license:          "",
			expectedError:    true,
			expectedResponse: false,
		},
	}
	// Iterate over the test cases
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			mux, gitlabInstance := setupEmptyTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
			if !test.expectedError {
				mux.HandleFunc("/api/v4/license", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
					w.WriteHeader(http.StatusOK)
					_, err := w.Write([]byte(`{"plan": "` + test.license + `"}`))
					if err != nil {
						t.Errorf("failed to write response: %v", err)
					}
				})
			}

			isPremium, err := gitlabInstance.IsLicensePremium()
			if (err != nil) != test.expectedError {
				t.Fatalf(EXPECTED_ERROR_MESSAGE, test.expectedError, err)
			}
			if isPremium != test.expectedResponse {
				t.Errorf("expected isPremium: %v, got: %v", test.expectedResponse, isPremium)
			}
		})
	}
}
