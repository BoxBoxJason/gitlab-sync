package gitlabmock

import (
	"net/http"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

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

	writeJSON(writer, http.StatusOK, paginate(writer, request, issues))
}

func (s *Server) createIssue(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.CreateIssueOptions

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

	issue := s.newIssue(found, deref(options.Title), deref(options.Description), "opened")
	if options.Labels != nil {
		issue.Labels = gitlab.Labels(*options.Labels)
	}

	found.issues = append(found.issues, issue)

	writeJSON(writer, http.StatusCreated, issue)
}

func (s *Server) updateIssue(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.UpdateIssueOptions

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

	iid := pathInt64(request, "iid")

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
		default:
			// no state change
		}

		writeJSON(writer, http.StatusOK, issue)

		return
	}

	writeError(writer, http.StatusNotFound, "404 Not found")
}
