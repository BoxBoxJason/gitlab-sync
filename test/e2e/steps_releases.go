//go:build e2e

package e2e

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cucumber/godog"
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

func (s *steps) registerReleaseSteps(scenario *godog.ScenarioContext) {
	scenario.Step(`^the destination project "([^"]+)" contains the (opened|closed) issue "([^"]*)"$`, s.hasIssue)
	scenario.Step(`^the destination project "([^"]+)" has (\d+) issues?$`, s.issueCount)
	scenario.Step(`^the destination project "([^"]+)" contains the release "([^"]+)"$`, s.hasRelease)
	scenario.Step(`^the destination project "([^"]+)" contains no release "([^"]+)"$`, s.hasNoRelease)
	scenario.Step(`^the destination project "([^"]+)" release "([^"]+)" contains the link "([^"]*)" to "(.*)"$`, s.hasLink)
	scenario.Step(`^the destination project "([^"]+)" release "([^"]+)" contains the link "([^"]*)" to the package file "([^"]+)"$`, s.hasLinkToPackage)
	scenario.Step(`^the destination project "([^"]+)" release "([^"]+)" has (\d+) links?$`, s.linkCount)
	scenario.Step(`^the destination project "([^"]+)" release "([^"]+)" has the (name|description) "(.*)"$`, s.releaseAttribute)
	scenario.Step(`^the destination project "([^"]+)" contains the generic package file "([^"]+)" with the content "([^"]*)"$`, s.hasPackageFile)
	scenario.Step(`^the destination project "([^"]+)" contains no generic package file "([^"]+)"$`, s.hasNoPackageFile)
	scenario.Step(`^the destination project "([^"]+)" has (\d+) generic package files?$`, s.packageFileCount)
}

// issues lists the issues of a destination project as "state title".
func (s *steps) issues(name string) ([]string, error) {
	fullPath := s.w.fullPath(roleDestination, name)

	listed, _, err := s.w.env.client.Issues.ListProjectIssues(fullPath, &gitlab.ListProjectIssuesOptions{
		State:       new("all"),
		ListOptions: gitlab.ListOptions{PerPage: listPageSize},
	})
	if err != nil {
		return nil, fmt.Errorf("list the issues of %s: %w%s", fullPath, err, s.w.describe())
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
		return fmt.Errorf("the destination project %s has the issues [%s], want %s %q%s", name, strings.Join(issues, ", "), state, title, s.w.describe())
	}

	return nil
}

func (s *steps) issueCount(name string, want int) error {
	issues, err := s.issues(name)
	if err != nil {
		return err
	}

	if len(issues) != want {
		return fmt.Errorf("the destination project %s has %d issue(s) [%s], want %d%s", name, len(issues), strings.Join(issues, ", "), want, s.w.describe())
	}

	return nil
}

// findRelease looks a release of the destination up by its tag, nil when there
// is none.
func (s *steps) findRelease(name, tag string) (*gitlab.Release, error) {
	fullPath := s.w.fullPath(roleDestination, name)

	releases, _, err := s.w.env.client.Releases.ListReleases(fullPath, &gitlab.ListReleasesOptions{ListOptions: gitlab.ListOptions{PerPage: listPageSize}})
	if err != nil {
		return nil, fmt.Errorf("list the releases of %s: %w%s", fullPath, err, s.w.describe())
	}

	for _, release := range releases {
		if release.TagName == tag {
			return release, nil
		}
	}

	return nil, nil //nolint:nilnil // not found is an answer, not a failure
}

func (s *steps) mustRelease(name, tag string) (*gitlab.Release, error) {
	release, err := s.findRelease(name, tag)
	if err != nil {
		return nil, err
	}

	if release == nil {
		return nil, fmt.Errorf("the destination project %s has no release %q%s", name, tag, s.w.describe())
	}

	return release, nil
}

func (s *steps) hasRelease(name, tag string) error {
	_, err := s.mustRelease(name, tag)

	return err
}

func (s *steps) hasNoRelease(name, tag string) error {
	release, err := s.findRelease(name, tag)
	if err != nil {
		return err
	}

	if release != nil {
		return fmt.Errorf("the destination project %s has the release %q%s", name, tag, s.w.describe())
	}

	return nil
}

func (s *steps) hasLink(name, tag, link, target string) error {
	release, err := s.mustRelease(name, tag)
	if err != nil {
		return err
	}

	target = s.w.expand(target)

	var links []string

	for _, candidate := range release.Assets.Links {
		if candidate.Name == link && candidate.URL == target {
			return nil
		}

		links = append(links, candidate.Name+" -> "+candidate.URL)
	}

	return fmt.Errorf("the release %s of the destination project %s has no link %q to %q, it has %v%s", tag, name, link, target, links, s.w.describe())
}

func (s *steps) hasLinkToPackage(name, tag, link, file string) error {
	project, err := s.destinationProject(name)
	if err != nil {
		return err
	}

	spec, err := splitPackageFile(file)
	if err != nil {
		return err
	}

	return s.hasLink(name, tag, link, fmt.Sprintf("%s/api/v4/projects/%d/packages/generic/%s/%s/%s",
		s.w.url(roleDestination), project.ID, spec.name, spec.version, spec.file))
}

func (s *steps) releaseAttribute(name, tag, attribute, want string) error {
	release, err := s.mustRelease(name, tag)
	if err != nil {
		return err
	}

	got := release.Name
	if attribute == attrDescription {
		got = release.Description
	}

	if got != want {
		return fmt.Errorf("the release %s of the destination project %s has the %s %q, want %q", tag, name, attribute, got, want)
	}

	return nil
}

func (s *steps) linkCount(name, tag string, want int) error {
	release, err := s.mustRelease(name, tag)
	if err != nil {
		return err
	}

	if got := len(release.Assets.Links); got != want {
		return fmt.Errorf("the release %s of the destination project %s has %d link(s), want %d%s", tag, name, got, want, s.w.describe())
	}

	return nil
}

// storedFile is a file of the generic package registry.
type storedFile struct {
	// path is name/version/file.
	path          string
	packageID, id int64
}

// packageFiles lists the files of the generic packages of a project.
func (s *steps) packageFiles(fullPath string) ([]storedFile, error) {
	packages, _, err := s.w.env.client.Packages.ListProjectPackages(fullPath, &gitlab.ListProjectPackagesOptions{
		PackageType: new("generic"),
		ListOptions: gitlab.ListOptions{PerPage: listPageSize},
	})
	if err != nil {
		return nil, fmt.Errorf("list the packages of %s: %w", fullPath, err)
	}

	var files []storedFile

	for _, pkg := range packages {
		listed, _, listErr := s.w.env.client.Packages.ListPackageFiles(fullPath, pkg.ID, &gitlab.ListPackageFilesOptions{
			ListOptions: gitlab.ListOptions{PerPage: listPageSize},
		})
		if listErr != nil {
			return nil, fmt.Errorf("list the files of the package %s of %s: %w", pkg.Name, fullPath, listErr)
		}

		for _, file := range listed {
			files = append(files, storedFile{path: pkg.Name + "/" + pkg.Version + "/" + file.FileName, packageID: pkg.ID, id: file.ID})
		}
	}

	return files, nil
}

// hasPackageFile checks the copy of a package file GitLab serves, the newest one.
func (s *steps) hasPackageFile(name, file, content string) error {
	spec, err := splitPackageFile(file)
	if err != nil {
		return err
	}

	got, _, err := s.w.env.client.GenericPackages.DownloadPackageFile(s.w.fullPath(roleDestination, name), spec.name, spec.version, spec.file)
	if err != nil {
		return fmt.Errorf("download %s of the destination project %s: %w%s", file, name, err, s.w.describe())
	}

	if string(got) != content {
		return fmt.Errorf("the generic package file %s of the destination project %s holds %q, want %q%s", file, name, got, content, s.w.describe())
	}

	return nil
}

func (s *steps) hasNoPackageFile(name, file string) error {
	files, err := s.packageFiles(s.w.fullPath(roleDestination, name))
	if err != nil {
		return err
	}

	if slices.ContainsFunc(files, func(candidate storedFile) bool { return candidate.path == file }) {
		return fmt.Errorf("the destination project %s still has the generic package file %s%s", name, file, s.w.describe())
	}

	return nil
}

func (s *steps) packageFileCount(name string, want int) error {
	files, err := s.packageFiles(s.w.fullPath(roleDestination, name))
	if err != nil {
		return err
	}

	if len(files) != want {
		return fmt.Errorf("the destination project %s has %d generic package file(s), want %d: %v%s", name, len(files), want, files, s.w.describe())
	}

	return nil
}
