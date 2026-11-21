package mirroring

import (
	"net/http"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
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
			// Setup the test server
			_, gitlabInstance := setupTestServer(t, tt.role, tt.instanceSize)
			gitlabInstance.AddGroup(TEST_GROUP)
			createdGroup, err := gitlabInstance.CreateGroupFromSource(TEST_GROUP_2, &mirrorcfg.MirroringOptions{
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

	mirrorMapping := &mirrorcfg.MirrorMapping{
		Groups: map[string]*mirrorcfg.MirroringOptions{
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
			// Not parallel: the assertion below reads the process-wide error tally.
			helpers.ResetReported()
			t.Cleanup(helpers.ResetReported)

			// Setup the test server
			_, sourceGitlabInstance := setupTestServer(t, RoleSource, tt.destinationSize)
			sourceGitlabInstance.AddGroup(TEST_GROUP)
			sourceGitlabInstance.AddGroup(TEST_GROUP_2)
			sourceGitlabInstance.AddProject(TEST_PROJECT)
			sourceGitlabInstance.AddProject(TEST_PROJECT_2)

			_, destinationGitlabInstance := setupTestServer(t, RoleDestination, tt.sourceSize)
			destinationGitlabInstance.AddGroup(&gitlab.Group{
				FullPath: "test",
			})

			// Create groups
			destinationGitlabInstance.CreateGroups(sourceGitlabInstance, mirrorMapping)
			if got := helpers.ExitCode(); got != 0 {
				t.Errorf("Unexpected error when creating groups: exit code %d", got)
			}
		})
	}
}

func TestClaimOwnershipToGroup(t *testing.T) {
	_, gitlabInstance := setupTestServer(t, RoleDestination, InstanceSizeSmall)

	err := gitlabInstance.ClaimOwnershipToGroup(TEST_GROUP)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestCreateGroupFromSourceClaimOwnershipOption(t *testing.T) {
	tests := []struct {
		name                 string
		claimOwnership       *bool
		expectedMemberClaims int
	}{
		{
			name:                 "claim ownership omitted defaults to no claim",
			claimOwnership:       nil,
			expectedMemberClaims: 0,
		},
		{
			name:                 "claim ownership false does not claim",
			claimOwnership:       new(false),
			expectedMemberClaims: 0,
		},
		{
			name:                 "claim ownership true claims ownership",
			claimOwnership:       new(true),
			expectedMemberClaims: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mux, gitlabInstance := setupEmptyTestServer(t, RoleDestination, InstanceSizeSmall)
			memberClaims := 0

			mux.HandleFunc("/api/v4/groups", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte(TEST_GROUP_2_STRING))
			})

			mux.HandleFunc("/api/v4/groups/2/members", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				memberClaims++
				w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte(`{"id": 1}`))
			})

			createdGroup, err := gitlabInstance.CreateGroupFromSource(TEST_GROUP_2, &mirrorcfg.MirroringOptions{
				DestinationPath: "group2",
				ClaimOwnership:  tc.claimOwnership,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if createdGroup == nil {
				t.Fatal("expected created group to be non-nil")
			}
			if memberClaims != tc.expectedMemberClaims {
				t.Fatalf("expected %d ownership claims, got %d", tc.expectedMemberClaims, memberClaims)
			}
		})
	}
}
