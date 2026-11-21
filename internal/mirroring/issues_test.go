package mirroring

import (
	"testing"

	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

func TestFetchProjectIssues(t *testing.T) {
	t.Parallel()

	gitlabInstance := setupTestServer(t, RoleDestination, InstanceSizeSmall)
	t.Run("Fetch Project Issues", func(t *testing.T) {
		t.Parallel()

		issues, err := gitlabInstance.FetchProjectIssues(testProject)
		if err != nil {
			t.Errorf("Unexpected error when fetching project issues: %v", err)
		}
		if len(issues) == 0 {
			t.Error("Expected to fetch at least one issue")
		}
	})
}

func TestFetchProjectIssuesTitles(t *testing.T) {
	t.Parallel()

	gitlabInstance := setupTestServer(t, RoleDestination, InstanceSizeSmall)
	t.Run("Fetch Project Issues Titles", func(t *testing.T) {
		t.Parallel()

		issueTitles, err := gitlabInstance.FetchProjectIssuesTitles(testProject)
		if err != nil {
			t.Errorf("Unexpected error when fetching project issues titles: %v", err)
		}
		if len(issueTitles) == 0 {
			t.Error("Expected to fetch at least one issue title")
		}
	})
}

func TestMirrorIssue(t *testing.T) {
	t.Parallel()

	gitlabInstance := setupTestServer(t, RoleSource, InstanceSizeSmall)
	t.Run("Mirror Issue", func(t *testing.T) {
		t.Parallel()

		err := gitlabInstance.MirrorIssue(testProject, testIssue)
		if err != nil {
			t.Errorf("Unexpected error when mirroring issue: %v", err)
		}
	})
}

func TestCloseIssue(t *testing.T) {
	t.Parallel()

	gitlabInstance := setupTestServer(t, RoleDestination, InstanceSizeSmall)
	t.Run("Close Issue", func(t *testing.T) {
		t.Parallel()

		err := gitlabInstance.CloseIssue(testProject, testIssue)
		if err != nil {
			t.Errorf("Unexpected error when closing issue: %v", err)
		}
	})
}

func TestMirrorIssues(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
	sourceGitlabInstance := setupTestServer(t, RoleSource, InstanceSizeSmall)
	destinationGitlabInstance := setupTestServer(t, RoleDestination, InstanceSizeSmall)
	t.Run("Mirror Issues", func(t *testing.T) {
		helpers.ResetReported()
		t.Cleanup(helpers.ResetReported)

		destinationGitlabInstance.MirrorIssues(sourceGitlabInstance, testProject, testProject2)
		if got := helpers.ExitCode(); got != 0 {
			t.Errorf("Unexpected errors when mirroring issues: exit code %d", got)
		}
	})
}
