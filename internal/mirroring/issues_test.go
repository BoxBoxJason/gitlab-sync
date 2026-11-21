package mirroring

import (
	"net/http"
	"strconv"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"

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

// TestMirrorIssueClosesTheCreatedIssue checks that a closed issue is closed by the IID
// the destination gave it, not by the IID it had in the source.
func TestMirrorIssueClosesTheCreatedIssue(t *testing.T) {
	t.Parallel()

	const (
		destinationIID = 7
		sourceIID      = 2
	)

	mux, gitlabInstance := setupEmptyTestServer(t, RoleDestination)

	closedIID := make(chan string, 1)

	mux.HandleFunc("/api/v4/projects/{id}/issues", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected %s on the issues of the destination", r.Method)
		}

		w.Header().Set(headerContentType, headerAccept)
		w.WriteHeader(http.StatusCreated)
		writeBody(w, `{"id": 70, "iid": 7, "title": "It was slow", "state": "opened"}`)
	})
	mux.HandleFunc("/api/v4/projects/{id}/issues/{iid}", func(w http.ResponseWriter, r *http.Request) {
		closedIID <- r.PathValue("iid")

		w.Header().Set(headerContentType, headerAccept)
		w.WriteHeader(http.StatusOK)
		writeBody(w, `{"id": 70, "iid": 7, "title": "It was slow", "state": "closed"}`)
	})

	closed := &gitlab.Issue{IID: sourceIID, Title: "It was slow", State: string(gitlab.ClosedEventType)}

	err := gitlabInstance.MirrorIssue(testProject, closed)
	if err != nil {
		t.Fatalf("Unexpected error when mirroring issue: %v", err)
	}

	if got, want := <-closedIID, strconv.Itoa(destinationIID); got != want {
		t.Errorf("closed the issue %s of the destination, want %s (the source IID is %d)", got, want, sourceIID)
	}
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
