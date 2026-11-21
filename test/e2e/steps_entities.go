//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

func (s *steps) hasEntity(role, kind, path string) error {
	if s.entityExists(role, kind, path) {
		return nil
	}

	return fmt.Errorf("the %s has no %s %q (it has %s)%s", role, kind, path, s.entities(role, kind), s.w.describeLastRun())
}

func (s *steps) hasNoEntity(role, kind, path string) error {
	if s.entityExists(role, kind, path) {
		return fmt.Errorf("the %s has the %s %q", role, kind, path)
	}

	return nil
}

func (s *steps) entityExists(role, kind, path string) bool {
	if kind == kindProject {
		_, ok := s.w.server(role).Project(path)

		return ok
	}

	_, ok := s.w.server(role).Group(path)

	return ok
}

func (s *steps) entities(role, kind string) string {
	if kind == kindProject {
		return fmt.Sprint(s.w.server(role).ProjectPaths())
	}

	return fmt.Sprint(s.w.server(role).GroupPaths())
}

func (s *steps) projectAttribute(role, path, attribute, want string) error {
	project, ok := s.w.server(role).Project(path)
	if !ok {
		return fmt.Errorf("the %s has no project %q", role, path)
	}

	got := map[string]string{
		attrDescription:  project.Description,
		attrVisibility:   string(project.Visibility),
		"default branch": project.DefaultBranch,
		"name":           project.Name,
	}[attribute]

	if got != want {
		return fmt.Errorf("the %s project %s has the %s %q, want %q", role, path, attribute, got, want)
	}

	return nil
}

func (s *steps) groupAttribute(role, path, attribute, want string) error {
	group, ok := s.w.server(role).Group(path)
	if !ok {
		return fmt.Errorf("the %s has no group %q", role, path)
	}

	got := map[string]string{
		attrDescription: group.Description,
		attrVisibility:  string(group.Visibility),
		"name":          group.Name,
	}[attribute]

	if got != want {
		return fmt.Errorf("the %s group %s has the %s %q, want %q", role, path, attribute, got, want)
	}

	return nil
}

func (s *steps) topics(role, path, want string) error {
	project, ok := s.w.server(role).Project(path)
	if !ok {
		return fmt.Errorf("the %s has no project %q", role, path)
	}

	wanted := splitList(want)
	got := slices.Clone(project.Topics)

	slices.Sort(wanted)
	slices.Sort(got)

	if !slices.Equal(got, wanted) {
		return fmt.Errorf("the %s project %s has the topics %v, want %v", role, path, project.Topics, wanted)
	}

	return nil
}

func (s *steps) refs(role, path string) (map[string]string, error) {
	refs, err := s.w.server(role).Refs(path)
	if err != nil {
		return nil, fmt.Errorf("list the refs of the %s project %s: %w", role, path, err)
	}

	return refs, nil
}

func (s *steps) sameRefs(destinationPath, sourcePath string) error {
	want, err := s.refs(roleSource, sourcePath)
	if err != nil {
		return err
	}

	got, err := s.refs(roleDestination, destinationPath)
	if err != nil {
		return err
	}

	if len(want) == 0 {
		return fmt.Errorf("the source project %s has no refs, the comparison would prove nothing", sourcePath)
	}

	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("the destination project %s has the refs %v, want those of the source project %s: %v%s", destinationPath, got, sourcePath, want, s.w.describeLastRun())
	}

	return nil
}

func (s *steps) hasAvatar(kind, path, sourcePath string) error {
	want := []byte("avatar of " + sourcePath)

	got := s.w.destination.ProjectAvatar(path)
	if kind == "group" {
		got = s.w.destination.GroupAvatar(path)
	}

	if !bytes.Equal(got, want) {
		return fmt.Errorf("the destination %s %s has the avatar %q, want %q", kind, path, got, want)
	}

	return nil
}

func (s *steps) noRefs(path string) error {
	got, err := s.refs(roleDestination, path)
	if err != nil {
		return err
	}

	if len(got) > 0 {
		return fmt.Errorf("the destination project %s has the refs %v, want none", path, got)
	}

	return nil
}

func (s *steps) hasRef(path, ref string) error {
	got, err := s.refs(roleDestination, path)
	if err != nil {
		return err
	}

	if _, ok := got[ref]; !ok {
		return fmt.Errorf("the destination project %s has the refs %v, want %s", path, got, ref)
	}

	return nil
}

func (s *steps) hasNoRef(path, ref string) error {
	got, err := s.refs(roleDestination, path)
	if err != nil {
		return err
	}

	if _, ok := got[ref]; ok {
		return fmt.Errorf("the destination project %s still has %s", path, ref)
	}

	return nil
}

func (s *steps) destinationProject(path string) (gitlab.Project, error) {
	project, ok := s.w.destination.Project(path)
	if !ok {
		return project, fmt.Errorf("the destination has no project %q (it has %v)%s", path, s.w.destination.ProjectPaths(), s.w.describeLastRun())
	}

	return project, nil
}
