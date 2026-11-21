package mirroring

import (
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

func TestCreateProjectFromSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		instanceSize string
		role         string
	}{
		{
			name:         "Small Destination Instance",
			instanceSize: "small",
			role:         RoleDestination,
		},
		{
			name:         "Small Source Instance",
			instanceSize: "small",
			role:         RoleSource,
		},
		{
			name:         "Big Destination Instance",
			instanceSize: "big",
			role:         RoleDestination,
		},
		{
			name:         "Big Source Instance",
			instanceSize: "big",
			role:         RoleSource,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup the test server
			gitlabInstance := setupTestServer(t, tt.role, tt.instanceSize)
			gitlabInstance.AddGroup(testGroup)
			createdProject, err := gitlabInstance.CreateProjectFromSource(testProject, &mirrorcfg.MirroringOptions{
				DestinationPath:     testProject.PathWithNamespace,
				MirrorIssues:        new(true),
				MirrorReleases:      new(true),
				MirrorTriggerBuilds: new(true),
				Visibility:          new("public"),
				CICDCatalog:         new(true),
			})
			if err != nil {
				t.Errorf("Unexpected error when creating project: %v", err)
			}
			if createdProject == nil {
				t.Fatal("Expected created project to be non-nil")
			}
			if createdProject.PathWithNamespace != testProject.PathWithNamespace {
				t.Errorf("Expected created project path to be %s, got %s", testProject.PathWithNamespace, createdProject.PathWithNamespace)
			}
		})
	}
}

func TestCreateProjectFromSourceWithMinimalOptions(t *testing.T) {
	t.Parallel()

	gitlabInstance := setupTestServer(t, RoleDestination, InstanceSizeSmall)
	gitlabInstance.AddGroup(testGroup)

	createdProject, err := gitlabInstance.CreateProjectFromSource(testProject, &mirrorcfg.MirroringOptions{
		DestinationPath: testProject.PathWithNamespace,
	})
	if err != nil {
		t.Fatalf("unexpected error when creating project with minimal options: %v", err)
	}
	if createdProject == nil {
		t.Fatal("expected created project to be non-nil")
	}
	if createdProject.PathWithNamespace != testProject.PathWithNamespace {
		t.Errorf("expected created project path to be %s, got %s", testProject.PathWithNamespace, createdProject.PathWithNamespace)
	}
}

func TestCopyProjectAvatar(t *testing.T) {
	t.Parallel()

	sourceGitlabInstance := setupTestServer(t, RoleSource, InstanceSizeSmall)
	destinationGitlabInstance := setupTestServer(t, RoleDestination, InstanceSizeSmall)

	t.Run("Copy Project Avatar", func(t *testing.T) {
		t.Parallel()

		sourceProject := *testProject2
		sourceProject.AvatarURL = "http://gitlab.example.com/uploads/avatar.png"

		err := sourceGitlabInstance.CopyProjectAvatar(destinationGitlabInstance, testProject, &sourceProject)
		if err != nil {
			t.Errorf("Unexpected error when copying project avatar: %v", err)
		}
	})

	t.Run("Source project without an avatar is skipped", func(t *testing.T) {
		t.Parallel()

		// The avatar endpoint answers 404 for avatar-less projects, so the download
		// must not even be attempted.
		_, isolatedSource := setupEmptyTestServer(t, RoleSource)

		err := isolatedSource.CopyProjectAvatar(destinationGitlabInstance, testProject, testProject2)
		if err != nil {
			t.Errorf("Unexpected error when skipping avatar copy: %v", err)
		}
	})
}

func TestCreateProjects(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
	t.Run("Test Create Projects", func(t *testing.T) {
		helpers.ResetReported()
		t.Cleanup(helpers.ResetReported)

		sourceGitlabInstance := setupTestServer(t, RoleSource, InstanceSizeSmall)
		sourceGitlabInstance.AddGroup(testGroup)
		sourceGitlabInstance.AddProject(testProject)
		destinationGitlabInstance := setupTestServer(t, RoleDestination, InstanceSizeSmall)
		destinationGitlabInstance.AddGroup(testGroup)
		destinationGitlabInstance.PullMirrorAvailable = true
		mirrorMapping := &mirrorcfg.MirrorMapping{
			Projects: map[string]*mirrorcfg.MirroringOptions{
				testProject.PathWithNamespace: {
					DestinationPath:     testProject.PathWithNamespace,
					CICDCatalog:         new(false),
					MirrorIssues:        new(true),
					MirrorTriggerBuilds: new(false),
					Visibility:          new("public"),
					MirrorReleases:      new(true),
				},
			},
		}
		destinationGitlabInstance.CreateProjects(sourceGitlabInstance, mirrorMapping)
		if got := helpers.ExitCode(); got != 0 {
			t.Errorf("Unexpected error when creating projects: exit code %d", got)
		}
		if len(destinationGitlabInstance.Projects) == 0 {
			t.Error("Expected projects to be created, but none were found")
		}
	})
}

func TestAddProjectToCICDCatalog(t *testing.T) {
	t.Parallel()

	gitlabInstance := setupTestServer(t, RoleDestination, InstanceSizeSmall)
	t.Run("Add Project to CI/CD Catalog", func(t *testing.T) {
		t.Parallel()

		err := gitlabInstance.AddProjectToCICDCatalog(testProject)
		if err != nil {
			t.Errorf("Unexpected error when adding project to CI/CD catalog: %v", err)
		}
	})

	t.Run("Skip Add Project to CI/CD Catalog when already enabled", func(t *testing.T) {
		t.Parallel()

		// This project ID has no registered endpoint on the test server, so an
		// unexpected error here would reveal that the skip check failed to
		// prevent the API call.
		alreadyEnabledProject := &gitlab.Project{
			ID:                 999999,
			PathWithNamespace:  "test/group/unregistered-project",
			CICDCatalogEnabled: true,
		}

		err := gitlabInstance.AddProjectToCICDCatalog(alreadyEnabledProject)
		if err != nil {
			t.Errorf("Unexpected error when project is already in the CI/CD catalog: %v", err)
		}
	})
}

func TestClaimOwnershipToProject(t *testing.T) {
	t.Parallel()

	gitlabInstance := setupTestServer(t, RoleDestination, InstanceSizeSmall)

	err := gitlabInstance.ClaimOwnershipToProject(testProject)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
