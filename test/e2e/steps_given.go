//go:build e2e

package e2e

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cucumber/godog"
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

func (s *steps) registerGivenSteps(scenario *godog.ScenarioContext) {
	scenario.Step(`^the source project "([^"]+)" exists$`, s.project)
	scenario.Step(`^the source project "([^"]+)" exists with the description "([^"]*)" and the topics "([^"]*)"$`, s.projectWith)
	scenario.Step(`^the source project "([^"]+)" has a commit on "([^"]+)"$`, s.commit)
	scenario.Step(`^the source project "([^"]+)" has the tag "([^"]+)" on "([^"]+)"$`, s.tag)
	scenario.Step(`^the source project "([^"]+)" has the (opened|closed) issue "([^"]*)"$`, s.issue)
	scenario.Step(`^the source project "([^"]+)" has the release "([^"]+)"$`, s.release)
	scenario.Step(`^the source project "([^"]+)" now has the description "([^"]*)"$`, s.setDescription)
	scenario.Step(`^the mirror mapping:$`, s.mirrorMapping)
}

// sourceGroupID returns the ID of the source group, created on first use.
func (s *steps) sourceGroupID() (int64, error) {
	if s.w.groupID != 0 {
		return s.w.groupID, nil
	}

	path := s.w.groupPath(roleSource)

	group, _, err := s.w.env.client.Groups.CreateGroup(&gitlab.CreateGroupOptions{Name: &path, Path: &path})
	if err != nil {
		return 0, fmt.Errorf("create the group %s: %w", path, err)
	}

	s.w.groupID = group.ID

	return group.ID, nil
}

func (s *steps) project(name string) error {
	return s.projectWith(name, "", "")
}

func (s *steps) projectWith(name, description, topics string) error {
	groupID, err := s.sourceGroupID()
	if err != nil {
		return err
	}

	options := &gitlab.CreateProjectOptions{Name: &name, Path: &name, NamespaceID: &groupID, Description: &description}

	if list := splitList(topics); len(list) > 0 {
		options.Topics = &list
	}

	_, _, err = s.w.env.client.Projects.CreateProject(options)
	if err != nil {
		return fmt.Errorf("create the project %s: %w", name, err)
	}

	return nil
}

// commit adds a commit, with a file of its own, to a branch. A new branch starts
// from the first one of the project: the branches are tracked here, because
// reading them back right after the first commit can still answer none.
func (s *steps) commit(name, branch string) error {
	path := s.w.projectPath(roleSource, name)

	s.w.files++

	file := fmt.Sprintf("file-%d.txt", s.w.files)
	message := "add " + file
	action := gitlab.FileCreate

	options := &gitlab.CreateCommitOptions{
		Branch:        &branch,
		CommitMessage: &message,
		Actions:       []*gitlab.CommitActionOptions{{Action: &action, FilePath: &file, Content: &file}},
	}

	known := s.w.branches[path]
	if !slices.Contains(known, branch) && len(known) > 0 {
		options.StartBranch = &known[0]
	}

	_, _, err := s.w.env.client.Commits.CreateCommit(path, options)
	if err != nil {
		return fmt.Errorf("commit to %s of %s: %w", branch, path, err)
	}

	if !slices.Contains(known, branch) {
		s.w.branches[path] = append(known, branch)
	}

	return nil
}

func (s *steps) tag(name, tag, branch string) error {
	path := s.w.projectPath(roleSource, name)

	_, _, err := s.w.env.client.Tags.CreateTag(path, &gitlab.CreateTagOptions{TagName: &tag, Ref: &branch})
	if err != nil {
		return fmt.Errorf("create the tag %s of %s: %w", tag, path, err)
	}

	return nil
}

func (s *steps) issue(name, state, title string) error {
	path := s.w.projectPath(roleSource, name)

	issue, _, err := s.w.env.client.Issues.CreateIssue(path, &gitlab.CreateIssueOptions{Title: &title})
	if err != nil {
		return fmt.Errorf("create the issue %q of %s: %w", title, path, err)
	}

	if state != "closed" {
		return nil
	}

	event := "close"

	_, _, err = s.w.env.client.Issues.UpdateIssue(path, issue.IID, &gitlab.UpdateIssueOptions{StateEvent: &event})
	if err != nil {
		return fmt.Errorf("close the issue %q of %s: %w", title, path, err)
	}

	return nil
}

func (s *steps) release(name, tag string) error {
	path := s.w.projectPath(roleSource, name)

	_, _, err := s.w.env.client.Releases.CreateRelease(path, &gitlab.CreateReleaseOptions{TagName: &tag, Name: &tag})
	if err != nil {
		return fmt.Errorf("create the release %s of %s: %w", tag, path, err)
	}

	return nil
}

func (s *steps) setDescription(name, description string) error {
	path := s.w.projectPath(roleSource, name)

	_, _, err := s.w.env.client.Projects.EditProject(path, &gitlab.EditProjectOptions{Description: &description})
	if err != nil {
		return fmt.Errorf("edit the project %s: %w", path, err)
	}

	return nil
}

func (s *steps) mirrorMapping(doc *godog.DocString) error {
	return s.w.writeMapping(doc.Content)
}

// splitList splits a comma separated list.
func splitList(value string) []string {
	var items []string

	for item := range strings.SplitSeq(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			items = append(items, item)
		}
	}

	return items
}
