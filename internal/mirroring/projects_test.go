package mirroring

import (
	"gitlab-sync/internal/utils"
	"testing"
)

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
			err := gitlabInstance.FetchAll(projectFilters, groupFilters, gitlabMirrorArgs)

			// Check if an error was expected
			if (err != nil) != test.expectedError {
				t.Errorf(EXPECTED_ERROR_MESSAGE, test.expectedError, err)
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

			err := gitlabInstance.FetchAndProcessProjectsBigInstance(&test.projectFilters, gitlabMirrorArgs)
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

func TestCreateProjectFromSource(t *testing.T) {
	tests := []struct {
		name         string
		instanceSize string
		role         string
	}{
		{
			name:         "Small Destination Instance",
			instanceSize: "small",
			role:         ROLE_DESTINATION,
		},
		{
			name:         "Small Source Instance",
			instanceSize: "small",
			role:         ROLE_SOURCE,
		},
		{
			name:         "Big Destination Instance",
			instanceSize: "big",
			role:         ROLE_DESTINATION,
		},
		{
			name:         "Big Source Instance",
			instanceSize: "big",
			role:         ROLE_SOURCE,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup the test server
			_, gitlabInstance := setupTestServer(t, tt.role, tt.instanceSize)
			gitlabInstance.AddGroup(TEST_GROUP)
			createdProject, err := gitlabInstance.CreateProjectFromSource(TEST_PROJECT, &utils.MirroringOptions{
				DestinationPath:     TEST_PROJECT.PathWithNamespace,
				MirrorIssues:        true,
				MirrorReleases:      true,
				MirrorTriggerBuilds: true,
				Visibility:          "public",
				CI_CD_Catalog:       true,
			})
			if err != nil {
				t.Errorf("Unexpected error when creating project: %v", err)
			}
			if createdProject == nil {
				t.Fatal("Expected created project to be non-nil")
			}
			if createdProject.PathWithNamespace != TEST_PROJECT.PathWithNamespace {
				t.Errorf("Expected created project path to be %s, got %s", TEST_PROJECT.PathWithNamespace, createdProject.PathWithNamespace)
			}
		})
	}

}

func TestCopyProjectAvatar(t *testing.T) {
	_, sourceGitlabInstance := setupTestServer(t, ROLE_SOURCE, INSTANCE_SIZE_SMALL)
	_, destinationGitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
	t.Run("Copy Project Avatar", func(t *testing.T) {
		err := sourceGitlabInstance.CopyProjectAvatar(destinationGitlabInstance, TEST_PROJECT, TEST_PROJECT_2)
		if err != nil {
			t.Errorf("Unexpected error when copying project avatar: %v", err)
		}
	})
}

func TestCreateProjects(t *testing.T) {
	t.Run("Test Create Projects", func(t *testing.T) {
		_, sourceGitlabInstance := setupTestServer(t, ROLE_SOURCE, INSTANCE_SIZE_SMALL)
		sourceGitlabInstance.AddGroup(TEST_GROUP)
		sourceGitlabInstance.AddProject(TEST_PROJECT)
		_, destinationGitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
		destinationGitlabInstance.AddGroup(TEST_GROUP)
		destinationGitlabInstance.PullMirrorAvailable = true
		mirrorMapping := &utils.MirrorMapping{
			Projects: map[string]*utils.MirroringOptions{
				TEST_PROJECT.PathWithNamespace: {
					DestinationPath:     TEST_PROJECT.PathWithNamespace,
					CI_CD_Catalog:       false,
					MirrorIssues:        true,
					MirrorTriggerBuilds: false,
					Visibility:          "public",
					MirrorReleases:      true,
				},
			},
		}
		err := destinationGitlabInstance.CreateProjects(sourceGitlabInstance, mirrorMapping)
		if len(err) > 0 {
			t.Errorf("Unexpected error when creating projects: %v", err)
		}
		if len(destinationGitlabInstance.Projects) == 0 {
			t.Errorf("Expected projects to be created, but none were found")
		}
	})
}

func TestAddProjectToCICDCatalog(t *testing.T) {
	_, gitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
	t.Run("Add Project to CI/CD Catalog", func(t *testing.T) {
		err := gitlabInstance.AddProjectToCICDCatalog(TEST_PROJECT)
		if err != nil {
			t.Errorf("Unexpected error when adding project to CI/CD catalog: %v", err)
		}
	})
}

func TestClaimOwnershipToProject(t *testing.T) {
	_, gitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)

	err := gitlabInstance.ClaimOwnershipToProject(TEST_PROJECT)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
