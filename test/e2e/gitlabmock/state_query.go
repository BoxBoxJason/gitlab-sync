package gitlabmock

import (
	"slices"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

// Project returns a copy of the project at fullPath.
func (s *Server) Project(fullPath string) (gitlab.Project, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.projectByPath(fullPath)
	if found == nil {
		return gitlab.Project{}, false
	}

	return found.Project, true
}

// ProjectPaths lists the full path of every project, sorted.
func (s *Server) ProjectPaths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	paths := make([]string, 0, len(s.projects))
	for _, candidate := range s.projects {
		paths = append(paths, candidate.PathWithNamespace)
	}

	slices.Sort(paths)

	return paths
}

// Group returns a copy of the group at fullPath.
func (s *Server) Group(fullPath string) (gitlab.Group, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.groupByPath(fullPath)
	if found == nil {
		return gitlab.Group{}, false
	}

	return *found.Group, true
}

// GroupPaths lists the full path of every group, sorted.
func (s *Server) GroupPaths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	paths := make([]string, 0, len(s.groups))
	for _, candidate := range s.groups {
		paths = append(paths, candidate.FullPath)
	}

	slices.Sort(paths)

	return paths
}

// ProjectMember returns the access level of the authenticated user on the project.
func (s *Server) ProjectMember(fullPath string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.projectByPath(fullPath)
	if found == nil {
		return 0, false
	}

	level, ok := found.members[userID]

	return level, ok
}

// GroupMember returns the access level of the authenticated user on the group.
func (s *Server) GroupMember(fullPath string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.groupByPath(fullPath)
	if found == nil {
		return 0, false
	}

	level, ok := found.members[userID]

	return level, ok
}

// PackageFileView describes a package file for assertions.
type PackageFileView struct {
	Package string
	Version string
	File    string
	Content []byte
}

// PackageFiles lists the generic package files of a project.
func (s *Server) PackageFiles(fullPath string) []PackageFileView {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.projectByPath(fullPath)
	if found == nil {
		return nil
	}

	var files []PackageFileView

	for _, record := range found.packages {
		for _, file := range record.files {
			files = append(files, PackageFileView{Package: record.Name, Version: record.Version, File: file.FileName, Content: file.content})
		}
	}

	return files
}

// GroupAvatar returns the avatar of the group at fullPath, nil when it has none.
func (s *Server) GroupAvatar(fullPath string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	if found := s.groupByPath(fullPath); found != nil {
		return found.avatar
	}

	return nil
}

// ProjectAvatar returns the avatar of the project at fullPath, nil when it has none.
func (s *Server) ProjectAvatar(fullPath string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	if found := s.projectByPath(fullPath); found != nil {
		return found.avatar
	}

	return nil
}
