package mirroring

import (
	"fmt"
	"gitlab-sync/pkg/helpers"
	"sync"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

var (
	CLOSE_STATE_EVENT = "close"
)

// ===========================================================================
//                         ISSUES MIRRORING FUNCTIONS                       //
// ===========================================================================

// ================
//	      GET
// ================

// FetchProjectIssues retrieves all issues for a project and processes them
func (g *GitlabInstance) FetchProjectIssues(project *gitlab.Project) ([]*gitlab.Issue, error) {
	zap.L().Debug("Fetching issues for project", zap.String("project", project.PathWithNamespace))
	fetchOpts := &gitlab.ListProjectIssuesOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
			Page:    1,
		},
	}

	var issues = make([]*gitlab.Issue, 0)

	for {
		fetchedIssues, resp, err := g.Gitlab.Issues.ListProjectIssues(project.ID, fetchOpts)
		if err != nil {
			return nil, err
		}
		issues = append(issues, fetchedIssues...)

		if resp.CurrentPage >= resp.TotalPages {
			break
		}
		fetchOpts.Page = resp.NextPage
	}

	return issues, nil
}

// FetchProjectIssuesTitles retrieves all issue titles for a project and returns them as a map
func (g *GitlabInstance) FetchProjectIssuesTitles(project *gitlab.Project) (map[string]struct{}, error) {
	// Fetch existing issues from the destination project
	issues, err := g.FetchProjectIssues(project)
	if err != nil {
		return nil, err
	}

	// Create a map of existing issue titles for quick lookup
	issueTitles := make(map[string]struct{})
	for _, issue := range issues {
		if issue != nil {
			issueTitles[issue.Title] = struct{}{}
		}
	}
	return issueTitles, nil
}

// ================
//	     POST
// ================

// MirrorIssue creates an issue in the destination project
func (g *GitlabInstance) MirrorIssue(project *gitlab.Project, issue *gitlab.Issue) error {
	zap.L().Debug("Creating issue in destination project", zap.String("issue", issue.Title), zap.String(ROLE_DESTINATION, project.HTTPURLToRepo))

	// Create the issue in the destination project
	_, _, err := g.Gitlab.Issues.CreateIssue(project.ID, &gitlab.CreateIssueOptions{
		Title:        &issue.Title,
		Description:  &issue.Description,
		Labels:       (*gitlab.LabelOptions)(&issue.Labels),
		CreatedAt:    issue.CreatedAt,
		Confidential: &issue.Confidential,
		DueDate:      issue.DueDate,
		Weight:       &issue.Weight,
		IssueType:    issue.IssueType,
	})

	if err == nil && issue.State == string(gitlab.ClosedEventType) {
		// If the issue is closed, close it in the destination project
		err = g.CloseIssue(project, issue)
	}

	return err
}

// CloseIssue closes an issue in the destination project
func (g *GitlabInstance) CloseIssue(project *gitlab.Project, issue *gitlab.Issue) error {
	zap.L().Debug("Closing issue in destination project", zap.String("issue", issue.Title), zap.String(ROLE_DESTINATION, project.HTTPURLToRepo))
	_, _, err := g.Gitlab.Issues.UpdateIssue(project.ID, issue.IID, &gitlab.UpdateIssueOptions{
		StateEvent: &CLOSE_STATE_EVENT,
	})
	return err
}

// ================
//    CONTROLLER
// ================

// MirrorIssues mirrors issues from the source project to the destination project.
// It fetches existing issues from the destination project and creates new issues for those that do not
func (destinationGitlab *GitlabInstance) MirrorIssues(sourceGitlab *GitlabInstance, sourceProject *gitlab.Project, destinationProject *gitlab.Project) []error {
	zap.L().Info("Starting issues mirroring", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))

	// Fetch existing issues from the destination project
	existingIssuesTitles, err := destinationGitlab.FetchProjectIssuesTitles(destinationProject)
	if err != nil {
		return []error{fmt.Errorf("failed to fetch existing issues for destination project %s: %v", destinationProject.HTTPURLToRepo, err)}
	}

	// Fetch issues from the source project
	sourceIssues, err := sourceGitlab.FetchProjectIssues(sourceProject)
	if err != nil {
		return []error{fmt.Errorf("failed to fetch issues for source project %s: %v", sourceProject.HTTPURLToRepo, err)}
	}

	// Create a wait group and an error channel for handling API calls concurrently
	var wg sync.WaitGroup
	errorChan := make(chan error, len(sourceIssues))

	// Iterate over each source issue
	for _, issue := range sourceIssues {
		// Check if the issue already exists in the destination project
		if _, exists := existingIssuesTitles[issue.Title]; exists {
			zap.L().Debug("Issue already exists", zap.String("issue", issue.Title), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))
			continue
		}

		// Increment the wait group counter
		wg.Add(1)

		// Define the API call logic for creating an issue
		go func(project *gitlab.Project, issueToMirror *gitlab.Issue) {
			defer wg.Done()
			err := destinationGitlab.MirrorIssue(destinationProject, issueToMirror)
			if err != nil {
				errorChan <- fmt.Errorf("failed to create issue %s in project %s: %s", issueToMirror.Title, destinationProject.HTTPURLToRepo, err)
			}
		}(destinationProject, issue)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(errorChan)
	zap.L().Info("Issues mirroring completed", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))
	return helpers.MergeErrors(errorChan)
}
