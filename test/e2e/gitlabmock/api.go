package gitlabmock

import (
	"net/http"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

// registerRoutes declares the REST endpoints the fake serves. Anything else
// answers 501 (see Server.notImplemented).
func (s *Server) registerRoutes() {
	routes := map[string]http.HandlerFunc{
		"GET /api/v4/user":     withoutRequest(serveUser),
		"GET /api/v4/metadata": withoutRequest(s.serveMetadata),
		"GET /api/v4/license":  withoutRequest(s.serveLicense),

		"GET /api/v4/groups":                    s.listGroups,
		"POST /api/v4/groups":                   s.createGroup,
		"GET /api/v4/groups/{id}":               s.getGroup,
		"PUT /api/v4/groups/{id}":               s.updateGroup,
		"GET /api/v4/groups/{id}/avatar":        s.serveGroupAvatar,
		"GET /api/v4/projects/{id}/avatar":      s.serveProjectAvatar,
		"GET /api/v4/groups/{id}/subgroups":     s.listSubgroups,
		"GET /api/v4/groups/{id}/projects":      s.listGroupProjects,
		"POST /api/v4/groups/{id}/members":      s.createGroupMember,
		"GET /api/v4/projects":                  s.listProjects,
		"POST /api/v4/projects":                 s.createProject,
		"GET /api/v4/projects/{id}":             s.getProject,
		"PUT /api/v4/projects/{id}":             s.editProject,
		"PUT /api/v4/projects/{id}/mirror/pull": s.configurePullMirror,

		"POST /api/v4/projects/{id}/members":          s.createProjectMember,
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

// withoutRequest adapts a handler that does not read the request.
func withoutRequest(handler func(http.ResponseWriter)) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) { //nolint:unused // http.HandlerFunc imposes the request parameter
		handler(writer)
	}
}

func serveUser(writer http.ResponseWriter) {
	writeJSON(writer, http.StatusOK, gitlab.User{ID: userID, Username: DefaultUser, Name: "Mirror Bot"})
}

func (s *Server) serveMetadata(writer http.ResponseWriter) {
	s.mu.Lock()
	version := s.version
	s.mu.Unlock()

	writeJSON(writer, http.StatusOK, gitlab.Metadata{Version: version, Revision: "mock"})
}

func (s *Server) serveLicense(writer http.ResponseWriter) {
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
