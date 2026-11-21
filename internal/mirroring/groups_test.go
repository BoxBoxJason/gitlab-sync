package mirroring

import (
	"net/http"
	"reflect"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

func TestCreateGroupFromSource(t *testing.T) {
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
			createdGroup, err := gitlabInstance.CreateGroupFromSource(testGroup2, &mirrorcfg.MirroringOptions{
				DestinationPath: testGroup2.FullPath,
			})
			if err != nil {
				t.Errorf("Unexpected error when creating group: %v", err)
			}
			if createdGroup == nil {
				t.Error("Expected created group to be non-nil")
			}
		})
	}
}

func TestCreateGroups(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
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
			testGroup.FullPath: {
				DestinationPath: testGroup.FullPath,
			},
			testGroup2.FullPath: {
				DestinationPath: testGroup2.FullPath,
			},
		},
	}
	for _, tt := range tests { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
		t.Run(tt.name, func(t *testing.T) {
			// Not parallel: the assertion below reads the process-wide error tally.
			helpers.ResetReported()
			t.Cleanup(helpers.ResetReported)

			// Setup the test server
			sourceGitlabInstance := setupTestServer(t, RoleSource, tt.destinationSize)
			sourceGitlabInstance.AddGroup(testGroup)
			sourceGitlabInstance.AddGroup(testGroup2)
			sourceGitlabInstance.AddProject(testProject)
			sourceGitlabInstance.AddProject(testProject2)

			destinationGitlabInstance := setupTestServer(t, RoleDestination, tt.sourceSize)
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
	t.Parallel()

	gitlabInstance := setupTestServer(t, RoleDestination, InstanceSizeSmall)

	err := gitlabInstance.ClaimOwnershipToGroup(testGroup)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestCreateGroupFromSourceClaimOwnershipOption(t *testing.T) {
	t.Parallel()

	tests := []struct {
		claimOwnership       *bool
		name                 string
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
			t.Parallel()

			mux, gitlabInstance := setupEmptyTestServer(t, RoleDestination)
			memberClaims := 0

			mux.HandleFunc("/api/v4/groups", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				w.Header().Set(headerContentType, headerAccept)
				w.WriteHeader(http.StatusCreated)
				writeBody(w, testGroup2String)
			})

			mux.HandleFunc("/api/v4/groups/2/members", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				memberClaims++
				w.Header().Set(headerContentType, headerAccept)
				w.WriteHeader(http.StatusCreated)
				writeBody(w, `{"id": 1}`)
			})

			createdGroup, err := gitlabInstance.CreateGroupFromSource(testGroup2, &mirrorcfg.MirroringOptions{
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

func TestReverseGroupMirrorMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                     string
		mirrorMapping            *mirrorcfg.MirrorMapping
		expectedReversedMap      map[string]string
		expectedDestinationPaths []string
	}{
		{
			name: "normal mapping",
			mirrorMapping: &mirrorcfg.MirrorMapping{
				Groups: map[string]*mirrorcfg.MirroringOptions{
					testGroup.FullPath:  {DestinationPath: testGroup.FullPath},
					testGroup2.FullPath: {DestinationPath: testGroup2.FullPath},
				},
			},
			expectedReversedMap: map[string]string{
				testGroup.FullPath:  testGroup.FullPath,
				testGroup2.FullPath: testGroup2.FullPath,
			},
			expectedDestinationPaths: []string{testGroup.FullPath, testGroup2.FullPath},
		},
		{
			name:                     "nil mirror mapping",
			mirrorMapping:            nil,
			expectedReversedMap:      nil,
			expectedDestinationPaths: []string{},
		},
		{
			name: "empty groups in mirror mapping",
			mirrorMapping: &mirrorcfg.MirrorMapping{
				Groups: map[string]*mirrorcfg.MirroringOptions{},
			},
			expectedReversedMap:      map[string]string{},
			expectedDestinationPaths: []string{},
		},
		{
			name: "unsorted input paths",
			mirrorMapping: &mirrorcfg.MirrorMapping{
				Groups: map[string]*mirrorcfg.MirroringOptions{
					testGroup2.FullPath: {DestinationPath: testGroup2.FullPath},
					testGroup.FullPath:  {DestinationPath: testGroup.FullPath},
				},
			},
			expectedReversedMap: map[string]string{
				testGroup.FullPath:  testGroup.FullPath,
				testGroup2.FullPath: testGroup2.FullPath,
			},
			expectedDestinationPaths: []string{testGroup.FullPath, testGroup2.FullPath},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := &GitlabInstance{
				Groups: nil,
			}

			actual := g.reverseGroupMirrorMap(tt.mirrorMapping)
			actualReversedMap, actualDestinationPaths := actual.sourceByDestination, actual.destinationPaths

			if !reflect.DeepEqual(actualReversedMap, tt.expectedReversedMap) {
				t.Errorf("reversedMirrorMap = %v, want %v", actualReversedMap, tt.expectedReversedMap)
			}

			if !reflect.DeepEqual(actualDestinationPaths, tt.expectedDestinationPaths) {
				t.Errorf("destinationGroupPaths = %v, want %v", actualDestinationPaths, tt.expectedDestinationPaths)
			}
		})
	}
}
