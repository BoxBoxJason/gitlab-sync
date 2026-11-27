package mirroring

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

func TestMirrorReleases(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
	sourceGitlabInstance := setupTestServer(t, RoleSource, InstanceSizeSmall)
	destinationGitlabInstance := setupTestServer(t, RoleDestination, InstanceSizeSmall)
	t.Run("Mirror Releases", func(t *testing.T) {
		helpers.ResetReported()
		t.Cleanup(helpers.ResetReported)

		destinationGitlabInstance.MirrorReleases(sourceGitlabInstance, testProject, testProject2, ReleasesMirroringOptions{})
		if got := helpers.ExitCode(); got != 0 {
			t.Errorf("Unexpected error when mirroring releases: exit code %d", got)
		}
	})
}

func TestFetchProjectReleases(t *testing.T) {
	t.Parallel()

	gitlabInstance := setupTestServer(t, RoleDestination, InstanceSizeSmall)
	t.Run("Fetch Project Releases", func(t *testing.T) {
		t.Parallel()

		releases, err := gitlabInstance.FetchProjectReleases(testProject)
		if err != nil {
			t.Errorf("Unexpected error when fetching project releases: %v", err)
		}
		if len(releases) == 0 {
			t.Error("Expected to fetch at least one release")
		}
	})
}

func TestFetchProjectReleasesTags(t *testing.T) {
	t.Parallel()

	gitlabInstance := setupTestServer(t, RoleDestination, InstanceSizeSmall)
	t.Run("Fetch Project Releases Tags", func(t *testing.T) {
		t.Parallel()

		releasesTags, err := gitlabInstance.FetchProjectReleasesTags(testProject)
		if err != nil {
			t.Errorf("Unexpected error when fetching project releases tags: %v", err)
		}
		if len(releasesTags) == 0 {
			t.Error("Expected to fetch at least one release tag")
		}
	})
}

func TestMirrorRelease(t *testing.T) {
	t.Parallel()

	gitlabInstance := setupTestServer(t, RoleSource, InstanceSizeSmall)
	t.Run("Mirror Release", func(t *testing.T) {
		t.Parallel()

		release := &gitlab.Release{
			Name:        "Test Release",
			TagName:     "v1.0.0",
			Description: "This is a test release",
		}
		err := gitlabInstance.MirrorRelease(testProject, release)
		if err != nil {
			t.Errorf("Unexpected error when mirroring release: %v", err)
		}
	})
}

func TestDeleteStaleReleaseRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		remaining string
		status    int
		wantError bool
	}{
		{name: "already deleted with its tag", status: http.StatusForbidden, remaining: `[]`},
		{name: "already deleted, not found", status: http.StatusNotFound, remaining: `[]`},
		{name: "forbidden", status: http.StatusForbidden, remaining: `[{"tag_name": "v1"}]`, wantError: true},
		{name: "another failure", status: http.StatusBadRequest, remaining: `[]`, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mux, gitlabInstance := setupEmptyTestServer(t, RoleDestination)
			mux.HandleFunc(fmt.Sprintf("DELETE /api/v4/projects/%d/releases/{tag}", testProject.ID), func(w http.ResponseWriter, r *http.Request) {
				if got := r.PathValue("tag"); got != "v1" {
					t.Errorf("deleted the release %q, want v1", got)
				}

				writeJSONResponse(w, tt.status, `{"message": "refused"}`)
			})
			serveReleases(mux, testProject, tt.remaining)

			err := gitlabInstance.DeleteStaleRelease(testProject, &gitlab.Release{TagName: "v1"}, nil)
			if (err != nil) != tt.wantError {
				t.Errorf("DeleteStaleRelease() error = %v, want an error: %t", err, tt.wantError)
			}
		})
	}
}
