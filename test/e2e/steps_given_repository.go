//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"slices"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

// addFile commits a new file to a branch. A new branch starts from the first one
// of the project: the branches are tracked here, because reading them back right
// after the first commit can still answer none.
func (s *steps) addFile(role, name, branch, file, content string) error {
	fullPath := s.w.fullPath(role, name)
	message := "add " + file

	options := &gitlab.CreateCommitOptions{
		Branch:        &branch,
		CommitMessage: &message,
		Actions:       []*gitlab.CommitActionOptions{{Action: new(gitlab.FileCreate), FilePath: &file, Content: &content}},
	}

	known := s.w.branches[fullPath]
	if !slices.Contains(known, branch) && len(known) > 0 {
		options.StartBranch = &known[0]
	}

	_, _, err := s.w.env.client.Commits.CreateCommit(fullPath, options)
	if err != nil {
		return fmt.Errorf("commit to %s of %s: %w", branch, fullPath, err)
	}

	if !slices.Contains(known, branch) {
		s.w.branches[fullPath] = append(known, branch)
	}

	return nil
}

func (s *steps) commit(role, name, branch string) error {
	s.w.files++

	file := fmt.Sprintf("file-%d.txt", s.w.files)

	return s.addFile(role, name, branch, file, file)
}

func (s *steps) ciConfiguration(name string) error {
	return s.addFile(roleSource, name, defaultBranch, ".gitlab-ci.yml", "build:\n  script:\n    - echo build\n")
}

func (s *steps) tag(role, name, tag, branch string) error {
	fullPath := s.w.fullPath(role, name)

	_, _, err := s.w.env.client.Tags.CreateTag(fullPath, &gitlab.CreateTagOptions{TagName: &tag, Ref: &branch})
	if err != nil {
		return fmt.Errorf("create the tag %s of %s: %w", tag, fullPath, err)
	}

	return nil
}

func (s *steps) issue(role, name, state, title string) error {
	fullPath := s.w.fullPath(role, name)
	description := "description of " + title

	issue, _, err := s.w.env.client.Issues.CreateIssue(fullPath, &gitlab.CreateIssueOptions{Title: &title, Description: &description})
	if err != nil {
		return fmt.Errorf("create the issue %q of %s: %w", title, fullPath, err)
	}

	if state != "closed" {
		return nil
	}

	_, _, err = s.w.env.client.Issues.UpdateIssue(fullPath, issue.IID, &gitlab.UpdateIssueOptions{StateEvent: new("close")})
	if err != nil {
		return fmt.Errorf("close the issue %q of %s: %w", title, fullPath, err)
	}

	return nil
}

// avatarOf draws a picture that is specific to a path.
func avatarOf(fullPath string) ([]byte, error) {
	picture := image.NewRGBA(image.Rect(0, 0, avatarSize, avatarSize))

	for index, value := range []byte(fullPath) {
		picture.Set(index%avatarSize, index/avatarSize%avatarSize, color.RGBA{R: value, G: value, B: value, A: value})
	}

	var encoded bytes.Buffer

	err := png.Encode(&encoded, picture)
	if err != nil {
		return nil, fmt.Errorf("draw an avatar: %w", err)
	}

	return encoded.Bytes(), nil
}

func (s *steps) avatar(kind, name string) error {
	fullPath := s.w.fullPath(roleSource, name)

	picture, err := avatarOf(name)
	if err != nil {
		return err
	}

	if kind == kindGroup {
		_, err = s.ensureGroup(fullPath)
		if err != nil {
			return err
		}

		_, _, err = s.w.env.client.Groups.UploadAvatar(fullPath, bytes.NewReader(picture), "avatar.png")
	} else {
		_, _, err = s.w.env.client.Projects.UploadAvatar(fullPath, bytes.NewReader(picture), "avatar.png")
	}

	if err != nil {
		return fmt.Errorf("set the avatar of %s: %w", fullPath, err)
	}

	return nil
}

func (s *steps) setDescription(name, description string) error {
	fullPath := s.w.fullPath(roleSource, name)

	_, _, err := s.w.env.client.Projects.EditProject(fullPath, &gitlab.EditProjectOptions{Description: &description})
	if err != nil {
		return fmt.Errorf("edit the project %s: %w", fullPath, err)
	}

	return nil
}

func (s *steps) removeRef(name, kind, ref string) error {
	fullPath := s.w.fullPath(roleSource, name)

	var err error

	if kind == "tag" {
		_, err = s.w.env.client.Tags.DeleteTag(fullPath, ref)
	} else {
		_, err = s.w.env.client.Branches.DeleteBranch(fullPath, ref)
		s.w.branches[fullPath] = slices.DeleteFunc(s.w.branches[fullPath], func(known string) bool { return known == ref })
	}

	if err != nil {
		return fmt.Errorf("delete the %s %s of %s: %w", kind, ref, fullPath, err)
	}

	return nil
}
