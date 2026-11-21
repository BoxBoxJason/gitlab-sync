package gitlabmock

import (
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

// SetGroupAvatar gives the group at fullPath an avatar.
func (s *Server) SetGroupAvatar(fullPath string, content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.groupByPath(fullPath)
	if found == nil {
		return fmt.Errorf("group %s: %w", fullPath, errNotFound)
	}

	found.avatar = content
	found.AvatarURL = fmt.Sprintf("%s/uploads/-/system/group/avatar/%d/avatar.png", s.publicBase, found.ID)

	return nil
}

// SetProjectAvatar gives the project at fullPath an avatar.
func (s *Server) SetProjectAvatar(fullPath string, content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.projectByPath(fullPath)
	if found == nil {
		return fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	found.avatar = content
	found.AvatarURL = fmt.Sprintf("%s/uploads/-/system/project/avatar/%d/avatar.png", s.publicBase, found.ID)

	return nil
}

// SetProjectDescription changes the description of the project at fullPath.
func (s *Server) SetProjectDescription(fullPath, description string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.projectByPath(fullPath)
	if found == nil {
		return fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	found.Description = description

	return nil
}

// SetGroupVisibility changes the visibility of the group at fullPath.
func (s *Server) SetGroupVisibility(fullPath, visibility string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.groupByPath(fullPath)
	if found == nil {
		return fmt.Errorf("group %s: %w", fullPath, errNotFound)
	}

	found.Visibility = gitlab.VisibilityValue(visibility)

	return nil
}

// AddProjectMember makes the authenticated user a member of the project, with
// the given access level (10 guest, 30 developer, 40 maintainer, 50 owner).
func (s *Server) AddProjectMember(fullPath string, level int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.projectByPath(fullPath)
	if found == nil {
		return fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	found.members[userID] = level

	return nil
}
