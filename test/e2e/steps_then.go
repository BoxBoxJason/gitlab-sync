//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/cucumber/godog"
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

const (
	listPageSize = 100

	// pipelineGrace is how long GitLab is given to create a pipeline after it
	// processed a push, before concluding it created none.
	pipelineGrace = 15 * time.Second

	skippedPipeline = "skipped"
)

func (s *steps) registerEntitySteps(scenario *godog.ScenarioContext) {
	scenario.Step(`^the `+role+` contains the (project|group) "([^"]+)"$`, s.hasEntity)
	scenario.Step(`^the `+role+` (?:contains|has) no (project|group) "([^"]+)"$`, s.hasNoEntity)
	scenario.Step(`^the destination project "([^"]+)" has the (description|visibility|default branch|name) "(.*)"$`, s.projectAttribute)
	scenario.Step(`^the source group "([^"]+)" is still (public|internal|private)$`, s.sourceGroupVisibility)
	scenario.Step(`^the destination project "([^"]+)" has the topics "(.*)"$`, s.topics)
	scenario.Step(`^the destination project "([^"]+)" has the same (?:refs|branches and tags) as the source project "([^"]+)"$`, s.sameRefs)
	scenario.Step(`^the destination (group|project) "([^"]+)" has the avatar of the source (?:group|project) "([^"]+)"$`, s.hasAvatar)
	scenario.Step(`^the source project "([^"]+)" has the same refs as the source project "([^"]+)"$`, s.sameSourceRefs)
	scenario.Step(`^the destination project "([^"]+)" does not have the same refs as the source project "([^"]+)"$`, s.differentRefs)
	scenario.Step(`^the destination project "([^"]+)" contains the ref "([^"]+)"$`, s.hasRef)
	scenario.Step(`^the destination project "([^"]+)" contains no ref "([^"]+)"$`, s.hasNoRef)
	scenario.Step(`^the destination project "([^"]+)" is not a pull mirror$`, s.notPullMirror)
	scenario.Step(`^the destination project "([^"]+)" is in the CI/CD catalog$`, s.inCatalog)
	scenario.Step(`^the destination project "([^"]+)" is not in the CI/CD catalog$`, s.notInCatalog)
	scenario.Step(`^the destination project "([^"]+)" runs a pipeline$`, s.runsPipeline)
	scenario.Step(`^the destination project "([^"]+)" runs no pipeline$`, s.runsNoPipeline)
	scenario.Step(`^the authenticated user is an owner of the destination (project|group) "([^"]+)"$`, s.isOwner)
	scenario.Step(`^the authenticated user is not a direct member of the destination (project|group) "([^"]+)"$`, s.isNotMember)
}

// found turns the answer of a lookup into whether the thing exists.
func found(response *gitlab.Response, err error) (bool, error) {
	if err == nil {
		return true, nil
	}

	if response != nil && response.StatusCode == http.StatusNotFound {
		return false, nil
	}

	return false, err
}

// exists tells whether a group or a project exists. A project marked for deletion
// still does.
func (s *steps) exists(role, kind, name string) (bool, error) {
	fullPath := s.w.fullPath(role, name)

	var (
		response *gitlab.Response
		err      error
	)

	if kind == kindGroup {
		_, response, err = s.w.env.client.Groups.GetGroup(fullPath, nil)
	} else {
		_, response, err = s.w.env.client.Projects.GetProject(fullPath, nil)
	}

	ok, err := found(response, err)
	if err != nil {
		return false, fmt.Errorf("look the %s %s up: %w", kind, fullPath, err)
	}

	return ok, nil
}

func (s *steps) hasEntity(role, kind, name string) error {
	ok, err := s.exists(role, kind, name)
	if err != nil {
		return err
	}

	if !ok {
		return fmt.Errorf("the %s has no %s %q%s", role, kind, name, s.w.describe())
	}

	return nil
}

func (s *steps) hasNoEntity(role, kind, name string) error {
	ok, err := s.exists(role, kind, name)
	if err != nil {
		return err
	}

	if ok {
		return fmt.Errorf("the %s has the %s %q%s", role, kind, name, s.w.describe())
	}

	return nil
}

func (s *steps) destinationProject(name string) (*gitlab.Project, error) {
	project, _, err := s.w.env.client.Projects.GetProject(s.w.fullPath(roleDestination, name), nil)
	if err != nil {
		return nil, fmt.Errorf("get the destination project %s: %w%s", name, err, s.w.describe())
	}

	return project, nil
}

// checkProject retries a check of a destination project while GitLab settles.
func (s *steps) checkProject(name string, check func(*gitlab.Project) error) error {
	return eventually(func() error {
		project, err := s.destinationProject(name)
		if err != nil {
			return err
		}

		return check(project)
	})
}

func (s *steps) projectAttribute(name, attribute, want string) error {
	return s.checkProject(name, func(project *gitlab.Project) error {
		got := map[string]string{
			attrDescription:  project.Description,
			attrVisibility:   string(project.Visibility),
			"default branch": project.DefaultBranch,
			"name":           project.Name,
		}[attribute]

		if got != want {
			return fmt.Errorf("the destination project %s has the %s %q, want %q%s", name, attribute, got, want, s.w.describe())
		}

		return nil
	})
}

func (s *steps) sourceGroupVisibility(name, want string) error {
	group, _, err := s.w.env.client.Groups.GetGroup(s.w.fullPath(roleSource, name), nil)
	if err != nil {
		return fmt.Errorf("get the source group %s: %w", name, err)
	}

	if string(group.Visibility) != want {
		return fmt.Errorf("the source group %s is now %s, it was changed behind the source's back (want %s)", name, group.Visibility, want)
	}

	return nil
}

func (s *steps) topics(name, want string) error {
	wanted := slices.Sorted(slices.Values(splitList(want, ",")))

	return s.checkProject(name, func(project *gitlab.Project) error {
		got := slices.Sorted(slices.Values(project.Topics))
		if !slices.Equal(got, wanted) {
			return fmt.Errorf("the destination project %s has the topics %v, want %v", name, got, wanted)
		}

		return nil
	})
}

func (s *steps) notPullMirror(name string) error {
	project, err := s.destinationProject(name)
	if err != nil {
		return err
	}

	if project.Mirror || project.ImportURL != "" {
		return fmt.Errorf("the destination project %s is a pull mirror (mirror=%t, import URL %q)", name, project.Mirror, project.ImportURL)
	}

	return nil
}

func (s *steps) inCatalog(name string) error {
	return s.checkProject(name, func(project *gitlab.Project) error {
		if !project.CICDCatalogEnabled {
			return fmt.Errorf("the destination project %s is not in the CI/CD catalog%s", name, s.w.describe())
		}

		return nil
	})
}

func (s *steps) notInCatalog(name string) error {
	project, err := s.destinationProject(name)
	if err != nil {
		return err
	}

	if project.CICDCatalogEnabled {
		return fmt.Errorf("the destination project %s is in the CI/CD catalog", name)
	}

	return nil
}
