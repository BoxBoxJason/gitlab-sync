package mirroring

import (
	"testing"
)

func TestFetchProjectIssues(t *testing.T) {
	_, gitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
	t.Run("Fetch Project Issues", func(t *testing.T) {
		issues, err := gitlabInstance.FetchProjectIssues(TEST_PROJECT)
		if err != nil {
			t.Errorf("Unexpected error when fetching project issues: %v", err)
		}
		if len(issues) == 0 {
			t.Error("Expected to fetch at least one issue")
		}
	})
}

func TestFetchProjectIssuesTitles(t *testing.T) {
	_, gitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
	t.Run("Fetch Project Issues Titles", func(t *testing.T) {
		issueTitles, err := gitlabInstance.FetchProjectIssuesTitles(TEST_PROJECT)
		if err != nil {
			t.Errorf("Unexpected error when fetching project issues titles: %v", err)
		}
		if len(issueTitles) == 0 {
			t.Error("Expected to fetch at least one issue title")
		}
	})
}

func TestMirrorIssue(t *testing.T) {
	_, gitlabInstance := setupTestServer(t, ROLE_SOURCE, INSTANCE_SIZE_SMALL)
	t.Run("Mirror Issue", func(t *testing.T) {
		err := gitlabInstance.MirrorIssue(TEST_PROJECT, TEST_ISSUE)
		if err != nil {
			t.Errorf("Unexpected error when mirroring issue: %v", err)
		}
	})
}

func TestCloseIssue(t *testing.T) {
	_, gitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
	t.Run("Close Issue", func(t *testing.T) {
		err := gitlabInstance.CloseIssue(TEST_PROJECT, TEST_ISSUE)
		if err != nil {
			t.Errorf("Unexpected error when closing issue: %v", err)
		}
	})
}

func TestMirrorIssues(t *testing.T) {
	_, sourceGitlabInstance := setupTestServer(t, ROLE_SOURCE, INSTANCE_SIZE_SMALL)
	_, destinationGitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
	t.Run("Mirror Issues", func(t *testing.T) {
		errors := destinationGitlabInstance.MirrorIssues(sourceGitlabInstance, TEST_PROJECT, TEST_PROJECT_2)
		if len(errors) > 0 {
			t.Errorf("Unexpected errors when mirroring issues: %v", errors)
		}
	})
}
