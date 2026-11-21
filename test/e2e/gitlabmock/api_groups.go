package gitlabmock

import (
	"fmt"
	"net/http"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

func (s *Server) listGroups(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	groups := make([]gitlab.Group, 0, len(s.groups))
	for _, candidate := range s.groups {
		groups = append(groups, *candidate.Group)
	}

	writeJSON(writer, http.StatusOK, paginate(writer, request, groups))
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

	err := decodeBody(request, &options)
	if err != nil {
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
	upload, err := readAvatar(writer, request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())

		return
	}

	var options gitlab.UpdateGroupOptions

	if !upload.uploaded {
		err = decodeBody(request, &options)
		if err != nil {
			writeError(writer, http.StatusBadRequest, err.Error())

			return
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.lookupGroup(request.PathValue("id"))
	if found == nil {
		writeError(writer, http.StatusNotFound, "404 Group Not Found")

		return
	}

	if upload.uploaded {
		found.avatar = upload.content
		found.AvatarURL = fmt.Sprintf("%s/uploads/-/system/group/avatar/%d/avatar.png", s.publicBase, found.ID)
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
			subgroups = append(subgroups, *candidate.Group)
		}
	}

	writeJSON(writer, http.StatusOK, paginate(writer, request, subgroups))
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
	hideArchived := request.URL.Query().Get("archived") == "false"

	var projects []gitlab.Project

	for _, candidate := range s.projects {
		if hideArchived && candidate.Archived {
			continue
		}

		namespace := candidate.Namespace.FullPath
		if namespace == parent.FullPath || includeSubgroups && strings.HasPrefix(namespace, parent.FullPath+"/") {
			projects = append(projects, candidate.Project)
		}
	}

	writeJSON(writer, http.StatusOK, paginate(writer, request, projects))
}
