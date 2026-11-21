//go:build e2e

package e2e

import (
	"fmt"
	"strconv"
)

func (s *steps) pullMirror(path, sourcePath string) error {
	project, err := s.destinationProject(path)
	if err != nil {
		return err
	}

	want := s.w.source.URL() + "/" + sourcePath + ".git"
	if !project.Mirror || project.ImportURL != want {
		return fmt.Errorf("the destination project %s has mirror=%t and import URL %q, want a pull mirror of %q", path, project.Mirror, project.ImportURL, want)
	}

	return nil
}

func (s *steps) notPullMirror(path string) error {
	project, err := s.destinationProject(path)
	if err != nil {
		return err
	}

	if project.Mirror || project.ImportURL != "" {
		return fmt.Errorf("the destination project %s is a pull mirror (mirror=%t, import URL %q)", path, project.Mirror, project.ImportURL)
	}

	return nil
}

func (s *steps) inCatalog(path string) error {
	project, err := s.destinationProject(path)
	if err != nil {
		return err
	}

	if !project.CICDCatalogEnabled {
		return fmt.Errorf("the destination project %s is not in the CI/CD catalog%s", path, s.w.describeLastRun())
	}

	return nil
}

func (s *steps) notInCatalog(path string) error {
	project, err := s.destinationProject(path)
	if err != nil {
		return err
	}

	if project.CICDCatalogEnabled {
		return fmt.Errorf("the destination project %s is in the CI/CD catalog", path)
	}

	return nil
}

func (s *steps) triggerBuilds(path, want string) error {
	project, err := s.destinationProject(path)
	if err != nil {
		return err
	}

	if strconv.FormatBool(project.MirrorTriggerBuilds) != want {
		return fmt.Errorf("the destination project %s has mirror_trigger_builds=%t, want %s", path, project.MirrorTriggerBuilds, want)
	}

	return nil
}

const ownerLevel = 50

func (s *steps) membership(kind, path string) (int64, bool) {
	if kind == kindProject {
		return s.w.destination.ProjectMember(path)
	}

	return s.w.destination.GroupMember(path)
}

func (s *steps) isOwner(kind, path string) error {
	level, ok := s.membership(kind, path)
	if !ok || level != ownerLevel {
		return fmt.Errorf("the authenticated user has access level %d (member: %t) on the destination %s %s, want owner (%d)", level, ok, kind, path, ownerLevel)
	}

	return nil
}

func (s *steps) isNotMember(kind, path string) error {
	if level, ok := s.membership(kind, path); ok {
		return fmt.Errorf("the authenticated user is a member of the destination %s %s (access level %d)", kind, path, level)
	}

	return nil
}

func (s *steps) groupStillVisibility(path, want string) error {
	group, ok := s.w.source.Group(path)
	if !ok {
		return fmt.Errorf("the source has no group %q", path)
	}

	if string(group.Visibility) != want {
		return fmt.Errorf("the source group %s is now %s, it was changed behind the source's back (want %s)", path, group.Visibility, want)
	}

	return nil
}
