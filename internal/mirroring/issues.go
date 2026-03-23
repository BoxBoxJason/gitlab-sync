package mirroring

import (
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

const (
	issuesPerPage   = 100
	closeStateEvent = "close"
)

// ===========================================================================
//                         ISSUES MIRRORING FUNCTIONS                       //
// ===========================================================================

// ================
//	      GET
// ================

// FetchProjectIssues retrieves all issues for a project and processes them.
func (g *GitlabInstance) FetchProjectIssues(project *gitlab.Project) ([]*gitlab.Issue, error) {
	zap.L().Debug("Fetching issues for project", zap.String("project", project.PathWithNamespace))

	fetchOpts := &gitlab.ListProjectIssuesOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: issuesPerPage,
			Page:    1,
		},
	}

	issues := make([]*gitlab.Issue, 0)

	for {
		fetchedIssues, resp, err := g.Gitlab.Issues.ListProjectIssues(project.ID, fetchOpts)
		if err != nil {
			return nil, fmt.Errorf("failed to list issues for project %s: %w", project.PathWithNamespace, err)
		}

		issues = append(issues, fetchedIssues...)

		if resp.CurrentPage >= resp.TotalPages {
			break
		}

		fetchOpts.Page = resp.NextPage
	}

	return issues, nil
}

// FetchProjectIssuesTitles retrieves all issue titles for a project and returns them as a map.
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

// MirrorIssue creates an issue in the destination project.
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

// CloseIssue closes an issue in the destination project.
func (g *GitlabInstance) CloseIssue(project *gitlab.Project, issue *gitlab.Issue) error {
	zap.L().Debug("Closing issue in destination project", zap.String("issue", issue.Title), zap.String(ROLE_DESTINATION, project.HTTPURLToRepo))

	_, _, err := g.Gitlab.Issues.UpdateIssue(project.ID, issue.IID, &gitlab.UpdateIssueOptions{
		StateEvent: gitlab.Ptr(closeStateEvent),
	})
	if err != nil {
		return fmt.Errorf("failed to close issue %d in project %s: %w", issue.IID, project.PathWithNamespace, err)
	}

	return nil
}

// ================
//    CONTROLLER
// ================

// MirrorIssues mirrors issues from the source project to the destination project.
// It fetches existing issues from the destination project and creates new issues for those that do not.
func (destinationGitlab *GitlabInstance) MirrorIssues(sourceGitlab *GitlabInstance, sourceProject, destinationProject *gitlab.Project) []error {
	return mirrorProjectEntities(
		"issue",
		sourceProject,
		destinationProject,
		destinationGitlab.FetchProjectIssuesTitles,
		sourceGitlab.FetchProjectIssues,
		func(issue *gitlab.Issue) string {
			return issue.Title
		},
		func(project *gitlab.Project, issue *gitlab.Issue) error {
			return destinationGitlab.MirrorIssue(project, issue)
		},
	)
}
