package gitlabmock

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

var errNotFound = errors.New("not found")

type group struct {
	gitlab.Group

	members map[int64]int64
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

	repoDir  string
	issues   []*gitlab.Issue
	releases []*gitlab.Release
	links    map[string][]*gitlab.ReleaseLink
	packages []*packageRecord
	members  map[int64]int64
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
		Group: gitlab.Group{
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
	if id, err := strconv.ParseInt(idOrPath, 10, 64); err == nil {
		return s.groupByID(id)
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
	id := s.id()
	fullPath := namespace.FullPath + "/" + projectPath

	created := &project{
		Project: gitlab.Project{
			ID:                id,
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
		repoDir: filepath.Join(s.dir, strconv.FormatInt(id, 10)+".git"),
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
	if id, err := strconv.ParseInt(idOrPath, 10, 64); err == nil {
		return s.projectByID(id)
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

// --- repository seeding and inspection -------------------------------------

var commitSignature = object.Signature{Name: "Seed", Email: "seed@example.com", When: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}

// repo opens (creating it when needed) the bare repository of a project.
func (p *project) repo() (*git.Repository, error) {
	repository, err := git.PlainOpen(p.repoDir)
	if err == nil {
		return repository, nil
	}

	if !errors.Is(err, git.ErrRepositoryNotExists) {
		return nil, err
	}

	repository, err = git.PlainInit(p.repoDir, true)
	if err != nil {
		return nil, err
	}

	head := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(p.DefaultBranch))

	return repository, repository.Storer.SetReference(head)
}

// Commit adds a commit writing content to file on branch of the project at
// fullPath, creating the branch when missing. It returns the commit hash.
func (s *Server) Commit(fullPath, branch, file, content string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return "", fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	repository, err := target.repo()
	if err != nil {
		return "", err
	}

	var parents []plumbing.Hash

	branchRef := plumbing.NewBranchReferenceName(branch)
	if ref, refErr := repository.Reference(branchRef, true); refErr == nil {
		parents = append(parents, ref.Hash())
	}

	blobObject := repository.Storer.NewEncodedObject()
	blobObject.SetType(plumbing.BlobObject)

	writer, err := blobObject.Writer()
	if err != nil {
		return "", err
	}

	if _, err = writer.Write([]byte(content)); err != nil {
		return "", err
	}

	if err = writer.Close(); err != nil {
		return "", err
	}

	blobHash, err := repository.Storer.SetEncodedObject(blobObject)
	if err != nil {
		return "", err
	}

	tree := &object.Tree{Entries: []object.TreeEntry{{Name: file, Mode: filemode.Regular, Hash: blobHash}}}
	treeObject := repository.Storer.NewEncodedObject()

	if err = tree.Encode(treeObject); err != nil {
		return "", err
	}

	treeHash, err := repository.Storer.SetEncodedObject(treeObject)
	if err != nil {
		return "", err
	}

	signature := commitSignature
	signature.When = signature.When.Add(time.Duration(len(parents)+int(blobHash[0])) * time.Second)

	commit := &object.Commit{Author: signature, Committer: signature, Message: "update " + file, TreeHash: treeHash, ParentHashes: parents}
	commitObject := repository.Storer.NewEncodedObject()

	if err = commit.Encode(commitObject); err != nil {
		return "", err
	}

	commitHash, err := repository.Storer.SetEncodedObject(commitObject)
	if err != nil {
		return "", err
	}

	if err = repository.Storer.SetReference(plumbing.NewHashReference(branchRef, commitHash)); err != nil {
		return "", err
	}

	target.EmptyRepo = false

	return commitHash.String(), nil
}

// Tag creates the lightweight tag name on the tip of branch.
func (s *Server) Tag(fullPath, name, branch string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	repository, err := target.repo()
	if err != nil {
		return err
	}

	ref, err := repository.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		return fmt.Errorf("branch %s of %s: %w", branch, fullPath, err)
	}

	return repository.Storer.SetReference(plumbing.NewHashReference(plumbing.NewTagReferenceName(name), ref.Hash()))
}

// Refs returns the branches and tags of a project's repository ("refs/heads/main"
// -> commit hash).
func (s *Server) Refs(fullPath string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return nil, fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	repository, err := target.repo()
	if err != nil {
		return nil, err
	}

	iterator, err := repository.References()
	if err != nil {
		return nil, err
	}

	refs := make(map[string]string)

	err = iterator.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() == plumbing.HashReference {
			refs[ref.Name().String()] = ref.Hash().String()
		}

		return nil
	})

	return refs, err
}

// --- inspection of the REST resources --------------------------------------

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

	sort.Strings(paths)

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

	return found.Group, true
}

// GroupPaths lists the full path of every group, sorted.
func (s *Server) GroupPaths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	paths := make([]string, 0, len(s.groups))
	for _, candidate := range s.groups {
		paths = append(paths, candidate.FullPath)
	}

	sort.Strings(paths)

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

// --- issues -----------------------------------------------------------------

// AddIssue adds an issue to a project. state is "opened" or "closed".
func (s *Server) AddIssue(fullPath, title, description, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	target.issues = append(target.issues, s.newIssue(target, title, description, state))

	return nil
}

func (s *Server) newIssue(target *project, title, description, state string) *gitlab.Issue {
	now := time.Now().UTC()

	return &gitlab.Issue{
		ID:          s.id(),
		IID:         int64(len(target.issues) + 1),
		ProjectID:   target.ID,
		Title:       title,
		Description: description,
		State:       state,
		CreatedAt:   &now,
		UpdatedAt:   &now,
	}
}

// Issues returns a copy of the issues of a project.
func (s *Server) Issues(fullPath string) []gitlab.Issue {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.projectByPath(fullPath)
	if found == nil {
		return nil
	}

	issues := make([]gitlab.Issue, 0, len(found.issues))
	for _, issue := range found.issues {
		issues = append(issues, *issue)
	}

	return issues
}

// --- releases ---------------------------------------------------------------

// AddRelease adds a release to a project.
func (s *Server) AddRelease(fullPath, tag, name, description string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	now := time.Now().UTC()
	target.releases = append(target.releases, &gitlab.Release{TagName: tag, Name: name, Description: description, CreatedAt: &now, ReleasedAt: &now})

	return nil
}

// AddReleaseLink adds an asset link to the release tag of a project. directPath
// is the optional direct asset path ("/bin/tool").
func (s *Server) AddReleaseLink(fullPath, tag, name, url, directPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	target.links[tag] = append(target.links[tag], s.newReleaseLink(target, tag, name, url, directPath, "other"))

	return nil
}

func (s *Server) newReleaseLink(target *project, tag, name, url, directPath, linkType string) *gitlab.ReleaseLink {
	link := &gitlab.ReleaseLink{ID: s.id(), Name: name, URL: url, LinkType: gitlab.LinkTypeValue(linkType), DirectAssetURL: url}
	if directPath != "" {
		link.DirectAssetURL = s.publicBase + "/" + target.PathWithNamespace + "/-/releases/" + tag + "/downloads" + directPath
	}

	return link
}

// Releases returns a copy of the releases of a project, with their links.
func (s *Server) Releases(fullPath string) []gitlab.Release {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.projectByPath(fullPath)
	if found == nil {
		return nil
	}

	releases := make([]gitlab.Release, 0, len(found.releases))
	for _, release := range found.releases {
		releases = append(releases, found.releaseView(release))
	}

	return releases
}

// releaseView copies a release and fills its asset links.
func (p *project) releaseView(release *gitlab.Release) gitlab.Release {
	view := *release

	for _, link := range p.links[release.TagName] {
		view.Assets.Links = append(view.Assets.Links, link)
	}

	view.Assets.Count = int64(len(view.Assets.Links))

	return view
}

// --- generic packages -------------------------------------------------------

// AddPackageFile publishes a file of a generic package of a project.
func (s *Server) AddPackageFile(fullPath, name, version, fileName string, content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	s.publishPackageFile(target, name, version, fileName, content)

	return nil
}

// publishPackageFile adds a file to a generic package, creating the package when
// needed. The caller holds the lock.
func (s *Server) publishPackageFile(target *project, name, version, fileName string, content []byte) {
	var record *packageRecord

	for _, candidate := range target.packages {
		if candidate.Name == name && candidate.Version == version {
			record = candidate
		}
	}

	if record == nil {
		record = &packageRecord{Package: gitlab.Package{ID: s.id(), Name: name, Version: version, PackageType: "generic"}}
		target.packages = append(target.packages, record)
	}

	sum := sha256.Sum256(content)
	record.files = append(record.files, &packageFile{
		PackageFile: gitlab.PackageFile{ID: s.id(), PackageID: record.ID, FileName: fileName, Size: int64(len(content)), FileSHA256: hex.EncodeToString(sum[:])},
		content:     content,
	})
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
