//go:build e2e

package e2e

import (
	"fmt"
	"maps"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/cucumber/godog"
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

func (s *steps) registerThenSteps(scenario *godog.ScenarioContext) {
	scenario.Step(`^the exit code is (\d+)$`, s.exitCode)
	scenario.Step(`^the output matches "(.*)"$`, s.outputMatches)
	scenario.Step(`^the stderr matches "(.*)"$`, s.stderrMatches)
	scenario.Step(`^the file "([^"]+)" exists$`, s.fileExists)
	scenario.Step(`^the file "([^"]+)" matches "(.*)"$`, s.fileMatches)
	scenario.Step(`^the destination has the project "([^"]+)"$`, s.hasProject)
	scenario.Step(`^the destination has no project "([^"]+)"$`, s.hasNoProject)
	scenario.Step(`^the destination project "([^"]+)" has the description "([^"]*)"$`, s.description)
	scenario.Step(`^the destination project "([^"]+)" has the topics "([^"]*)"$`, s.topics)
	scenario.Step(`^the destination project "([^"]+)" has the visibility "([^"]+)"$`, s.visibility)
	scenario.Step(`^the destination project "([^"]+)" has the same branches and tags as the source project "([^"]+)"$`, s.sameRefs)
	scenario.Step(`^the destination project "([^"]+)" has the (opened|closed) issue "([^"]*)"$`, s.hasIssue)
	scenario.Step(`^the destination project "([^"]+)" has (\d+) issues?$`, s.issueCount)
	scenario.Step(`^the destination project "([^"]+)" has the release "([^"]+)"$`, s.hasRelease)
}

func (s *steps) stderrMatches(pattern string) error {
	if s.w.last == nil {
		return errNotRun
	}

	matched, err := regexp.MatchString(pattern, s.w.last.stderr)
	if err != nil {
		return fmt.Errorf("bad pattern %q: %w", pattern, err)
	}
	if !matched {
		return fmt.Errorf("stderr does not match %q%s", pattern, s.w.describe())
	}

	return nil
}

func (s *steps) fileExists(path string) error {
	path = s.w.expand(path)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("the file %s does not exist: %w", path, err)
	}

	return nil
}

func (s *steps) fileMatches(path, pattern string) error {
	path = s.w.expand(path)
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	matched, err := regexp.Match(pattern, contents)
	if err != nil {
		return fmt.Errorf("bad pattern %q: %w", pattern, err)
	}
	if !matched {
		return fmt.Errorf("the file %s does not match %q", path, pattern)
	}

	return nil
}

func (s *steps) exitCode(want int) error {
	if s.w.last == nil {
		return errNotRun
	}

	if s.w.last.exit != want {
		return fmt.Errorf("the exit code is %d, want %d%s", s.w.last.exit, want, s.w.describe())
	}

	return nil
}

func (s *steps) outputMatches(pattern string) error {
	if s.w.last == nil {
		return errNotRun
	}

	matched, err := regexp.MatchString(pattern, s.w.last.stdout+s.w.last.stderr)
	if err != nil {
		return fmt.Errorf("bad pattern %q: %w", pattern, err)
	}

	if !matched {
		return fmt.Errorf("the output does not match %q%s", pattern, s.w.describe())
	}

	return nil
}

// destinationProject looks a destination project up. It is nil when there is none.
func (s *steps) destinationProject(path string) (*gitlab.Project, error) {
	found, response, err := s.w.env.client.Projects.GetProject(path, nil)
	if err != nil && response != nil && response.StatusCode == http.StatusNotFound {
		return nil, nil //nolint:nilnil // not found is an answer, not a failure
	}

	if err != nil {
		return nil, fmt.Errorf("get the project %s: %w", path, err)
	}

	return found, nil
}

func (s *steps) hasProject(name string) error {
	path := s.w.projectPath(roleDestination, name)

	found, err := s.destinationProject(path)
	if err != nil {
		return err
	}

	if found == nil {
		return fmt.Errorf("the destination has no project %s%s", path, s.w.describe())
	}

	return nil
}

func (s *steps) hasNoProject(name string) error {
	path := s.w.projectPath(roleDestination, name)

	found, err := s.destinationProject(path)
	if err != nil {
		return err
	}

	if found != nil {
		return fmt.Errorf("the destination has the project %s", path)
	}

	return nil
}

func (s *steps) description(name, want string) error {
	path := s.w.projectPath(roleDestination, name)

	found, err := s.destinationProject(path)
	if err != nil {
		return err
	}

	if found == nil || found.Description != want {
		return fmt.Errorf("the destination project %s has the description %q, want %q", path, describeProject(found), want)
	}

	return nil
}

func describeProject(found *gitlab.Project) string {
	if found == nil {
		return "(no such project)"
	}

	return found.Description
}

func (s *steps) topics(name, want string) error {
	path := s.w.projectPath(roleDestination, name)

	found, err := s.destinationProject(path)
	if err != nil {
		return err
	}

	if found == nil {
		return fmt.Errorf("the destination has no project %s", path)
	}

	got, wanted := slices.Sorted(slices.Values(found.Topics)), slices.Sorted(slices.Values(splitList(want)))
	if !slices.Equal(got, wanted) {
		return fmt.Errorf("the destination project %s has the topics %v, want %v", path, got, wanted)
	}

	return nil
}

func (s *steps) visibility(name, want string) error {
	found, err := s.destinationProject(s.w.projectPath(roleDestination, name))
	if err != nil {
		return err
	}
	if found == nil || string(found.Visibility) != want {
		return fmt.Errorf("the destination project %s has visibility %q, want %q", name, describeVisibility(found), want)
	}

	return nil
}

func describeVisibility(found *gitlab.Project) string {
	if found == nil {
		return "(no such project)"
	}

	return string(found.Visibility)
}

// refsOf maps the branches and tags of a project to their commit.
func (s *steps) refsOf(path string) (map[string]string, error) {
	refs := make(map[string]string)

	branches, _, err := s.w.env.client.Branches.ListBranches(path, nil)
	if err != nil {
		return nil, fmt.Errorf("list the branches of %s: %w", path, err)
	}

	for _, branch := range branches {
		refs["refs/heads/"+branch.Name] = branch.Commit.ID
	}

	tags, _, err := s.w.env.client.Tags.ListTags(path, nil)
	if err != nil {
		return nil, fmt.Errorf("list the tags of %s: %w", path, err)
	}

	for _, tag := range tags {
		refs["refs/tags/"+tag.Name] = tag.Commit.ID
	}

	return refs, nil
}

func (s *steps) sameRefs(destination, source string) error {
	got, err := s.refsOf(s.w.projectPath(roleDestination, destination))
	if err != nil {
		return err
	}

	want, err := s.refsOf(s.w.projectPath(roleSource, source))
	if err != nil {
		return err
	}

	if !maps.Equal(got, want) {
		return fmt.Errorf("the destination project %s has the refs %v, want those of the source: %v%s", destination, got, want, s.w.describe())
	}

	return nil
}

// issues lists the issues of a destination project as "state title".
func (s *steps) issues(name string) ([]string, error) {
	path := s.w.projectPath(roleDestination, name)
	all := "all"

	listed, _, err := s.w.env.client.Issues.ListProjectIssues(path, &gitlab.ListProjectIssuesOptions{State: &all})
	if err != nil {
		return nil, fmt.Errorf("list the issues of %s: %w", path, err)
	}

	issues := make([]string, 0, len(listed))

	for _, issue := range listed {
		issues = append(issues, issue.State+" "+issue.Title)
	}

	return issues, nil
}

func (s *steps) hasIssue(name, state, title string) error {
	issues, err := s.issues(name)
	if err != nil {
		return err
	}

	if !slices.Contains(issues, state+" "+title) {
		return fmt.Errorf("the destination project %s has the issues [%s], want %s %q", name, strings.Join(issues, ", "), state, title)
	}

	return nil
}

func (s *steps) issueCount(name string, want int) error {
	issues, err := s.issues(name)
	if err != nil {
		return err
	}

	if len(issues) != want {
		return fmt.Errorf("the destination project %s has %d issue(s) [%s], want %d", name, len(issues), strings.Join(issues, ", "), want)
	}

	return nil
}

func (s *steps) hasRelease(name, tag string) error {
	path := s.w.projectPath(roleDestination, name)

	releases, _, err := s.w.env.client.Releases.ListReleases(path, nil)
	if err != nil {
		return fmt.Errorf("list the releases of %s: %w", path, err)
	}

	for _, release := range releases {
		if release.TagName == tag {
			return nil
		}
	}

	return fmt.Errorf("the destination project %s has no release %q%s", path, tag, s.w.describe())
}
