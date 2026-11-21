package mirroring

import (
	"testing"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

func TestFetchAll(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
	tests := []struct {
		name          string
		instanceSize  string
		role          string
		expectedError bool
	}{
		{
			name:          "Fetch all Destination with small instance size",
			instanceSize:  InstanceSizeSmall,
			role:          RoleDestination,
			expectedError: false,
		},
		{
			name:          "Fetch all Destination with big instance size",
			instanceSize:  InstanceSizeBig,
			role:          RoleDestination,
			expectedError: false,
		},
		{
			name:          "Fetch all Source with small instance size",
			instanceSize:  InstanceSizeSmall,
			role:          RoleSource,
			expectedError: false,
		},
		{
			name:          "Fetch all Source with big instance size",
			instanceSize:  InstanceSizeBig,
			role:          RoleSource,
			expectedError: false,
		},
	}

	projectFilters := map[string]struct{}{
		testProject.PathWithNamespace: {},
	}
	groupFilters := map[string]struct{}{
		testGroup2.FullPath: {},
	}
	gitlabMirrorArgs := &mirrorcfg.MirrorMapping{
		Projects: map[string]*mirrorcfg.MirroringOptions{},
		Groups:   map[string]*mirrorcfg.MirroringOptions{},
	}
	gitlabMirrorArgs.Projects[testProject.PathWithNamespace] = &mirrorcfg.MirroringOptions{
		DestinationPath: testProject.PathWithNamespace,
	}
	gitlabMirrorArgs.Groups[testGroup2.FullPath] = &mirrorcfg.MirroringOptions{
		DestinationPath: testGroup2.FullPath,
	}

	// Iterate over the test cases
	for _, test := range tests { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
		t.Run(test.name, func(t *testing.T) {
			// Not parallel: the assertion below reads the process-wide error tally.
			helpers.ResetReported()
			t.Cleanup(helpers.ResetReported)

			gitlabInstance := setupTestServer(t, test.role, test.instanceSize)

			// Call the function with the test case parameters
			gitlabInstance.FetchAll(projectFilters, groupFilters, gitlabMirrorArgs)

			// Check if an error was expected
			if (helpers.ExitCode() != 0) != test.expectedError {
				t.Errorf(expectedErrorMessage, test.expectedError, helpers.ExitCode())
			}

			// Check if the instance cache contains the expected projects and groups
			if _, ok := gitlabInstance.Projects[testProject.PathWithNamespace]; !ok {
				t.Errorf("expected project %s not found in %s %s instance cache", testProject.PathWithNamespace, gitlabInstance.Role, gitlabInstance.InstanceSize)
			}
			if _, ok := gitlabInstance.Groups[testGroup2.FullPath]; !ok {
				t.Errorf("expected group %s not found in %s %s instance cache", testGroup2.FullPath, gitlabInstance.Role, gitlabInstance.InstanceSize)
			}
		})
	}
}

func TestFetchAndProcessProjectsBigInstance(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
	const customDestination = "custom/destination/project"

	tests := []struct {
		projectFilters             map[string]struct{}
		expectedProjects           map[string]struct{}
		name                       string
		role                       string
		expectedMappingDestination string
		expectError                bool
	}{
		{
			name: "Test with source role, 1 project only, no error",
			role: RoleSource,
			projectFilters: map[string]struct{}{
				testProject.PathWithNamespace: {},
			},
			expectedProjects: map[string]struct{}{
				testProject.PathWithNamespace: {},
			},
			expectedMappingDestination: customDestination,
		},
		{
			name: "Test with destination role, 1 project only, no error",
			role: RoleDestination,
			projectFilters: map[string]struct{}{
				testProject.PathWithNamespace: {},
			},
			expectedProjects: map[string]struct{}{
				testProject.PathWithNamespace: {},
			},
		},
		{
			name: "Test with source role, 2 projects, no error",
			role: RoleSource,
			projectFilters: map[string]struct{}{
				testProject.PathWithNamespace:  {},
				testProject2.PathWithNamespace: {},
			},
			expectedProjects: map[string]struct{}{
				testProject.PathWithNamespace:  {},
				testProject2.PathWithNamespace: {},
			},
			expectedMappingDestination: customDestination,
		},
		{
			name: "Test with destination role, 2 projects, no error",
			role: RoleDestination,
			projectFilters: map[string]struct{}{
				testProject.PathWithNamespace:  {},
				testProject2.PathWithNamespace: {},
			},
			expectedProjects: map[string]struct{}{
				testProject.PathWithNamespace:  {},
				testProject2.PathWithNamespace: {},
			},
		},
		{
			name: "Test with source role, 1 project, 1 error",
			role: RoleSource,
			projectFilters: map[string]struct{}{
				testProject.PathWithNamespace:    {},
				invalidProject.PathWithNamespace: {},
			},
			expectedProjects: map[string]struct{}{
				testProject.PathWithNamespace: {},
			},
			expectError:                true,
			expectedMappingDestination: customDestination,
		},
	}

	for _, test := range tests { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
		t.Run(test.name, func(t *testing.T) {
			// Not parallel: the assertion below reads the process-wide error tally.
			helpers.ResetReported()
			t.Cleanup(helpers.ResetReported)

			gitlabMirrorArgs := &mirrorcfg.MirrorMapping{
				Projects: map[string]*mirrorcfg.MirroringOptions{
					testProject.PathWithNamespace: {
						DestinationPath: customDestination,
					},
				},
			}

			gitlabInstance := setupTestServer(t, test.role, InstanceSizeBig)

			gitlabInstance.FetchAndProcessProjectsBigInstance(test.projectFilters, gitlabMirrorArgs)
			if (helpers.ExitCode() != 0) != test.expectError {
				t.Fatalf("Expected error: %v, got exit code: %d", test.expectError, helpers.ExitCode())
			}
			if len(gitlabInstance.Projects) != len(test.expectedProjects) {
				t.Fatalf("Expected %d projects, got %d", len(test.expectedProjects), len(gitlabInstance.Projects))
			}
			for project := range test.expectedProjects {
				if _, ok := gitlabInstance.Projects[project]; !ok {
					t.Errorf("Expected project %s to be in the cache", project)
				}
			}

			// For source-role cases the configured destination path must not be overwritten.
			if test.expectedMappingDestination != "" {
				opts, ok := gitlabMirrorArgs.GetProject(testProject.PathWithNamespace)
				if !ok {
					t.Fatalf("expected %q to remain in mirror mapping after FetchAndProcessProjectsBigInstance", testProject.PathWithNamespace)
				}
				if opts.DestinationPath != test.expectedMappingDestination {
					t.Errorf("mirror mapping destination path: got %q, want %q", opts.DestinationPath, test.expectedMappingDestination)
				}
			}
		})
	}
}
