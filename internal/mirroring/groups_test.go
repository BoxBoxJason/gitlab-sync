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
			gitlabInstance.AddGroup(TEST_GROUP)
			createdGroup, err := gitlabInstance.CreateGroupFromSource(TEST_GROUP_2, &utils.MirroringOptions{
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
			sourceGitlabInstance.AddGroup(TEST_GROUP)
			sourceGitlabInstance.AddGroup(TEST_GROUP_2)
			sourceGitlabInstance.AddProject(TEST_PROJECT)
			sourceGitlabInstance.AddProject(TEST_PROJECT_2)

			_, destinationGitlabInstance := setupTestServer(t, ROLE_DESTINATION, tt.sourceSize)
			destinationGitlabInstance.AddGroup(&gitlab.Group{
				FullPath: "test",
			})

			// Create groups
			err := destinationGitlabInstance.CreateGroups(sourceGitlabInstance, mirrorMapping)
			if err != nil {
				t.Errorf("Unexpected error when creating groups: %v", err)
			}
		})
	}
}

func TestClaimOwnershipToGroup(t *testing.T) {
	_, gitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)

	err := gitlabInstance.ClaimOwnershipToGroup(TEST_GROUP)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
