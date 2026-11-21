package gitlabmock

import (
	"net/http"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

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

	writeJSON(writer, http.StatusOK, paginate(writer, request, releases))
}

func (s *Server) createRelease(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.CreateReleaseOptions

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

	err := decodeBody(request, &options)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.releaseTarget(writer, request)
	if target == nil {
		return
	}

	found, tag := target.project, target.tag

	link := s.newReleaseLink(found, tag, deref(options.Name), deref(options.URL), deref(options.DirectAssetPath), linkType(options.LinkType))
	found.links[tag] = append(found.links[tag], link)

	writeJSON(writer, http.StatusCreated, link)
}

func (s *Server) updateReleaseLink(writer http.ResponseWriter, request *http.Request) {
	var options gitlab.UpdateReleaseLinkOptions

	err := decodeBody(request, &options)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.releaseTarget(writer, request)
	if target == nil {
		return
	}

	found, tag := target.project, target.tag

	linkID := pathInt64(request, "link")

	for index, link := range found.links[tag] {
		if link.ID != linkID {
			continue
		}

		name, linkURL := link.Name, link.URL
		if options.Name != nil {
			name = *options.Name
		}

		if options.URL != nil {
			linkURL = *options.URL
		}

		updated := s.newReleaseLink(found, tag, name, linkURL, deref(options.DirectAssetPath), linkType(options.LinkType))
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

	target := s.releaseTarget(writer, request)
	if target == nil {
		return
	}

	found, tag := target.project, target.tag

	linkID := pathInt64(request, "link")

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
// releaseRef is a release of a project, by its tag.
type releaseRef struct {
	project *project
	tag     string
}

func (s *Server) releaseTarget(writer http.ResponseWriter, request *http.Request) *releaseRef {
	found := s.lookupProject(request.PathValue("id"))
	if found == nil {
		writeError(writer, http.StatusNotFound, "404 Project Not Found")

		return nil
	}

	tag := request.PathValue("tag")

	for _, release := range found.releases {
		if release.TagName == tag {
			return &releaseRef{project: found, tag: tag}
		}
	}

	writeError(writer, http.StatusNotFound, "404 Not found")

	return nil
}

func linkType(value *gitlab.LinkTypeValue) string {
	if value == nil {
		return "other"
	}

	return string(*value)
}
