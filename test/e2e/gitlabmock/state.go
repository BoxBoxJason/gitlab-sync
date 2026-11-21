package gitlabmock

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

var errNotFound = errors.New("not found")

type group struct {
	*gitlab.Group

	members map[int64]int64
	avatar  []byte
}

type packageFile struct {
	gitlab.PackageFile

	content []byte
}

type packageRecord struct {
	gitlab.Package

	files []*packageFile
}

type project struct {
	gitlab.Project

	links    map[string][]*gitlab.ReleaseLink
	members  map[int64]int64
	repoDir  string
	avatar   []byte
	issues   []*gitlab.Issue
	releases []*gitlab.Release
	packages []*packageRecord
}

// ProjectOption tunes a project created by AddProject.
type ProjectOption func(*project)

// WithDescription sets the project description.
func WithDescription(description string) ProjectOption {
	return func(p *project) { p.Description = description }
}

// WithVisibility sets the project visibility.
func WithVisibility(visibility string) ProjectOption {
	return func(p *project) { p.Visibility = gitlab.VisibilityValue(visibility) }
}

// WithDefaultBranch sets the default branch of the (still empty) repository.
func WithDefaultBranch(branch string) ProjectOption {
	return func(p *project) { p.DefaultBranch = branch }
}

// WithArchived archives the project.
func WithArchived() ProjectOption {
	return func(p *project) { p.Archived = true }
}

// WithTopics sets the project topics.
func WithTopics(topics ...string) ProjectOption {
	return func(p *project) { p.Topics = topics }
}

// AddGroup creates the group at fullPath and any missing parent group.
func (s *Server) AddGroup(fullPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.ensureGroup(strings.Trim(fullPath, "/"))

	return err
}

// ensureGroup returns the group at fullPath, creating it (and its parents) when
// missing. The caller holds the lock.
func (s *Server) ensureGroup(fullPath string) (*group, error) {
	if fullPath == "" {
		return nil, errors.New("empty group path")
	}

	if existing := s.groupByPath(fullPath); existing != nil {
		return existing, nil
	}

	var parentID int64

	parentPath := path.Dir(fullPath)
	if parentPath != "." {
		parent, err := s.ensureGroup(parentPath)
		if err != nil {
			return nil, err
		}

		parentID = parent.ID
	}

	return s.newGroup(path.Base(fullPath), path.Base(fullPath), parentID, "public", ""), nil
}

// newGroup records a group whose parent exists. The caller holds the lock.
func (s *Server) newGroup(name, groupPath string, parentID int64, visibility, description string) *group {
	fullPath := groupPath
	fullName := name

	if parentID != 0 {
		parent := s.groupByID(parentID)
		fullPath = parent.FullPath + "/" + groupPath
		fullName = parent.FullName + " / " + name
	}

	created := &group{
		Group: &gitlab.Group{
			ID:          s.id(),
			Name:        name,
			Path:        groupPath,
			FullPath:    fullPath,
			FullName:    fullName,
			ParentID:    parentID,
			Visibility:  gitlab.VisibilityValue(visibility),
			Description: description,
			WebURL:      s.publicBase + "/groups/" + fullPath,
		},
		members: make(map[int64]int64),
	}

	s.groups = append(s.groups, created)

	return created
}

func (s *Server) groupByPath(fullPath string) *group {
	for _, candidate := range s.groups {
		if candidate.FullPath == fullPath {
			return candidate
		}
	}

	return nil
}

func (s *Server) groupByID(id int64) *group {
	for _, candidate := range s.groups {
		if candidate.ID == id {
			return candidate
		}
	}

	return nil
}

// lookupGroup finds a group by numeric id or by full path.
func (s *Server) lookupGroup(idOrPath string) *group {
	parsed, err := strconv.ParseInt(idOrPath, 10, 64)
	if err == nil {
		return s.groupByID(parsed)
	}

	return s.groupByPath(idOrPath)
}

// AddProject creates the project at fullPath, with an empty repository, and any
// missing parent group. It is a no-op error when the project already exists.
func (s *Server) AddProject(fullPath string, options ...ProjectOption) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fullPath = strings.Trim(fullPath, "/")
	if s.projectByPath(fullPath) != nil {
		return fmt.Errorf("project %s already exists", fullPath)
	}

	namespace := path.Dir(fullPath)
	if namespace == "." {
		return fmt.Errorf("project %s has no namespace", fullPath)
	}

	parent, err := s.ensureGroup(namespace)
	if err != nil {
		return err
	}

	created := s.newProject(path.Base(fullPath), path.Base(fullPath), parent, "public")
	for _, option := range options {
		option(created)
	}

	return nil
}

// newProject records a project (the caller holds the lock). The repository is
// created on first use.
func (s *Server) newProject(name, projectPath string, namespace *group, visibility string) *project {
	projectID := s.id()
	fullPath := namespace.FullPath + "/" + projectPath

	created := &project{
		Project: gitlab.Project{
			ID:                projectID,
			Name:              name,
			Path:              projectPath,
			PathWithNamespace: fullPath,
			NameWithNamespace: namespace.FullName + " / " + name,
			DefaultBranch:     "main",
			Visibility:        gitlab.VisibilityValue(visibility),
			WebURL:            s.publicBase + "/" + fullPath,
			HTTPURLToRepo:     s.publicBase + "/" + fullPath + ".git",
			EmptyRepo:         true,
			Topics:            []string{},
			Namespace:         &gitlab.ProjectNamespace{ID: namespace.ID, FullPath: namespace.FullPath, Name: namespace.Name, Path: namespace.Path},
		},
		repoDir: filepath.Join(s.dir, strconv.FormatInt(projectID, 10)+".git"),
		links:   make(map[string][]*gitlab.ReleaseLink),
		members: make(map[int64]int64),
	}

	s.projects = append(s.projects, created)

	return created
}

func (s *Server) projectByPath(fullPath string) *project {
	for _, candidate := range s.projects {
		if candidate.PathWithNamespace == fullPath {
			return candidate
		}
	}

	return nil
}

func (s *Server) projectByID(id int64) *project {
	for _, candidate := range s.projects {
		if candidate.ID == id {
			return candidate
		}
	}

	return nil
}

// lookupProject finds a project by numeric id or by full path.
func (s *Server) lookupProject(idOrPath string) *project {
	parsed, err := strconv.ParseInt(idOrPath, 10, 64)
	if err == nil {
		return s.projectByID(parsed)
	}

	return s.projectByPath(idOrPath)
}

// AddGeneratedProjects creates count small projects named project-001... under
// the group at parent (created when missing).
func (s *Server) AddGeneratedProjects(parent string, count int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	namespace, err := s.ensureGroup(strings.Trim(parent, "/"))
	if err != nil {
		return err
	}

	for index := 1; index <= count; index++ {
		name := fmt.Sprintf("project-%03d", index)
		s.newProject(name, name, namespace, "public")
	}

	return nil
}
