package mirroring

import (
	"gitlab-sync/internal/utils"
	"testing"
)

func TestCheckPathMatchesFilters(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		projectFilters map[string]struct{}
		groupFilters   map[string]struct{}
		expected       bool
	}{
		{
			name: "Project in project filters AND group filters",
			path: TEST_PROJECT.PathWithNamespace,
			projectFilters: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace: {},
			},
			groupFilters: map[string]struct{}{
				TEST_GROUP.FullPath: {},
			},
			expected: true,
		},
		{
			name: "Project in project filters but not in group filters",
			path: TEST_PROJECT.PathWithNamespace,
			projectFilters: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace: {},
			},
			groupFilters: map[string]struct{}{},
			expected:     true,
		},
		{
			name:           "Project not in project filters but in group filters",
			path:           TEST_PROJECT.PathWithNamespace,
			projectFilters: map[string]struct{}{},
			groupFilters: map[string]struct{}{
				TEST_GROUP.FullPath: {},
			},
			expected: true,
		},
		{
			name:           "Project not in project filters and not in group filters",
			path:           TEST_PROJECT.PathWithNamespace,
			projectFilters: map[string]struct{}{},
			groupFilters:   map[string]struct{}{},
			expected:       false,
		},
	}

	// Iterate over the test cases
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Call the function with the test case parameters
			_, got := checkPathMatchesFilters(test.path, &test.projectFilters, &test.groupFilters)

			// Check if the result matches the expected value
			if got != test.expected {
				t.Errorf("expected %v, got %v", test.expected, got)
			}
		})
	}

}

func TestGetParentNamespaceID(t *testing.T) {
	gitlabInstance := setupTestGitlabInstance(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
	gitlabInstance.addGroup(TEST_GROUP)
	gitlabInstance.addProject(TEST_PROJECT)

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
			gotID, err := gitlabInstance.getParentNamespaceID(test.path)

			// Check if the result matches the expected value
			if gotID != test.expectedID {
				t.Errorf("expected %d, got %d", test.expectedID, gotID)
			}

			// Check if an error was expected
			if (err != nil) != test.expectedError {
				t.Errorf("expected error: %v, got: %v", test.expectedError, err)
			}
		})
	}
}

func TestFetchAll(t *testing.T) {

	tests := []struct {
		name          string
		instanceSize  string
		role          string
		expectedError bool
	}{
		{
			name:          "Fetch all Destination with small instance size",
			instanceSize:  INSTANCE_SIZE_SMALL,
			role:          ROLE_DESTINATION,
			expectedError: false,
		},
		{
			name:          "Fetch all Destination with big instance size",
			instanceSize:  INSTANCE_SIZE_BIG,
			role:          ROLE_DESTINATION,
			expectedError: false,
		},
		{
			name:          "Fetch all Source with small instance size",
			instanceSize:  INSTANCE_SIZE_SMALL,
			role:          ROLE_SOURCE,
			expectedError: false,
		},
		{
			name:          "Fetch all Source with big instance size",
			instanceSize:  INSTANCE_SIZE_BIG,
			role:          ROLE_SOURCE,
			expectedError: false,
		},
	}

	projectFilters := map[string]struct{}{
		TEST_PROJECT.PathWithNamespace: {},
	}
	groupFilters := map[string]struct{}{
		TEST_GROUP_2.FullPath: {},
	}
	gitlabMirrorArgs := &utils.MirrorMapping{
		Projects: map[string]*utils.MirroringOptions{},
		Groups:   map[string]*utils.MirroringOptions{},
	}
	gitlabMirrorArgs.Projects[TEST_PROJECT.PathWithNamespace] = &utils.MirroringOptions{
		DestinationPath: TEST_PROJECT.PathWithNamespace,
	}
	gitlabMirrorArgs.Groups[TEST_GROUP_2.FullPath] = &utils.MirroringOptions{
		DestinationPath: TEST_GROUP_2.FullPath,
	}

	// Iterate over the test cases
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, gitlabInstance := setupTestServer(t, test.role, test.instanceSize)

			// Call the function with the test case parameters
			err := gitlabInstance.fetchAll(projectFilters, groupFilters, gitlabMirrorArgs)

			// Check if an error was expected
			if (err != nil) != test.expectedError {
				t.Errorf("expected error: %v, got: %v", test.expectedError, err)
			}

			//Check if the instance cache contains the expected projects and groups
			if _, ok := gitlabInstance.Projects[TEST_PROJECT.PathWithNamespace]; !ok {
				t.Errorf("expected project %s not found in %s %s instance cache", TEST_PROJECT.PathWithNamespace, gitlabInstance.Role, gitlabInstance.InstanceSize)
			}
			if _, ok := gitlabInstance.Groups[TEST_GROUP_2.FullPath]; !ok {
				t.Errorf("expected group %s not found in %s %s instance cache", TEST_GROUP_2.FullPath, gitlabInstance.Role, gitlabInstance.InstanceSize)
			}

		})
	}

}
