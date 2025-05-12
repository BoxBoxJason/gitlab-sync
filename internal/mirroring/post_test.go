package mirroring

import (
	"gitlab-sync/internal/utils"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestCreateGroupFromSource(t *testing.T) {
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
			gitlabInstance.addGroup(TEST_GROUP)
			createdGroup, err := gitlabInstance.createGroupFromSource(TEST_GROUP_2, &utils.MirroringOptions{
				DestinationPath: TEST_GROUP_2.FullPath,
			})
			if err != nil {
				t.Errorf("Unexpected error when creating group: %v", err)
			}
			if createdGroup == nil {
				t.Errorf("Expected created group to be non-nil")
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
			gitlabInstance.addGroup(TEST_GROUP)
			createdProject, err := gitlabInstance.createProjectFromSource(TEST_PROJECT, &utils.MirroringOptions{
				DestinationPath:     TEST_PROJECT.PathWithNamespace,
				Issues:              true,
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

func TestCreateGroups(t *testing.T) {
	tests := []struct {
		name            string
		destinationSize string
		sourceSize      string
	}{
		{
			name:            "Small Destination, Small Source",
			destinationSize: "small",
			sourceSize:      "small",
		},
		{
			name:            "Small Destination, Big Source",
			destinationSize: "small",
			sourceSize:      "big",
		},
		{
			name:            "Big Destination, Small Source",
			destinationSize: "big",
			sourceSize:      "small",
		},
		{
			name:            "Big Destination, Big Source",
			destinationSize: "big",
			sourceSize:      "big",
		},
	}

	mirrorMapping := &utils.MirrorMapping{
		Groups: map[string]*utils.MirroringOptions{
			TEST_GROUP.FullPath: {
				DestinationPath: TEST_GROUP.FullPath,
			},
			TEST_GROUP_2.FullPath: {
				DestinationPath: TEST_GROUP_2.FullPath,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Setup the test server
			_, sourceGitlabInstance := setupTestServer(t, ROLE_SOURCE, tt.destinationSize)
			sourceGitlabInstance.addGroup(TEST_GROUP)
			sourceGitlabInstance.addGroup(TEST_GROUP_2)
			sourceGitlabInstance.addProject(TEST_PROJECT)
			sourceGitlabInstance.addProject(TEST_PROJECT_2)

			_, destinationGitlabInstance := setupTestServer(t, ROLE_DESTINATION, tt.sourceSize)
			destinationGitlabInstance.addGroup(&gitlab.Group{
				FullPath: "test",
			})

			// Create groups
			err := destinationGitlabInstance.createGroups(sourceGitlabInstance, mirrorMapping)
			if err != nil {
				t.Errorf("Unexpected error when creating groups: %v", err)
			}
		})
	}
}

func TestCopyProjectAvatar(t *testing.T) {
	_, sourceGitlabInstance := setupTestServer(t, ROLE_SOURCE, INSTANCE_SIZE_SMALL)
	_, destinationGitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
	t.Run("Copy Project Avatar", func(t *testing.T) {
		err := sourceGitlabInstance.copyProjectAvatar(destinationGitlabInstance, TEST_PROJECT, TEST_PROJECT_2)
		if err != nil {
			t.Errorf("Unexpected error when copying project avatar: %v", err)
		}
	})
}

func TestMirrorReleases(t *testing.T) {
	_, sourceGitlabInstance := setupTestServer(t, ROLE_SOURCE, INSTANCE_SIZE_SMALL)
	_, destinationGitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
	t.Run("Mirror Releases", func(t *testing.T) {
		err := destinationGitlabInstance.mirrorReleases(sourceGitlabInstance, TEST_PROJECT, TEST_PROJECT_2)
		if err != nil {
			t.Errorf("Unexpected error when mirroring releases: %v", err)
		}
	})
}

func TestCreateProjects(t *testing.T) {
	t.Run("Test Create Projects", func(t *testing.T) {
		_, sourceGitlabInstance := setupTestServer(t, ROLE_SOURCE, INSTANCE_SIZE_SMALL)
		sourceGitlabInstance.addGroup(TEST_GROUP)
		sourceGitlabInstance.addProject(TEST_PROJECT)
		_, destinationGitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
		destinationGitlabInstance.addGroup(TEST_GROUP)
		mirrorMapping := &utils.MirrorMapping{
			Projects: map[string]*utils.MirroringOptions{
				TEST_PROJECT.PathWithNamespace: {
					DestinationPath:     TEST_PROJECT.PathWithNamespace,
					CI_CD_Catalog:       false,
					Issues:              true,
					MirrorTriggerBuilds: false,
					Visibility:          "public",
					MirrorReleases:      true,
				},
			},
		}
		err := destinationGitlabInstance.createProjects(sourceGitlabInstance, mirrorMapping)
		if err != nil {
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
		err := gitlabInstance.addProjectToCICDCatalog(TEST_PROJECT)
		if err != nil {
			t.Errorf("Unexpected error when adding project to CI/CD catalog: %v", err)
		}
	})
}
