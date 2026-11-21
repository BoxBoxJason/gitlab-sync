//go:build e2e

package e2e

import (
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"

	"github.com/boxboxjason/gitlab-sync/test/e2e/gitlabmock"
)

func (s *steps) hasIssue(path, state, title string) error {
	issues := s.w.destination.Issues(path)

	for index := range issues {
		if issues[index].Title == title && issues[index].State == state {
			return nil
		}
	}

	return fmt.Errorf("the destination project %s has no %s issue %q, it has %s", path, state, title, s.issueSummary(path))
}

func (s *steps) issueSummary(path string) string {
	issues := s.w.destination.Issues(path)
	summary := make([]string, 0, len(issues))

	for index := range issues {
		summary = append(summary, fmt.Sprintf("%s (%s)", issues[index].Title, issues[index].State))
	}

	return fmt.Sprint(summary)
}

func (s *steps) issueCount(path string, want int) error {
	if got := len(s.w.destination.Issues(path)); got != want {
		return fmt.Errorf("the destination project %s has %d issue(s), want %d: %s", path, got, want, s.issueSummary(path))
	}

	return nil
}

// findRelease looks a release of the destination up by its tag.
func (s *steps) findRelease(path, tag string) (*gitlab.Release, bool) {
	releases := s.w.destination.Releases(path)

	for index := range releases {
		if releases[index].TagName == tag {
			return &releases[index], true
		}
	}

	return nil, false
}

func (s *steps) hasRelease(path, tag string) error {
	_, found := s.findRelease(path, tag)
	if found {
		return nil
	}

	return fmt.Errorf("the destination project %s has no release %q%s", path, tag, s.w.describeLastRun())
}

func (s *steps) hasNoRelease(path, tag string) error {
	_, found := s.findRelease(path, tag)
	if found {
		return fmt.Errorf("the destination project %s still has the release %q", path, tag)
	}

	return nil
}

func (s *steps) releaseLinks(path, tag string) ([]*gitlab.ReleaseLink, error) {
	release, found := s.findRelease(path, tag)
	if found {
		return release.Assets.Links, nil
	}

	return nil, fmt.Errorf("the destination project %s has no release %q", path, tag)
}

func (s *steps) hasLink(path, tag, name, target string) error {
	links, err := s.releaseLinks(path, tag)
	if err != nil {
		return err
	}

	target = s.w.expand(target)

	for _, link := range links {
		if link.Name == name && link.URL == target {
			return nil
		}
	}

	var found []string
	for _, link := range links {
		found = append(found, link.Name+" -> "+link.URL)
	}

	return fmt.Errorf("the release %s of the destination project %s has no link %q to %q, it has %v", tag, path, name, target, found)
}

func (s *steps) releaseAttribute(path, tag, attribute, want string) error {
	release, found := s.findRelease(path, tag)
	if !found {
		return fmt.Errorf("the destination project %s has no release %q", path, tag)
	}

	got := release.Name
	if attribute == attrDescription {
		got = release.Description
	}

	if got != want {
		return fmt.Errorf("the release %s of the destination project %s has the %s %q, want %q", tag, path, attribute, got, want)
	}

	return nil
}

func (s *steps) linkCount(path, tag string, want int) error {
	links, err := s.releaseLinks(path, tag)
	if err != nil {
		return err
	}

	if len(links) != want {
		return fmt.Errorf("the release %s of the destination project %s has %d link(s), want %d", tag, path, len(links), want)
	}

	return nil
}

// hasPackageFile checks the newest copy of a package file, the one GitLab serves.
func (s *steps) hasPackageFile(path, file, content string) error {
	spec, err := splitPackageFile(file)
	if err != nil {
		return err
	}

	var (
		found  []string
		newest *gitlabmock.PackageFileView
	)

	for _, candidate := range s.w.destination.PackageFiles(path) {
		found = append(found, candidate.Package+"/"+candidate.Version+"/"+candidate.File)

		if candidate.Package == spec.name && candidate.Version == spec.version && candidate.File == spec.file {
			newest = &candidate
		}
	}

	if newest == nil {
		return fmt.Errorf("the destination project %s has no generic package file %s, it has %v%s", path, file, found, s.w.describeLastRun())
	}

	if string(newest.Content) != content {
		return fmt.Errorf("the newest copy of the generic package file %s of the destination project %s holds %q, want %q", file, path, newest.Content, content)
	}

	return nil
}

func (s *steps) hasNoPackageFile(path, file string) error {
	spec, err := splitPackageFile(file)
	if err != nil {
		return err
	}

	for _, candidate := range s.w.destination.PackageFiles(path) {
		if candidate.Package == spec.name && candidate.Version == spec.version && candidate.File == spec.file {
			return fmt.Errorf("the destination project %s still has the generic package file %s", path, file)
		}
	}

	return nil
}

func (s *steps) packageFileCount(path string, want int) error {
	if got := len(s.w.destination.PackageFiles(path)); got != want {
		return fmt.Errorf("the destination project %s has %d generic package file(s), want %d: %v", path, got, want, s.w.destination.PackageFiles(path))
	}

	return nil
}

func (s *steps) hasLinkToPackage(path, tag, name, file string) error {
	project, err := s.destinationProject(path)
	if err != nil {
		return err
	}

	spec, err := splitPackageFile(file)
	if err != nil {
		return err
	}

	target := fmt.Sprintf("%s/api/v4/projects/%d/packages/generic/%s/%s/%s", s.w.destination.URL(), project.ID, spec.name, spec.version, spec.file)

	return s.hasLink(path, tag, name, target)
}
