package gitlabmock

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

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

		if version := query.Get("package_version"); version != "" && version != record.Version {
			continue
		}

		packages = append(packages, record.Package)
	}

	writeJSON(writer, http.StatusOK, paginate(writer, request, packages))
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

	writeJSON(writer, http.StatusOK, paginate(writer, request, files))
}

func (s *Server) deletePackageFile(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record := s.packageTarget(writer, request)
	if record == nil {
		return
	}

	fileID := pathInt64(request, "file")

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

	packageID := pathInt64(request, "package")

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

		// GitLab serves the newest of the files sharing a name.
		for _, file := range slices.Backward(record.files) {
			if file.FileName == request.PathValue("file") {
				sendBytes(writer, file.content)

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
