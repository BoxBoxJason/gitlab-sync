//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"io"
	"maps"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

// refs maps the branches and tags of a project to their commit.
func (s *steps) refs(role, name string) (map[string]string, error) {
	fullPath := s.w.fullPath(role, name)
	refs := make(map[string]string)

	branches, _, err := s.w.env.client.Branches.ListBranches(fullPath, &gitlab.ListBranchesOptions{ListOptions: gitlab.ListOptions{PerPage: listPageSize}})
	if err != nil {
		return nil, fmt.Errorf("list the branches of %s: %w", fullPath, err)
	}

	for _, branch := range branches {
		refs["refs/heads/"+branch.Name] = branch.Commit.ID
	}

	tags, _, err := s.w.env.client.Tags.ListTags(fullPath, &gitlab.ListTagsOptions{ListOptions: gitlab.ListOptions{PerPage: listPageSize}})
	if err != nil {
		return nil, fmt.Errorf("list the tags of %s: %w", fullPath, err)
	}

	for _, tag := range tags {
		refs["refs/tags/"+tag.Name] = tag.Commit.ID
	}

	return refs, nil
}

// checkRefs retries a check of the refs of a project while GitLab processes the
// push.
func (s *steps) checkRefs(role, name string, check func(map[string]string) error) error {
	return eventually(func() error {
		refs, err := s.refs(role, name)
		if err != nil {
			return fmt.Errorf("%w%s", err, s.w.describe())
		}

		return check(refs)
	})
}

func (s *steps) sameRefs(destination, source string) error {
	return s.compareRefs(roleDestination, destination, source)
}

func (s *steps) sameSourceRefs(copied, source string) error {
	return s.compareRefs(roleSource, copied, source)
}

func (s *steps) compareRefs(role, copied, source string) error {
	return s.checkRefs(role, copied, func(got map[string]string) error {
		want, err := s.refs(roleSource, source)
		if err != nil {
			return err
		}

		if len(want) == 0 {
			return fmt.Errorf("the source project %s has no refs, the comparison would prove nothing", source)
		}

		if !maps.Equal(got, want) {
			return fmt.Errorf("the %s project %s has the refs %v, want those of the source project %s: %v%s", role, copied, got, source, want, s.w.describe())
		}

		return nil
	})
}

// differentRefs checks that a push was refused: it is immediate, no need to wait.
func (s *steps) differentRefs(destination, source string) error {
	got, err := s.refs(roleDestination, destination)
	if err != nil {
		return err
	}

	want, err := s.refs(roleSource, source)
	if err != nil {
		return err
	}

	if maps.Equal(got, want) {
		return fmt.Errorf("the destination project %s has the refs of the source project %s%s", destination, source, s.w.describe())
	}

	return nil
}

func (s *steps) hasRef(name, ref string) error {
	return s.checkRefs(roleDestination, name, func(got map[string]string) error {
		if _, ok := got[ref]; !ok {
			return fmt.Errorf("the destination project %s has the refs %v, want %s", name, got, ref)
		}

		return nil
	})
}

func (s *steps) hasNoRef(name, ref string) error {
	return s.checkRefs(roleDestination, name, func(got map[string]string) error {
		if _, ok := got[ref]; ok {
			return fmt.Errorf("the destination project %s still has %s", name, ref)
		}

		return nil
	})
}

// avatarContent downloads the avatar of a group or a project, nil when it has none.
func (s *steps) avatarContent(role, kind, name string) ([]byte, error) {
	fullPath := s.w.fullPath(role, name)

	var (
		reader   *bytes.Reader
		response *gitlab.Response
		err      error
	)

	if kind == kindGroup {
		reader, response, err = s.w.env.client.Groups.DownloadAvatar(fullPath)
	} else {
		reader, response, err = s.w.env.client.Projects.DownloadAvatar(fullPath)
	}

	ok, err := found(response, err)
	if err != nil || !ok {
		return nil, err
	}

	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read the avatar of %s: %w", fullPath, err)
	}

	return content, nil
}

func (s *steps) hasAvatar(kind, name, source string) error {
	want, err := s.avatarContent(roleSource, kind, source)
	if err != nil {
		return err
	}

	if len(want) == 0 {
		return fmt.Errorf("the source %s %s has no avatar", kind, source)
	}

	return eventually(func() error {
		got, downloadErr := s.avatarContent(roleDestination, kind, name)
		if downloadErr != nil {
			return downloadErr
		}

		if !bytes.Equal(got, want) {
			return fmt.Errorf("the destination %s %s has an avatar of %d bytes, want the %d bytes of the source one%s", kind, name, len(got), len(want), s.w.describe())
		}

		return nil
	})
}
