package mirroring

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

// releaseAssetsTestServers wires a source and a destination fake GitLab instance
// for the release assets mirroring tests, and records what reaches the destination.
type releaseAssetsTestServers struct {
	destination         *GitlabInstance
	sourceMux           *http.ServeMux
	destinationMux      *http.ServeMux
	uploads             map[string]string
	destinationPackages map[string]testPackage
	source              *GitlabInstance
	sourcePackageFiles  string
	createdReleases     []string
	deletedFiles        []string
	deletedReleases     []string
	deletedLinks        []string
	updatedLinks        []map[string]any
	createdLinks        []map[string]any
	mu                  sync.Mutex
	sourceFileMissing   bool
}

// testPackage is a generic package of a fake instance.
type testPackage struct {
	// files is the JSON answered for the package files.
	files string
	id    int
}

// setDestinationPackage makes the destination project hold the generic package name/version with the given files JSON.
func (s *releaseAssetsTestServers) setDestinationPackage(name, version string, id int, files string) {
	s.destinationPackages[name+"/"+version] = testPackage{id: id, files: files}
}

func newReleaseAssetsTestServers(t *testing.T) *releaseAssetsTestServers {
	t.Helper()

	s := &releaseAssetsTestServers{uploads: make(map[string]string), destinationPackages: make(map[string]testPackage)}
	s.sourceMux, s.source = setupEmptyTestServer(t, RoleSource)
	s.destinationMux, s.destination = setupEmptyTestServer(t, RoleDestination)

	s.destinationMux.HandleFunc(fmt.Sprintf("POST /api/v4/projects/%d/releases", testProject2.ID), func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}
		s.mu.Lock()
		s.createdReleases = append(s.createdReleases, fmt.Sprint(body["tag_name"]))
		s.mu.Unlock()
		writeJSONResponse(w, http.StatusCreated, testReleaseString)
	})
	s.destinationMux.HandleFunc(fmt.Sprintf("PUT /api/v4/projects/%d/packages/generic/{file...}", testProject2.ID), func(w http.ResponseWriter, r *http.Request) {
		content, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		// Spooled assets are streamed with their size, never chunked.
		if r.ContentLength != int64(len(content)) {
			writeJSONResponse(w, http.StatusBadRequest, `{"message": "missing Content-Length"}`)

			return
		}

		s.mu.Lock()
		s.uploads[r.PathValue("file")] = string(content)
		s.mu.Unlock()
		writeJSONResponse(w, http.StatusCreated, `{"message": "201 Created"}`)
	})
	s.destinationMux.HandleFunc(fmt.Sprintf("POST /api/v4/projects/%d/releases/{tag}/assets/links", testProject2.ID), func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}
		body["tag"] = r.PathValue("tag")
		s.mu.Lock()
		s.createdLinks = append(s.createdLinks, body)
		s.mu.Unlock()
		writeJSONResponse(w, http.StatusCreated, `{"id": 1}`)
	})
	s.destinationMux.HandleFunc(fmt.Sprintf("PUT /api/v4/projects/%d/releases/{tag}/assets/links/{link}", testProject2.ID), func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}
		body["tag"] = r.PathValue("tag")
		body["id"] = r.PathValue("link")
		s.mu.Lock()
		s.updatedLinks = append(s.updatedLinks, body)
		s.mu.Unlock()
		writeJSONResponse(w, http.StatusOK, `{"id": 1}`)
	})
	s.destinationMux.HandleFunc(fmt.Sprintf("GET /api/v4/projects/%d/packages", testProject2.ID), func(w http.ResponseWriter, r *http.Request) {
		name, version := r.URL.Query().Get("package_name"), r.URL.Query().Get("package_version")
		pkg, ok := s.destinationPackages[name+"/"+version]
		if !ok {
			writeJSONResponse(w, http.StatusOK, "[]")
			return
		}
		writeJSONResponse(w, http.StatusOK, fmt.Sprintf(`[{"id": %d, "name": %q, "version": %q, "package_type": "generic"}]`, pkg.id, name, version))
	})
	s.destinationMux.HandleFunc(fmt.Sprintf("GET /api/v4/projects/%d/packages/{package}/package_files", testProject2.ID), func(w http.ResponseWriter, r *http.Request) {
		for _, pkg := range s.destinationPackages {
			if strconv.Itoa(pkg.id) == r.PathValue("package") {
				writeJSONResponse(w, http.StatusOK, pkg.files)
				return
			}
		}
		http.NotFound(w, r)
	})
	s.destinationMux.HandleFunc(fmt.Sprintf("DELETE /api/v4/projects/%d/packages/{package}/package_files/{file}", testProject2.ID), func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.deletedFiles = append(s.deletedFiles, r.PathValue("package")+"/"+r.PathValue("file"))
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	s.destinationMux.HandleFunc(fmt.Sprintf("DELETE /api/v4/projects/%d/releases/{tag}", testProject2.ID), func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.deletedReleases = append(s.deletedReleases, r.PathValue("tag"))
		s.mu.Unlock()
		writeJSONResponse(w, http.StatusOK, testReleaseString)
	})
	s.destinationMux.HandleFunc(fmt.Sprintf("DELETE /api/v4/projects/%d/releases/{tag}/assets/links/{link}", testProject2.ID), func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.deletedLinks = append(s.deletedLinks, r.PathValue("tag")+"/"+r.PathValue("link"))
		s.mu.Unlock()
		writeJSONResponse(w, http.StatusOK, `{"id": 1}`)
	})

	return s
}

// destinationPackageURL is the URL the destination link of the app-linux generic package file must point to.
func (s *releaseAssetsTestServers) destinationPackageURL() string {
	return fmt.Sprintf("%sprojects/%d/packages/generic/app/1.0.0/app-linux.tar.gz", s.destination.Gitlab.BaseURL().String(), testProject2.ID)
}

func serveReleases(mux *http.ServeMux, project *gitlab.Project, releases string) {
	mux.HandleFunc(fmt.Sprintf("GET /api/v4/projects/%d/releases", project.ID), func(w http.ResponseWriter, r *http.Request) {
		if !requireGet(w, r) {
			return
		}

		writeJSONResponse(w, http.StatusOK, releases)
	})
}

func (s *releaseAssetsTestServers) sourceRoot() string {
	return instanceRootURL(s.source.Gitlab).String()
}

// sourceReleaseWithAssets returns a source release holding a generic package link,
// a job artifact link and an external link.
func (s *releaseAssetsTestServers) sourceReleaseWithAssets() string {
	root := s.sourceRoot()

	return fmt.Sprintf(`[{
		"tag_name": "v1.0.0",
		"name": "v1.0.0",
		"description": "release",
		"assets": {"links": [
			{"id": 1, "name": "app-linux", "url": "%[1]sapi/v4/projects/%[2]d/packages/generic/app/1.0.0/app-linux.tar.gz", "direct_asset_url": "%[1]sgroup/project/-/releases/v1.0.0/downloads/bin/app-linux", "link_type": "package"},
			{"id": 2, "name": "report", "url": "%[1]sgroup/project/-/jobs/42/artifacts/raw/dist/report.txt?inline=false", "direct_asset_url": "%[1]sgroup/project/-/jobs/42/artifacts/raw/dist/report.txt?inline=false", "link_type": "other"},
			{"id": 3, "name": "docs", "url": "https://docs.example.com/app", "direct_asset_url": "https://docs.example.com/app", "link_type": "runbook"}
		]}
	}]`, root, testProject.ID)
}

// sourcePackageFileSHA256 is the checksum of the source app-linux.tar.gz generic package file.
const (
	sourcePackageFileSHA256 = "5f1ab0b5e1b8c7d6"
)

func (s *releaseAssetsTestServers) serveSourceFiles() {
	s.sourceMux.HandleFunc(fmt.Sprintf("GET /api/v4/projects/%d/packages", testProject.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("package_type") != "generic" || r.URL.Query().Get("package_name") != "app" || r.URL.Query().Get("package_version") != "1.0.0" {
			http.Error(w, "unexpected package filters: "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		// package_name is a partial match: app-extra must be ignored.
		writeJSONResponse(w, http.StatusOK, `[
			{"id": 11, "name": "app-extra", "version": "1.0.0", "package_type": "generic"},
			{"id": 10, "name": "app", "version": "1.0.0", "package_type": "generic"}
		]`)
	})
	if s.sourcePackageFiles == "" {
		s.sourcePackageFiles = fmt.Sprintf(`[
			{"id": 100, "file_name": "app-linux.tar.gz", "size": 15, "file_sha256": "0000", "file_sha1": "old"},
			{"id": 101, "file_name": "app-linux.tar.gz", "size": 15, "file_sha256": %q, "file_sha1": "new"},
			{"id": 102, "file_name": "app-windows.zip", "size": 3, "file_sha256": "ffff"}
		]`, sourcePackageFileSHA256)
	}
	s.sourceMux.HandleFunc(fmt.Sprintf("GET /api/v4/projects/%d/packages/10/package_files", testProject.ID), func(w http.ResponseWriter, r *http.Request) {
		if !requireGet(w, r) {
			return
		}

		writeJSONResponse(w, http.StatusOK, s.sourcePackageFiles)
	})
	s.sourceMux.HandleFunc(fmt.Sprintf("GET /api/v4/projects/%d/packages/generic/app/1.0.0/app-linux.tar.gz", testProject.ID), func(w http.ResponseWriter, r *http.Request) {
		if !requireGet(w, r) {
			return
		}

		if s.sourceFileMissing {
			writeJSONResponse(w, http.StatusNotFound, `{"message": "404 Package Not Found"}`)
			return
		}
		writeBody(w, "package-content")
	})
	s.sourceMux.HandleFunc("GET /group/project/-/jobs/42/artifacts/raw/dist/report.txt", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Private-Token") == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		writeBody(w, "report-content")
	})
}

func (s *releaseAssetsTestServers) linksByName() map[string]map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	links := make(map[string]map[string]any, len(s.createdLinks))
	for _, link := range s.createdLinks {
		links[fmt.Sprint(link["name"])] = link
	}
	return links
}

func (s *releaseAssetsTestServers) deletedReleasesCopy() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Sorted(slices.Values(s.deletedReleases))
}

func (s *releaseAssetsTestServers) deletedLinkList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Sorted(slices.Values(s.deletedLinks))
}

func (s *releaseAssetsTestServers) deletedFileList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Sorted(slices.Values(s.deletedFiles))
}

func (s *releaseAssetsTestServers) updatedLinksCopy() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.updatedLinks...)
}
