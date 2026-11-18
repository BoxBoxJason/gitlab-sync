package gitlabmock

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

// registerRoutes declares the REST endpoints the fake serves. Anything else
// answers 501 (see Server.notImplemented).
func (s *Server) registerRoutes() {
	routes := map[string]http.HandlerFunc{
		"GET /api/v4/user":     s.getUser,
		"GET /api/v4/metadata": s.getMetadata,
		"GET /api/v4/license":  s.getLicense,

		"GET /api/v4/groups":                    s.listGroups,
		"POST /api/v4/groups":                   s.createGroup,
		"GET /api/v4/groups/{id}":               s.getGroup,
		"PUT /api/v4/groups/{id}":               s.updateGroup,
		"GET /api/v4/groups/{id}/subgroups":     s.listSubgroups,
		"GET /api/v4/groups/{id}/projects":      s.listGroupProjects,
		"POST /api/v4/groups/{id}/members":      s.addGroupMember,
		"GET /api/v4/projects":                  s.listProjects,
		"POST /api/v4/projects":                 s.createProject,
		"GET /api/v4/projects/{id}":             s.getProject,
		"PUT /api/v4/projects/{id}":             s.editProject,
		"PUT /api/v4/projects/{id}/mirror/pull": s.configurePullMirror,

		"POST /api/v4/projects/{id}/members":          s.addProjectMember,
		"PUT /api/v4/projects/{id}/members/{user}":    s.editProjectMember,
		"GET /api/v4/projects/{id}/issues":            s.listIssues,
		"POST /api/v4/projects/{id}/issues":           s.createIssue,
		"PUT /api/v4/projects/{id}/issues/{iid}":      s.updateIssue,
		"GET /api/v4/projects/{id}/releases":          s.listReleases,
		"POST /api/v4/projects/{id}/releases":         s.createRelease,
		"DELETE /api/v4/projects/{id}/releases/{tag}": s.deleteRelease,

		"POST /api/v4/projects/{id}/releases/{tag}/assets/links":          s.createReleaseLink,
		"PUT /api/v4/projects/{id}/releases/{tag}/assets/links/{link}":    s.updateReleaseLink,
		"DELETE /api/v4/projects/{id}/releases/{tag}/assets/links/{link}": s.deleteReleaseLink,
		"GET /{path...}":                     s.gitOrDownload,
		"POST /{path...}":                    s.gitPost,
		"GET /api/v4/projects/{id}/packages": s.listPackages,
		"GET /api/v4/projects/{id}/packages/{package}/package_files":            s.listPackageFiles,
		"DELETE /api/v4/projects/{id}/packages/{package}/package_files/{file}":  s.deletePackageFile,
		"PUT /api/v4/projects/{id}/packages/generic/{name}/{version}/{file...}": s.publishGenericFile,
		"GET /api/v4/projects/{id}/packages/generic/{name}/{version}/{file...}": s.downloadGenericFile,
	}

	for pattern, handler := range routes {
		s.mux.HandleFunc(pattern, handler)
	}
}

// --- instance ---------------------------------------------------------------

func (s *Server) getUser(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, gitlab.User{ID: userID, Username: DefaultUser, Name: "Mirror Bot"})
}

func (s *Server) getMetadata(writer http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	version := s.version
	s.mu.Unlock()

	writeJSON(writer, http.StatusOK, gitlab.Metadata{Version: version, Revision: "mock"})
}

func (s *Server) getLicense(writer http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	plan, missing := s.plan, s.noLicense
	s.mu.Unlock()

	if missing {
		writeError(writer, http.StatusNotFound, "404 Not Found")

		return
	}

	if plan == "" {
		writeJSON(writer, http.StatusOK, nil)

		return
	}

	writeJSON(writer, http.StatusOK, gitlab.License{ID: 1, Plan: plan})
}

// --- groups -----------------------------------------------------------------

func (s *Server) listGroups(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	groups := make([]gitlab.Group, 0, len(s.groups))
	for _, candidate := range s.groups {
		groups = append(groups, candidate.Group)
	}

	start, end := paginate(writer, request, len(groups))
	writeJSON(writer, http.StatusOK, groups[start:end])
}

func (s *Server) getGroup(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.lookupGroup(request.PathValue("id"))
	if found == nil {
		writeError(writer, http.StatusNotFound, "404 Group Not Found")

		return
	}

	writeJSON(writer, http.StatusOK, found.Group)
}

func (s *Server) createGroup(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.CreateGroupOptions
	if err := decodeBody(request, &options); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	name, groupPath := deref(options.Name), deref(options.Path)
	if name == "" || groupPath == "" {
		writeError(writer, http.StatusBadRequest, "name and path are required")

		return
	}

	var parentID int64
	if options.ParentID != nil {
		parentID = *options.ParentID
		if s.groupByID(parentID) == nil {
			writeError(writer, http.StatusNotFound, "404 Parent Group Not Found")

			return
		}
	}

	visibility := "private"
	if options.Visibility != nil {
		visibility = string(*options.Visibility)
	}

	created := s.newGroup(name, groupPath, parentID, visibility, deref(options.Description))

	writeJSON(writer, http.StatusCreated, created.Group)
}

func (s *Server) updateGroup(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.UpdateGroupOptions
	if err := decodeBody(request, &options); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.lookupGroup(request.PathValue("id"))
	if found == nil {
		writeError(writer, http.StatusNotFound, "404 Group Not Found")

		return
	}

	if options.Name != nil {
		found.Name = *options.Name
	}

	if options.Description != nil {
		found.Description = *options.Description
	}

	if options.Visibility != nil {
		found.Visibility = *options.Visibility
	}

	writeJSON(writer, http.StatusOK, found.Group)
}

func (s *Server) listSubgroups(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	parent := s.lookupGroup(request.PathValue("id"))
	if parent == nil {
		writeError(writer, http.StatusNotFound, "404 Group Not Found")

		return
	}

	var subgroups []gitlab.Group

	for _, candidate := range s.groups {
		if candidate.ParentID == parent.ID {
			subgroups = append(subgroups, candidate.Group)
		}
	}

	start, end := paginate(writer, request, len(subgroups))
	writeJSON(writer, http.StatusOK, subgroups[start:end])
}

func (s *Server) listGroupProjects(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	parent := s.lookupGroup(request.PathValue("id"))
	if parent == nil {
		writeError(writer, http.StatusNotFound, "404 Group Not Found")

		return
	}

	includeSubgroups := request.URL.Query().Get("include_subgroups") == "true"

	var projects []gitlab.Project

	for _, candidate := range s.projects {
		namespace := candidate.Namespace.FullPath
		if namespace == parent.FullPath || includeSubgroups && strings.HasPrefix(namespace, parent.FullPath+"/") {
			projects = append(projects, candidate.Project)
		}
	}

	start, end := paginate(writer, request, len(projects))
	writeJSON(writer, http.StatusOK, projects[start:end])
}

func (s *Server) addGroupMember(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.AddGroupMemberOptions
	if err := decodeBody(request, &options); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.lookupGroup(request.PathValue("id"))
	if found == nil {
		writeError(writer, http.StatusNotFound, "404 Group Not Found")

		return
	}

	if options.AccessLevel == nil {
		writeError(writer, http.StatusBadRequest, "access_level is required")

		return
	}

	if _, exists := found.members[userID]; exists {
		writeError(writer, http.StatusConflict, "Member already exists")

		return
	}

	found.members[userID] = int64(*options.AccessLevel)

	writeJSON(writer, http.StatusCreated, gitlab.GroupMember{ID: userID, Username: DefaultUser, AccessLevel: *options.AccessLevel})
}

// --- projects ---------------------------------------------------------------

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

	start, end := paginate(writer, request, len(projects))
	writeJSON(writer, http.StatusOK, projects[start:end])
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
	if err := decodeBody(request, &options); err != nil {
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
	if strings.HasPrefix(request.Header.Get("Content-Type"), "multipart/") {
		s.notImplemented(writer, request)

		return
	}

	var options gitlab.EditProjectOptions
	if err := decodeBody(request, &options); err != nil {
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
		repository, err := p.repo()
		if err != nil {
			return err.Error()
		}

		// GitLab refuses a default branch the repository does not have (yet).
		_, err = repository.Reference(plumbing.NewBranchReferenceName(*options.DefaultBranch), true)
		if err != nil {
			return fmt.Sprintf("Failed to change the default branch: branch %q does not exist", *options.DefaultBranch)
		}

		p.DefaultBranch = *options.DefaultBranch

		_ = repository.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(p.DefaultBranch)))
	}

	if options.Mirror != nil && *options.Mirror && p.ImportURL == "" {
		return "Import url can't be blank"
	}

	if options.Name != nil {
		p.Name = *options.Name
	}

	if options.Description != nil {
		p.Description = *options.Description
	}

	if options.Visibility != nil {
		p.Visibility = *options.Visibility
	}

	if options.Topics != nil {
		p.Topics = *options.Topics
	}

	if options.Mirror != nil {
		p.Mirror = *options.Mirror
	}

	if options.MirrorTriggerBuilds != nil {
		p.MirrorTriggerBuilds = *options.MirrorTriggerBuilds
	}

	if options.MirrorOverwritesDivergedBranches != nil {
		p.MirrorOverwritesDivergedBranches = *options.MirrorOverwritesDivergedBranches
	}

	if options.CICDCatalogEnabled != nil {
		p.CICDCatalogEnabled = *options.CICDCatalogEnabled
	}

	return ""
}

func (s *Server) configurePullMirror(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.ConfigureProjectPullMirrorOptions
	if err := decodeBody(request, &options); err != nil {
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

func (s *Server) addProjectMember(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.AddProjectMemberOptions
	if err := decodeBody(request, &options); err != nil {
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

	if options.AccessLevel == nil {
		writeError(writer, http.StatusBadRequest, "access_level is required")

		return
	}

	if _, exists := found.members[userID]; exists {
		writeError(writer, http.StatusConflict, "Member already exists")

		return
	}

	found.members[userID] = int64(*options.AccessLevel)

	writeJSON(writer, http.StatusCreated, gitlab.ProjectMember{ID: userID, Username: DefaultUser, AccessLevel: *options.AccessLevel})
}

func (s *Server) editProjectMember(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.EditProjectMemberOptions
	if err := decodeBody(request, &options); err != nil {
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

	user, _ := strconv.ParseInt(request.PathValue("user"), 10, 64)
	if _, exists := found.members[user]; !exists || options.AccessLevel == nil {
		writeError(writer, http.StatusNotFound, "404 Not found")

		return
	}

	found.members[user] = int64(*options.AccessLevel)

	writeJSON(writer, http.StatusOK, gitlab.ProjectMember{ID: user, Username: DefaultUser, AccessLevel: *options.AccessLevel})
}

// --- issues -----------------------------------------------------------------

func (s *Server) listIssues(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.lookupProject(request.PathValue("id"))
	if found == nil {
		writeError(writer, http.StatusNotFound, "404 Project Not Found")

		return
	}

	state := request.URL.Query().Get("state")
	issues := make([]*gitlab.Issue, 0, len(found.issues))

	for _, issue := range found.issues {
		if state == "" || state == "all" || state == issue.State {
			issues = append(issues, issue)
		}
	}

	start, end := paginate(writer, request, len(issues))
	writeJSON(writer, http.StatusOK, issues[start:end])
}

func (s *Server) createIssue(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.CreateIssueOptions
	if err := decodeBody(request, &options); err != nil {
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

	issue := s.newIssue(found, deref(options.Title), deref(options.Description), "opened")
	if options.Labels != nil {
		issue.Labels = gitlab.Labels(*options.Labels)
	}

	found.issues = append(found.issues, issue)

	writeJSON(writer, http.StatusCreated, issue)
}

func (s *Server) updateIssue(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.UpdateIssueOptions
	if err := decodeBody(request, &options); err != nil {
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

	iid, _ := strconv.ParseInt(request.PathValue("iid"), 10, 64)

	for _, issue := range found.issues {
		if issue.IID != iid {
			continue
		}

		if options.Title != nil {
			issue.Title = *options.Title
		}

		if options.Description != nil {
			issue.Description = *options.Description
		}

		switch deref(options.StateEvent) {
		case "close":
			issue.State = "closed"
		case "reopen":
			issue.State = "opened"
		}

		writeJSON(writer, http.StatusOK, issue)

		return
	}

	writeError(writer, http.StatusNotFound, "404 Not found")
}

// --- releases ---------------------------------------------------------------

func (s *Server) listReleases(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.lookupProject(request.PathValue("id"))
	if found == nil {
		writeError(writer, http.StatusNotFound, "404 Project Not Found")

		return
	}

	releases := make([]gitlab.Release, 0, len(found.releases))
	for _, release := range found.releases {
		releases = append(releases, found.releaseView(release))
	}

	start, end := paginate(writer, request, len(releases))
	writeJSON(writer, http.StatusOK, releases[start:end])
}

func (s *Server) createRelease(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.CreateReleaseOptions
	if err := decodeBody(request, &options); err != nil {
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

	tag := deref(options.TagName)
	for _, release := range found.releases {
		if release.TagName == tag {
			writeError(writer, http.StatusConflict, "Release already exists")

			return
		}
	}

	now := time.Now().UTC()
	release := &gitlab.Release{TagName: tag, Name: deref(options.Name), Description: deref(options.Description), CreatedAt: &now, ReleasedAt: &now}

	if options.ReleasedAt != nil {
		release.ReleasedAt = options.ReleasedAt
	}

	found.releases = append(found.releases, release)

	if options.Assets != nil {
		for _, link := range options.Assets.Links {
			found.links[tag] = append(found.links[tag], s.newReleaseLink(found, tag, deref(link.Name), deref(link.URL), deref(link.DirectAssetPath), linkType(link.LinkType)))
		}
	}

	writeJSON(writer, http.StatusCreated, found.releaseView(release))
}

func (s *Server) deleteRelease(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.lookupProject(request.PathValue("id"))
	if found == nil {
		writeError(writer, http.StatusNotFound, "404 Project Not Found")

		return
	}

	tag := request.PathValue("tag")

	for index, release := range found.releases {
		if release.TagName == tag {
			found.releases = append(found.releases[:index], found.releases[index+1:]...)
			delete(found.links, tag)

			writeJSON(writer, http.StatusOK, release)

			return
		}
	}

	writeError(writer, http.StatusNotFound, "404 Not found")
}

func (s *Server) createReleaseLink(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.CreateReleaseLinkOptions
	if err := decodeBody(request, &options); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	found, tag := s.releaseTarget(writer, request)
	if found == nil {
		return
	}

	link := s.newReleaseLink(found, tag, deref(options.Name), deref(options.URL), deref(options.DirectAssetPath), linkType(options.LinkType))
	found.links[tag] = append(found.links[tag], link)

	writeJSON(writer, http.StatusCreated, link)
}

func (s *Server) updateReleaseLink(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.UpdateReleaseLinkOptions
	if err := decodeBody(request, &options); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	found, tag := s.releaseTarget(writer, request)
	if found == nil {
		return
	}

	linkID, _ := strconv.ParseInt(request.PathValue("link"), 10, 64)

	for index, link := range found.links[tag] {
		if link.ID != linkID {
			continue
		}

		name, linkURL, directPath := link.Name, link.URL, ""
		if options.Name != nil {
			name = *options.Name
		}

		if options.URL != nil {
			linkURL = *options.URL
		}

		directPath = deref(options.DirectAssetPath)
		updated := s.newReleaseLink(found, tag, name, linkURL, directPath, linkType(options.LinkType))
		updated.ID = link.ID
		found.links[tag][index] = updated

		writeJSON(writer, http.StatusOK, updated)

		return
	}

	writeError(writer, http.StatusNotFound, "404 Not found")
}

func (s *Server) deleteReleaseLink(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found, tag := s.releaseTarget(writer, request)
	if found == nil {
		return
	}

	linkID, _ := strconv.ParseInt(request.PathValue("link"), 10, 64)

	for index, link := range found.links[tag] {
		if link.ID == linkID {
			found.links[tag] = append(found.links[tag][:index], found.links[tag][index+1:]...)

			writeJSON(writer, http.StatusOK, link)

			return
		}
	}

	writeError(writer, http.StatusNotFound, "404 Not found")
}

// releaseTarget resolves the project and release tag of a request, answering 404
// itself (and returning nil) when either is missing. The caller holds the lock.
func (s *Server) releaseTarget(writer http.ResponseWriter, request *http.Request) (*project, string) {
	found := s.lookupProject(request.PathValue("id"))
	if found == nil {
		writeError(writer, http.StatusNotFound, "404 Project Not Found")

		return nil, ""
	}

	tag := request.PathValue("tag")

	for _, release := range found.releases {
		if release.TagName == tag {
			return found, tag
		}
	}

	writeError(writer, http.StatusNotFound, "404 Not found")

	return nil, ""
}

func linkType(value *gitlab.LinkTypeValue) string {
	if value == nil {
		return "other"
	}

	return string(*value)
}

// --- packages ---------------------------------------------------------------

func (s *Server) listPackages(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.lookupProject(request.PathValue("id"))
	if found == nil {
		writeError(writer, http.StatusNotFound, "404 Project Not Found")

		return
	}

	query := request.URL.Query()
	packages := make([]gitlab.Package, 0, len(found.packages))

	for _, record := range found.packages {
		if kind := query.Get("package_type"); kind != "" && kind != record.PackageType {
			continue
		}

		if name := query.Get("package_name"); name != "" && !strings.Contains(record.Name, name) {
			continue
		}

		packages = append(packages, record.Package)
	}

	start, end := paginate(writer, request, len(packages))
	writeJSON(writer, http.StatusOK, packages[start:end])
}

func (s *Server) listPackageFiles(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record := s.packageTarget(writer, request)
	if record == nil {
		return
	}

	files := make([]gitlab.PackageFile, 0, len(record.files))
	for _, file := range record.files {
		files = append(files, file.PackageFile)
	}

	start, end := paginate(writer, request, len(files))
	writeJSON(writer, http.StatusOK, files[start:end])
}

func (s *Server) deletePackageFile(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record := s.packageTarget(writer, request)
	if record == nil {
		return
	}

	fileID, _ := strconv.ParseInt(request.PathValue("file"), 10, 64)

	for index, file := range record.files {
		if file.ID == fileID {
			record.files = append(record.files[:index], record.files[index+1:]...)

			writer.WriteHeader(http.StatusNoContent)

			return
		}
	}

	writeError(writer, http.StatusNotFound, "404 Not found")
}

// packageTarget resolves the package of a request, answering 404 itself (and
// returning nil) when it is missing. The caller holds the lock.
func (s *Server) packageTarget(writer http.ResponseWriter, request *http.Request) *packageRecord {
	found := s.lookupProject(request.PathValue("id"))
	if found == nil {
		writeError(writer, http.StatusNotFound, "404 Project Not Found")

		return nil
	}

	packageID, _ := strconv.ParseInt(request.PathValue("package"), 10, 64)

	for _, record := range found.packages {
		if record.ID == packageID {
			return record
		}
	}

	writeError(writer, http.StatusNotFound, "404 Package Not Found")

	return nil
}

func (s *Server) publishGenericFile(writer http.ResponseWriter, request *http.Request) {
	content, err := io.ReadAll(request.Body)
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

	s.publishPackageFile(found, request.PathValue("name"), request.PathValue("version"), request.PathValue("file"), content)

	sum := sha256.Sum256(content)
	writeJSON(writer, http.StatusCreated, map[string]string{"message": "201 Created", "sha256": hex.EncodeToString(sum[:])})
}

func (s *Server) downloadGenericFile(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.lookupProject(request.PathValue("id"))
	if found == nil {
		writeError(writer, http.StatusNotFound, "404 Project Not Found")

		return
	}

	for _, record := range found.packages {
		if record.Name != request.PathValue("name") || record.Version != request.PathValue("version") {
			continue
		}

		for _, file := range record.files {
			if file.FileName == request.PathValue("file") {
				_, _ = writer.Write(file.content)

				return
			}
		}
	}

	writeError(writer, http.StatusNotFound, "404 Not found")
}

// downloadRedirect serves a release direct asset URL
// (/:project/-/releases/:tag/downloads/:path) the way GitLab does: a redirect to
// the URL of the matching release link. It reports whether the request was one.
func (s *Server) downloadRedirect(writer http.ResponseWriter, request *http.Request) bool {
	projectPath, rest, found := strings.Cut(strings.TrimPrefix(request.URL.Path, "/"), "/-/releases/")
	if !found {
		return false
	}

	tag, assetPath, found := strings.Cut(rest, "/downloads")
	if !found {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(projectPath)
	if target == nil {
		writeError(writer, http.StatusNotFound, "404 Project Not Found")

		return true
	}

	for _, link := range target.links[tag] {
		if strings.HasSuffix(link.DirectAssetURL, "/downloads"+assetPath) {
			target, err := url.Parse(link.URL)
			if err == nil {
				http.Redirect(writer, request, target.String(), http.StatusFound)

				return true
			}
		}
	}

	writeError(writer, http.StatusNotFound, "404 Not found")

	return true
}

func deref[T any](value *T) T {
	var zero T

	if value == nil {
		return zero
	}

	return *value
}
