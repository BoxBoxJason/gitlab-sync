package mirroring

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

func TestCreateProjectClaimOwnershipOption(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
	tests := []struct {
		claimOwnership       *bool
		name                 string
		expectedMemberClaims int
		preExisting          bool
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
			name:                 "claim ownership true claims ownership when the project is created",
			claimOwnership:       new(true),
			expectedMemberClaims: 1,
		},
		{
			name:                 "claim ownership true reclaims ownership on an already existing project",
			claimOwnership:       new(true),
			preExisting:          true,
			expectedMemberClaims: 1,
		},
	}

	for _, tc := range tests { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
		t.Run(tc.name, func(t *testing.T) {
			sourceGitlabInstance := setupTestServer(t, RoleSource, InstanceSizeSmall)
			sourceGitlabInstance.AddProject(testProject2)

			mux, destinationGitlabInstance := setupEmptyTestServer(t, RoleDestination)
			destinationGitlabInstance.PullMirrorAvailable = true
			destinationGitlabInstance.AddGroup(testGroup2)

			memberClaims := 0

			mux.HandleFunc("/api/v4/projects", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				writeJSONResponse(w, http.StatusCreated, testProject2String)
			})
			mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d", testProject2.ID), func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet, http.MethodPut:
					writeJSONResponse(w, http.StatusOK, testProject2String)
				default:
					writeMethodNotAllowed(w)
				}
			})
			mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d/mirror/pull", testProject2.ID), func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					writeMethodNotAllowed(w)
					return
				}
				writeJSONResponse(w, http.StatusOK, "{}")
			})
			// The edit-member endpoint is intentionally left unregistered so ClaimOwnershipToProject's
			// EditProjectMember attempt 404s and falls back to AddProjectMember below.
			mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d/members", testProject2.ID), func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					writeMethodNotAllowed(w)
					return
				}
				memberClaims++
				writeJSONResponse(w, http.StatusCreated, `{"id": 1, "access_level": 50}`)
			})

			if tc.preExisting {
				destinationGitlabInstance.AddProject(testProject2)
			}

			helpers.ResetReported()
			t.Cleanup(helpers.ResetReported)

			createdProject := destinationGitlabInstance.CreateProject(testProject2.PathWithNamespace, &mirrorcfg.MirroringOptions{
				DestinationPath: testProject2.PathWithNamespace,
				ClaimOwnership:  tc.claimOwnership,
			}, sourceGitlabInstance)
			if got := helpers.ExitCode(); got != 0 {
				t.Fatalf("unexpected error: exit code %d", got)
			}
			if createdProject == nil {
				t.Fatal("expected created project to be non-nil")
			}
			if memberClaims != tc.expectedMemberClaims {
				t.Fatalf("expected %d ownership claims, got %d", tc.expectedMemberClaims, memberClaims)
			}
		})
	}
}

func TestCreateProjectFromSourceDoesNotRequestMirror(t *testing.T) {
	t.Parallel()

	// GitLab validates mirror=true against the presence of an import URL and a
	// mirror user, neither of which exists when the project is being created.
	for _, pullMirrorAvailable := range []bool{false, true} {
		t.Run(fmt.Sprintf("pullMirrorAvailable=%v", pullMirrorAvailable), func(t *testing.T) {
			t.Parallel()

			mux, gitlabInstance := setupEmptyTestServer(t, RoleDestination)
			gitlabInstance.PullMirrorAvailable = pullMirrorAvailable
			gitlabInstance.AddGroup(testGroup)

			var createBody map[string]any

			mux.HandleFunc("/api/v4/projects", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					w.WriteHeader(http.StatusMethodNotAllowed)

					return
				}

				if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
					t.Errorf("failed to decode project creation body: %v", err)
				}

				w.Header().Set(headerContentType, headerAccept)
				w.WriteHeader(http.StatusCreated)
				writeBody(w, testProjectString)
			})

			_, err := gitlabInstance.CreateProjectFromSource(testProject, &mirrorcfg.MirroringOptions{
				DestinationPath:     testProject.PathWithNamespace,
				MirrorTriggerBuilds: new(true),
			})
			if err != nil {
				t.Fatalf("unexpected error when creating project: %v", err)
			}

			if _, ok := createBody["mirror"]; ok {
				t.Errorf("project creation must not send the mirror attribute, got %v", createBody["mirror"])
			}

			_, sentTriggerBuilds := createBody["mirror_trigger_builds"]
			if sentTriggerBuilds != pullMirrorAvailable {
				t.Errorf("expected mirror_trigger_builds to be sent = %v, got %v", pullMirrorAvailable, sentTriggerBuilds)
			}
		})
	}
}
