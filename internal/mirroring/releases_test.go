package mirroring

import (
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
