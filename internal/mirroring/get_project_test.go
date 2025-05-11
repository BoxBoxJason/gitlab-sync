package mirroring

import (
	"gitlab-sync/internal/utils"
	"testing"
)

func TestFetchAndProcessProjectsBigInstance(t *testing.T) {
	tests := []struct {
		name             string
		role             string
		projectFilters   map[string]struct{}
		expectedProjects map[string]struct{}
		expectError      bool
	}{
		{
			name: "Test with source role, 1 project only, no error",
			role: ROLE_SOURCE,
			projectFilters: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace: {},
			},
			expectedProjects: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace: {},
			},
		},
		{
			name: "Test with destination role, 1 project only, no error",
			role: ROLE_DESTINATION,
			projectFilters: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace: {},
			},
			expectedProjects: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace: {},
			},
		},
		{
			name: "Test with source role, 2 projects, no error",
			role: ROLE_SOURCE,
			projectFilters: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace:   {},
				TEST_PROJECT_2.PathWithNamespace: {},
			},
			expectedProjects: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace:   {},
				TEST_PROJECT_2.PathWithNamespace: {},
			},
		},
		{
			name: "Test with destination role, 2 projects, no error",
			role: ROLE_DESTINATION,
			projectFilters: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace:   {},
				TEST_PROJECT_2.PathWithNamespace: {},
			},
			expectedProjects: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace:   {},
				TEST_PROJECT_2.PathWithNamespace: {},
			},
		},
		{
			name: "Test with source role, 1 project, 1 error",
			role: ROLE_SOURCE,
			projectFilters: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace:    {},
				INVALID_PROJECT.PathWithNamespace: {},
			},
			expectedProjects: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace: {},
			},
			expectError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			gitlabMirrorArgs := &utils.MirrorMapping{
				Projects: map[string]*utils.MirroringOptions{
					TEST_PROJECT.PathWithNamespace: {
						DestinationPath: TEST_PROJECT.PathWithNamespace,
					},
				},
			}

			_, gitlabInstance := setupTestServer(t, test.role, INSTANCE_SIZE_BIG)

			err := gitlabInstance.fetchAndProcessProjectsBigInstance(&test.projectFilters, gitlabMirrorArgs)
			if (err != nil) != test.expectError {
				t.Fatalf("Expected error: %v, got: %v", test.expectError, err)
			}
			if len(gitlabInstance.Projects) != len(test.expectedProjects) {
				t.Fatalf("Expected %d projects, got %d", len(test.expectedProjects), len(gitlabInstance.Projects))
			}
			for project := range test.expectedProjects {
				if _, ok := gitlabInstance.Projects[project]; !ok {
					t.Errorf("Expected project %s to be in the cache", project)
				}
			}
		})
	}

}
