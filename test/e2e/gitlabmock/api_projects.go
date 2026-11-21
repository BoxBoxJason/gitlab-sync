package gitlabmock

import (
	"fmt"
	"net/http"

	"github.com/go-git/go-git/v5/plumbing"
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

func (s *Server) listProjects(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	hideArchived := request.URL.Query().Get("archived") == "false"
	projects := make([]gitlab.Project, 0, len(s.projects))

	for _, candidate := range s.projects {
		if hideArchived && candidate.Archived {
			continue
		}

		projects = append(projects, candidate.Project)
	}

	writeJSON(writer, http.StatusOK, paginate(writer, request, projects))
}

func (s *Server) getProject(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.lookupProject(request.PathValue("id"))
	if found == nil {
		writeError(writer, http.StatusNotFound, "404 Project Not Found")

		return
	}

	writeJSON(writer, http.StatusOK, found.Project)
}

func (s *Server) createProject(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.CreateProjectOptions

	err := decodeBody(request, &options)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	name, projectPath := deref(options.Name), deref(options.Path)
	if projectPath == "" {
		projectPath = name
	}

	if name == "" {
		name = projectPath
	}

	if name == "" {
		writeError(writer, http.StatusBadRequest, "name or path is required")

		return
	}

	namespace, ok := s.projectNamespace(options.NamespaceID)
	if !ok {
		writeError(writer, http.StatusNotFound, "404 Namespace Not Found")

		return
	}

	if s.projectByPath(namespace.FullPath+"/"+projectPath) != nil {
		writeError(writer, http.StatusBadRequest, "Failed to save project: path has already been taken")

		return
	}

	visibility := "private"
	if options.Visibility != nil {
		visibility = string(*options.Visibility)
	}

	created := s.newProject(name, projectPath, namespace, visibility)
	created.Description = deref(options.Description)

	if branch := deref(options.DefaultBranch); branch != "" {
		created.DefaultBranch = branch
	}

	if options.MirrorTriggerBuilds != nil {
		created.MirrorTriggerBuilds = *options.MirrorTriggerBuilds
	}

	writeJSON(writer, http.StatusCreated, created.Project)
}

// projectNamespace resolves the namespace of a new project: the group with the
// given id, or the namespace of the authenticated user.
func (s *Server) projectNamespace(namespaceID *int64) (*group, bool) {
	if namespaceID != nil {
		found := s.groupByID(*namespaceID)

		return found, found != nil
	}

	personal, err := s.ensureGroup(DefaultUser)
	if err != nil {
		return nil, false
	}

	return personal, true
}

func (s *Server) editProject(writer http.ResponseWriter, request *http.Request) {
	upload, err := readAvatar(writer, request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())

		return
	}

	var options gitlab.EditProjectOptions

	if !upload.uploaded {
		err = decodeBody(request, &options)
		if err != nil {
			writeError(writer, http.StatusBadRequest, err.Error())

			return
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.lookupProject(request.PathValue("id"))
	if found == nil {
		writeError(writer, http.StatusNotFound, "404 Project Not Found")

		return
	}

	if upload.uploaded {
		found.avatar = upload.content
		found.AvatarURL = fmt.Sprintf("%s/uploads/-/system/project/avatar/%d/avatar.png", s.publicBase, found.ID)
	}

	if message := found.apply(&options); message != "" {
		writeError(writer, http.StatusBadRequest, message)

		return
	}

	writeJSON(writer, http.StatusOK, found.Project)
}

// apply copies the edited attributes onto the project. It returns the error
// message GitLab would answer with, or an empty string.
func (p *project) apply(options *gitlab.EditProjectOptions) string {
	if options.DefaultBranch != nil && *options.DefaultBranch != p.DefaultBranch {
		message := p.changeDefaultBranch(*options.DefaultBranch)
		if message != "" {
			return message
		}
	}

	if options.Mirror != nil && *options.Mirror && p.ImportURL == "" {
		return "Import url can't be blank"
	}

	assign(&p.Name, options.Name)
	assign(&p.Description, options.Description)
	assign(&p.Visibility, options.Visibility)
	assign(&p.Topics, options.Topics)
	assign(&p.Mirror, options.Mirror)
	assign(&p.MirrorTriggerBuilds, options.MirrorTriggerBuilds)
	assign(&p.MirrorOverwritesDivergedBranches, options.MirrorOverwritesDivergedBranches)
	assign(&p.CICDCatalogEnabled, options.CICDCatalogEnabled)

	return ""
}

// changeDefaultBranch makes branch the default one. It returns the error message
// GitLab would answer with, or an empty string.
func (p *project) changeDefaultBranch(branch string) string {
	repository, err := p.repo()
	if err != nil {
		return err.Error()
	}

	// GitLab refuses a default branch the repository does not have (yet).
	_, err = repository.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		return fmt.Sprintf("Failed to change the default branch: branch %q does not exist", branch)
	}

	err = repository.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(branch)))
	if err != nil {
		return fmt.Sprintf("Failed to change the default branch: %v", err)
	}

	p.DefaultBranch = branch

	return ""
}

// assign sets target to what value points to, when it points to something.
func assign[T any](target, value *T) {
	if value != nil {
		*target = *value
	}
}

func (s *Server) configurePullMirror(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.ConfigureProjectPullMirrorOptions

	err := decodeBody(request, &options)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.lookupProject(request.PathValue("id"))
	if found == nil {
		writeError(writer, http.StatusNotFound, "404 Project Not Found")

		return
	}

	if options.URL != nil {
		found.ImportURL = *options.URL
	}

	if options.Enabled != nil {
		found.Mirror = *options.Enabled
	}

	if options.MirrorTriggerBuilds != nil {
		found.MirrorTriggerBuilds = *options.MirrorTriggerBuilds
	}

	if options.MirrorOverwritesDivergedBranches != nil {
		found.MirrorOverwritesDivergedBranches = *options.MirrorOverwritesDivergedBranches
	}

	writeJSON(writer, http.StatusOK, map[string]any{"id": found.ID, "enabled": found.Mirror, "url": found.ImportURL})
}
