//go:build e2e

package e2e

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/cucumber/godog"

	"github.com/boxboxjason/gitlab-sync/test/e2e/gitlabmock"
)

func (s *steps) registerGivenSteps(scenario *godog.ScenarioContext) {
	scenario.Step(`^the source and the destination are the same GitLab instance$`, s.sameInstance)
	scenario.Step(`^the `+role+` is (premium|freemium)$`, s.edition)
	scenario.Step(`^the `+role+` license plan is "([^"]*)"$`, s.licensePlan)
	scenario.Step(`^the `+role+` has no license endpoint$`, s.noLicenseEndpoint)
	scenario.Step(`^the `+role+` version is "([^"]*)"$`, s.version)
	scenario.Step(`^the `+role+` has the group "([^"]+)"$`, s.group)
	scenario.Step(`^the `+role+` has the project "([^"]+)"$`, s.project)
	scenario.Step(`^the `+role+` has the project "([^"]+)" with:$`, s.projectWith)
	scenario.Step(`^the `+role+` has the project "([^"]+)" with a commit on "([^"]+)"$`, s.projectWithCommit)
	scenario.Step(`^the `+role+` project "([^"]+)" has a commit on "([^"]+)"$`, s.commit)
	scenario.Step(`^the `+role+` project "([^"]+)" has the tag "([^"]+)" on "([^"]+)"$`, s.tag)
	scenario.Step(`^the `+role+` has (\d+) generated projects under "([^"]+)"$`, s.generatedProjects)
	scenario.Step(`^the `+role+` project "([^"]+)" has the (opened|closed) issue "([^"]*)"$`, s.issue)
	scenario.Step(`^the `+role+` project "([^"]+)" has the release "([^"]+)"$`, s.release)
	scenario.Step(`^the `+role+` project "([^"]+)" release "([^"]+)" has the link "([^"]*)" to "([^"]*)"$`, s.releaseLink)
	scenario.Step(`^the `+role+` project "([^"]+)" release "([^"]+)" has the link "([^"]*)" to the package file "([^"]+)" at "([^"]+)"$`, s.releaseLinkToPackage)
	scenario.Step(`^the `+role+` project "([^"]+)" has the generic package file "([^"]+)" with the content "([^"]*)"$`, s.packageFile)
	scenario.Step(`^the `+role+` (group|project) "([^"]+)" has an avatar$`, s.avatar)
	scenario.Step(`^the `+role+` has the archived project "([^"]+)"$`, s.archivedProject)
	scenario.Step(`^the `+role+` project "([^"]+)" now has the description "([^"]*)"$`, s.setDescription)
	scenario.Step(`^the `+role+` project "([^"]+)" no longer has the (branch|tag) "([^"]+)"$`, s.removeRef)
	scenario.Step(`^the `+role+` group "([^"]+)" is (public|internal|private)$`, s.groupVisibility)
	scenario.Step(`^the destination project "([^"]+)" has the authenticated user as (guest|developer|maintainer)$`, s.member)
	scenario.Step(`^the `+role+` project "([^"]+)" no longer has the generic package file "([^"]+)"$`, s.removePackageFile)
	scenario.Step(`^the `+role+` project "([^"]+)" no longer has the release "([^"]+)"$`, s.removeRelease)
	scenario.Step(`^the `+role+` project "([^"]+)" release "([^"]+)" no longer has the link "([^"]*)"$`, s.removeReleaseLink)
	scenario.Step(`^the `+role+` serves the file "([^"]+)" with the content "([^"]*)"$`, s.servedFile)
	scenario.Step(`^the mirror mapping:$`, s.mirrorMapping)
	scenario.Step(`^the `+role+` answers (\d+)(?: (\d+) times?)? on `+method+` (\S+)$`, s.fault)
	scenario.Step(`^the `+role+` forbids git (clone|push) of "([^"]+)"$`, s.forbidGit)
	scenario.Step(`^the `+role+` forbids every git (clone|push)$`, s.forbidEveryGit)
	scenario.Step(`^the `+role+` only accepts the token "([^"]+)"$`, s.requireToken)
	scenario.Step(`^the `+role+` cannot be reached$`, s.unreachable)
}

func (s *steps) sameInstance() error {
	if s.w.destination != s.w.source {
		err := s.w.destination.Close()
		if err != nil {
			return fmt.Errorf("stop the destination instance: %w", err)
		}
	}

	s.w.destination = s.w.source

	return nil
}

func (s *steps) edition(role, edition string) error {
	if edition == "premium" {
		s.w.server(role).SetLicensePlan("premium")
	} else {
		s.w.server(role).SetLicensePlan("")
	}

	return nil
}

func (s *steps) licensePlan(role, plan string) error {
	s.w.server(role).SetLicensePlan(plan)

	return nil
}

func (s *steps) noLicenseEndpoint(role string) error {
	s.w.server(role).SetLicenseEndpointMissing(true)

	return nil
}

func (s *steps) version(role, version string) error {
	s.w.server(role).SetVersion(version)

	return nil
}

func (s *steps) group(role, path string) error {
	return failed(s.w.server(role).AddGroup(path))
}

func (s *steps) project(role, path string) error {
	return failed(s.w.server(role).AddProject(path))
}

// projectWith creates a project with the attributes of a two columns table
// (description, visibility, default branch, topics).
func (s *steps) projectWith(role, path string, table *godog.Table) error {
	var options []projectOption

	for _, row := range table.Rows {
		if len(row.Cells) != tableColumns {
			return errors.New("the table must have two columns: attribute, value")
		}

		option, err := projectOptionFor(row.Cells[0].Value, row.Cells[1].Value)
		if err != nil {
			return err
		}

		options = append(options, option)
	}

	return failed(s.w.server(role).AddProject(path, options...))
}

func (s *steps) projectWithCommit(role, path, branch string) error {
	err := s.project(role, path)
	if err != nil {
		return err
	}

	return s.commit(role, path, branch)
}

func (s *steps) commit(role, path, branch string) error {
	n := s.w.unique()
	_, err := s.w.server(role).Commit(path, branch, fmt.Sprintf("file-%d.txt", n), fmt.Sprintf("content %d", n))

	return failed(err)
}

func (s *steps) tag(role, path, tag, branch string) error {
	return failed(s.w.server(role).Tag(path, tag, branch))
}

func (s *steps) generatedProjects(role string, count int, parent string) error {
	return failed(s.w.server(role).AddGeneratedProjects(parent, count))
}

func (s *steps) issue(role, path, state, title string) error {
	return failed(s.w.server(role).AddIssue(path, title, "description of "+title, state))
}

func (s *steps) release(role, path, tag string) error {
	return failed(s.w.server(role).AddRelease(path, tag, "Release "+tag, "notes of "+tag))
}

func (s *steps) releaseLink(role, path, tag, name, target string) error {
	return failed(s.w.server(role).AddReleaseLink(path, tag, name, s.w.expand(target), ""))
}

func (s *steps) releaseLinkToPackage(role, path, tag, name, file, directPath string) error {
	spec, err := splitPackageFile(file)
	if err != nil {
		return err
	}

	target := fmt.Sprintf("%s/api/v4/projects/%s/packages/generic/%s/%s/%s", s.w.server(role).URL(), url.PathEscape(path), spec.name, spec.version, spec.file)

	return failed(s.w.server(role).AddReleaseLink(path, tag, name, target, directPath))
}

func (s *steps) packageFile(role, path, file, content string) error {
	spec, err := splitPackageFile(file)
	if err != nil {
		return err
	}

	return failed(s.w.server(role).AddPackageFile(path, spec.name, spec.version, spec.file, []byte(content)))
}

func (s *steps) avatar(role, kind, path string) error {
	if kind == "group" {
		return failed(s.w.server(role).SetGroupAvatar(path, []byte("avatar of "+path)))
	}

	return failed(s.w.server(role).SetProjectAvatar(path, []byte("avatar of "+path)))
}

func (s *steps) archivedProject(role, path string) error {
	return failed(s.w.server(role).AddProject(path, gitlabmock.WithArchived()))
}

func (s *steps) setDescription(role, path, description string) error {
	return failed(s.w.server(role).SetProjectDescription(path, description))
}

func (s *steps) removeRef(role, path, kind, name string) error {
	prefix := "refs/heads/"
	if kind == "tag" {
		prefix = "refs/tags/"
	}

	return failed(s.w.server(role).DeleteRef(path, prefix+name))
}

func (s *steps) groupVisibility(role, path, visibility string) error {
	return failed(s.w.server(role).SetGroupVisibility(path, visibility))
}

func (s *steps) member(path, level string) error {
	levels := map[string]int64{"guest": accessGuest, "developer": accessDeveloper, "maintainer": accessMaintainer}

	return failed(s.w.destination.AddProjectMember(path, levels[level]))
}

func (s *steps) removePackageFile(role, path, file string) error {
	spec, err := splitPackageFile(file)
	if err != nil {
		return err
	}

	return failed(s.w.server(role).RemovePackageFile(path, spec.name, spec.version, spec.file))
}

func (s *steps) removeRelease(role, path, tag string) error {
	return failed(s.w.server(role).RemoveRelease(path, tag))
}

func (s *steps) removeReleaseLink(role, path, tag, name string) error {
	return failed(s.w.server(role).RemoveReleaseLink(path, tag, name))
}

func (s *steps) servedFile(role, path, content string) error {
	s.w.server(role).AddFile(path, []byte(content))

	return nil
}

func (s *steps) mirrorMapping(doc *godog.DocString) error {
	return s.w.writeMapping(doc.Content)
}

// fault fails the requests matching the method and the path pattern; times is
// empty when every matching request must fail.
func (s *steps) fault(role string, status int, times, method, pattern string) error {
	count := 0

	if times != "" {
		parsed, err := strconv.Atoi(times)
		if err != nil {
			return fmt.Errorf("parse the number of failures %q: %w", times, err)
		}

		count = parsed
	}

	s.w.server(role).InjectFault(method, pattern, status, count)

	return nil
}

func (s *steps) forbidGit(role, operation, path string) error {
	return failed(s.w.server(role).ForbidGit(operation, path))
}

func (s *steps) forbidEveryGit(role, operation string) error {
	return failed(s.w.server(role).ForbidGit(operation, ""))
}

func (s *steps) requireToken(role, token string) error {
	s.w.server(role).RequireToken(token)

	return nil
}

func (s *steps) unreachable(role string) error {
	return s.w.makeUnreachable(role)
}

// splitPackageFile splits "name/version/file".
func splitPackageFile(spec string) (packageFile, error) {
	parts := strings.SplitN(spec, "/", packageFileParts)

	if len(parts) != packageFileParts {
		return packageFile{}, fmt.Errorf("%q is not a package file, want name/version/file", spec)
	}

	return packageFile{name: parts[0], version: parts[1], file: parts[2]}, nil
}

// packageFile is a generic package file: name/version/file.
type packageFile struct {
	name, version, file string
}
