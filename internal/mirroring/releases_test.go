package mirroring

import (
	"testing"

	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
)

func TestMirrorReleases(t *testing.T) {
	_, sourceGitlabInstance := setupTestServer(t, ROLE_SOURCE, INSTANCE_SIZE_SMALL)
	_, destinationGitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
	t.Run("Mirror Releases", func(t *testing.T) {
		helpers.ResetReported()
		t.Cleanup(helpers.ResetReported)

		destinationGitlabInstance.MirrorReleases(sourceGitlabInstance, TEST_PROJECT, TEST_PROJECT_2)
		if got := helpers.ExitCode(); got != 0 {
			t.Errorf("Unexpected error when mirroring releases: exit code %d", got)
		}
	})
}

func TestFetchProjectReleases(t *testing.T) {
	_, gitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
	t.Run("Fetch Project Releases", func(t *testing.T) {
		releases, err := gitlabInstance.FetchProjectReleases(TEST_PROJECT)
		if err != nil {
			t.Errorf("Unexpected error when fetching project releases: %v", err)
		}
		if len(releases) == 0 {
			t.Error("Expected to fetch at least one release")
		}
	})
}

func TestFetchProjectReleasesTags(t *testing.T) {
	_, gitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
	t.Run("Fetch Project Releases Tags", func(t *testing.T) {
		releasesTags, err := gitlabInstance.FetchProjectReleasesTags(TEST_PROJECT)
		if err != nil {
			t.Errorf("Unexpected error when fetching project releases tags: %v", err)
		}
		if len(releasesTags) == 0 {
			t.Error("Expected to fetch at least one release tag")
		}
	})
}

func TestMirrorRelease(t *testing.T) {
	_, gitlabInstance := setupTestServer(t, ROLE_SOURCE, INSTANCE_SIZE_SMALL)
	t.Run("Mirror Release", func(t *testing.T) {
		release := &gitlab.Release{
			Name:        "Test Release",
			TagName:     "v1.0.0",
			Description: "This is a test release",
		}
		err := gitlabInstance.MirrorRelease(TEST_PROJECT, release)
		if err != nil {
			t.Errorf("Unexpected error when mirroring release: %v", err)
		}
	})
}
