//go:build e2e

package e2e

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/cucumber/godog"
)

const (
	role   = `(source|destination)`
	method = `(GET|POST|PUT|DELETE|PATCH)`
)

// steps holds the scenario state the step definitions work on.
type steps struct {
	w *world
}

// initializeScenario is called by godog for every scenario.
func initializeScenario(scenario *godog.ScenarioContext) {
	s := &steps{}

	scenario.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		w, err := newWorld()
		s.w = w

		return ctx, err
	})

	// A scenario fails when the application reached something the fake does not
	// implement: the answer it got (501) is not what a GitLab would have given.
	scenario.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		if s.w == nil {
			return ctx, err
		}

		defer s.w.close()

		if err != nil {
			return ctx, err
		}

		for _, server := range s.w.servers() {
			if unimplemented := server.Unimplemented(); len(unimplemented) > 0 {
				return ctx, fmt.Errorf("the fake GitLab does not implement: %s", strings.Join(unimplemented, ", "))
			}
		}

		return ctx, nil
	})

	s.registerGivenSteps(scenario)
	s.registerRunSteps(scenario)
	s.registerThenSteps(scenario)
}

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
		s.w.destination.Close()
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
	return s.w.server(role).AddGroup(path)
}

func (s *steps) project(role, path string) error {
	return s.w.server(role).AddProject(path)
}

// projectWith creates a project with the attributes of a two columns table
// (description, visibility, default branch, topics).
func (s *steps) projectWith(role, path string, table *godog.Table) error {
	var options []projectOption

	for _, row := range table.Rows {
		if len(row.Cells) != 2 {
			return errors.New("the table must have two columns: attribute, value")
		}

		option, err := projectOptionFor(row.Cells[0].Value, row.Cells[1].Value)
		if err != nil {
			return err
		}

		options = append(options, option)
	}

	return s.w.server(role).AddProject(path, options...)
}

func (s *steps) projectWithCommit(role, path, branch string) error {
	if err := s.project(role, path); err != nil {
		return err
	}

	return s.commit(role, path, branch)
}

func (s *steps) commit(role, path, branch string) error {
	n := s.w.unique()
	_, err := s.w.server(role).Commit(path, branch, fmt.Sprintf("file-%d.txt", n), fmt.Sprintf("content %d", n))

	return err
}

func (s *steps) tag(role, path, tag, branch string) error {
	return s.w.server(role).Tag(path, tag, branch)
}

func (s *steps) generatedProjects(role string, count int, parent string) error {
	return s.w.server(role).AddGeneratedProjects(parent, count)
}

func (s *steps) issue(role, path, state, title string) error {
	return s.w.server(role).AddIssue(path, title, "description of "+title, state)
}

func (s *steps) release(role, path, tag string) error {
	return s.w.server(role).AddRelease(path, tag, "Release "+tag, "notes of "+tag)
}

func (s *steps) releaseLink(role, path, tag, name, target string) error {
	return s.w.server(role).AddReleaseLink(path, tag, name, s.w.expand(target), "")
}

func (s *steps) releaseLinkToPackage(role, path, tag, name, file, directPath string) error {
	packageName, version, fileName, err := splitPackageFile(file)
	if err != nil {
		return err
	}

	target := fmt.Sprintf("%s/api/v4/projects/%s/packages/generic/%s/%s/%s", s.w.server(role).URL(), url.PathEscape(path), packageName, version, fileName)

	return s.w.server(role).AddReleaseLink(path, tag, name, target, directPath)
}

func (s *steps) packageFile(role, path, file, content string) error {
	packageName, version, fileName, err := splitPackageFile(file)
	if err != nil {
		return err
	}

	return s.w.server(role).AddPackageFile(path, packageName, version, fileName, []byte(content))
}

func (s *steps) avatar(role, kind, path string) error {
	if kind == "group" {
		return s.w.server(role).SetGroupAvatar(path, []byte("avatar of "+path))
	}

	return s.w.server(role).SetProjectAvatar(path, []byte("avatar of "+path))
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
		var err error

		if count, err = strconv.Atoi(times); err != nil {
			return err
		}
	}

	s.w.server(role).InjectFault(method, pattern, status, count)

	return nil
}

func (s *steps) forbidGit(role, operation, path string) error {
	return s.w.server(role).ForbidGit(operation, path)
}

func (s *steps) forbidEveryGit(role, operation string) error {
	return s.w.server(role).ForbidGit(operation, "")
}

func (s *steps) requireToken(role, token string) error {
	s.w.server(role).RequireToken(token)

	return nil
}

func (s *steps) unreachable(role string) error {
	return s.w.makeUnreachable(role)
}

// splitPackageFile splits "name/version/file".
func splitPackageFile(spec string) (string, string, string, error) {
	parts := strings.SplitN(spec, "/", 3) //nolint:mnd // name, version, file

	if len(parts) != 3 { //nolint:mnd // name, version, file
		return "", "", "", fmt.Errorf("%q is not a package file, want name/version/file", spec)
	}

	return parts[0], parts[1], parts[2], nil
}

func (s *steps) registerRunSteps(scenario *godog.ScenarioContext) {
	scenario.Step(`^I run gitlab-sync$`, func() error { return s.runWith("", "") })
	scenario.Step(`^I run gitlab-sync with "([^"]*)"$`, func(flags string) error { return s.runWith(flags, "") })
	scenario.Step(`^I run gitlab-sync without "([^"]*)"$`, func(dropped string) error { return s.runWith("", dropped) })
	scenario.Step(`^I run gitlab-sync with "([^"]*)" without "([^"]*)"$`, s.runWith)
	scenario.Step(`^I run gitlab-sync with the bare arguments "([^"]*)"$`, func(args string) error {
		return s.w.execute(splitArgs(s.w.expand(args)))
	})
}

// runWith runs the binary with the default flags, the extra flags, minus the
// dropped flags.
func (s *steps) runWith(flags, dropped string) error {
	return s.w.execute(s.w.flagsFor(splitArgs(s.w.expand(flags)), splitArgs(dropped)))
}
