package gitlabmock

import (
	"net/http"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

func (s *Server) createGroupMember(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.AddGroupMemberOptions

	err := decodeBody(request, &options)
	if err != nil {
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

	denied := addMember(found.members, options.AccessLevel)
	if denied != nil {
		writeError(writer, denied.status, denied.message)

		return
	}

	writeJSON(writer, http.StatusCreated, gitlab.GroupMember{ID: userID, Username: DefaultUser, AccessLevel: *options.AccessLevel})
}

// addMember makes the authenticated user a member of an entity, whose members
// are given. It returns the error GitLab would answer with, if any.
func addMember(members map[int64]int64, level *gitlab.AccessLevelValue) *apiError {
	if level == nil {
		return &apiError{status: http.StatusBadRequest, message: "access_level is required"}
	}

	if _, exists := members[userID]; exists {
		return &apiError{status: http.StatusConflict, message: "Member already exists"}
	}

	members[userID] = int64(*level)

	return nil
}

// apiError is an error answer of the API.
type apiError struct {
	message string
	status  int
}

func (s *Server) createProjectMember(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.AddProjectMemberOptions

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

	denied := addMember(found.members, options.AccessLevel)
	if denied != nil {
		writeError(writer, denied.status, denied.message)

		return
	}

	writeJSON(writer, http.StatusCreated, gitlab.ProjectMember{ID: userID, Username: DefaultUser, AccessLevel: *options.AccessLevel})
}

func (s *Server) editProjectMember(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.EditProjectMemberOptions

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

	user := pathInt64(request, "user")
	if _, exists := found.members[user]; !exists || options.AccessLevel == nil {
		writeError(writer, http.StatusNotFound, "404 Not found")

		return
	}

	found.members[user] = int64(*options.AccessLevel)

	writeJSON(writer, http.StatusOK, gitlab.ProjectMember{ID: user, Username: DefaultUser, AccessLevel: *options.AccessLevel})
}
