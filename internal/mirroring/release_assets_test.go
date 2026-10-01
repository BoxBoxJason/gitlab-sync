package mirroring

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/boxboxjason/gitlab-sync/internal/utils"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", rawURL, err)
	}
	return u
}

func TestInstanceRootURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{name: "root install", baseURL: "https://gitlab.example.com", want: "https://gitlab.example.com/"},
		{name: "relative URL install", baseURL: "https://example.com/gitlab/", want: "https://example.com/gitlab/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := gitlab.NewClient("", gitlab.WithBaseURL(tt.baseURL))
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}
			if got := instanceRootURL(client).String(); got != tt.want {
				t.Errorf("instanceRootURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInstanceRelativePath(t *testing.T) {
	root := mustParseURL(t, "https://example.com/gitlab/")

	tests := []struct {
		name       string
		link       string
		want       string
		onInstance bool
	}{
		{name: "same instance", link: "https://example.com/gitlab/group/project/-/jobs/1/artifacts/raw/a.txt", want: "group/project/-/jobs/1/artifacts/raw/a.txt", onInstance: true},
		{name: "host is case insensitive", link: "https://EXAMPLE.com/gitlab/api/v4/projects/1", want: "api/v4/projects/1", onInstance: true},
		{name: "other scheme same host", link: "http://example.com/gitlab/a", want: "a", onInstance: true},
		{name: "outside of the relative root", link: "https://example.com/other/a", onInstance: false},
		{name: "other host", link: "https://github.com/gitlab/a", onInstance: false},
		{name: "other port", link: "https://example.com:8443/gitlab/a", onInstance: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, onInstance := instanceRelativePath(root, mustParseURL(t, tt.link))
			if onInstance != tt.onInstance || got != tt.want {
				t.Errorf("instanceRelativePath() = (%q, %v), want (%q, %v)", got, onInstance, tt.want, tt.onInstance)
			}
		})
	}
}

func TestParseGenericPackagePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want genericPackageFile
		ok   bool
	}{
		{
			name: "numeric project ID",
			path: "api/v4/projects/12/packages/generic/app/1.0.0/app.tar.gz",
			want: genericPackageFile{ProjectID: "12", Name: "app", Version: "1.0.0", FileName: "app.tar.gz"},
			ok:   true,
		},
		{
			name: "escaped project path and file name",
			path: "api/v4/projects/group%2Fproject/packages/generic/app/1.0.0/my%2Bfile.bin",
			want: genericPackageFile{ProjectID: "group/project", Name: "app", Version: "1.0.0", FileName: "my+file.bin"},
			ok:   true,
		},
		{
			name: "nested file name",
			path: "api/v4/projects/12/packages/generic/app/1.0.0/linux/amd64/app",
			want: genericPackageFile{ProjectID: "12", Name: "app", Version: "1.0.0", FileName: "linux/amd64/app"},
			ok:   true,
		},
		{name: "missing file name", path: "api/v4/projects/12/packages/generic/app/1.0.0", ok: false},
		{name: "empty file name", path: "api/v4/projects/12/packages/generic/app/1.0.0/", ok: false},
		{name: "other package type", path: "api/v4/projects/12/packages/maven/app/1.0.0/app.jar", ok: false},
		{name: "job artifact", path: "group/project/-/jobs/1/artifacts/raw/dist/app", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseGenericPackagePath(tt.path)
			if ok != tt.ok || got != tt.want {
				t.Errorf("parseGenericPackagePath() = (%+v, %v), want (%+v, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestDirectAssetPath(t *testing.T) {
	tests := []struct {
		name           string
		directAssetURL string
		want           string
	}{
		{name: "direct asset path", directAssetURL: "https://example.com/group/project/-/releases/v1.0.0/downloads/bin/app", want: "/bin/app"},
		{name: "no direct asset path", directAssetURL: "https://example.com/group/project/-/jobs/1/artifacts/raw/app", want: ""},
		{name: "empty", directAssetURL: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := directAssetPath(&gitlab.ReleaseLink{DirectAssetURL: tt.directAssetURL}); got != tt.want {
				t.Errorf("directAssetPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReleaseAssetVersion(t *testing.T) {
	tests := map[string]string{
		"v1.0.0":       "v1.0.0",
		"release/1.0":  "release-1.0",
		"v1..0 beta":   "v1.0-beta",
		".hidden.":     "hidden",
		"":             defaultReleaseAssetVersion,
		"v1.0.0+build": "v1.0.0+build",
	}

	for tag, want := range tests {
		if got := releaseAssetVersion(tag); got != want {
			t.Errorf("releaseAssetVersion(%q) = %q, want %q", tag, got, want)
		}
	}
}

func TestPlanReleaseAssets(t *testing.T) {
	root := mustParseURL(t, "https://gitlab.example.com/")
	release := &gitlab.Release{
		TagName: "release/1.0",
		Assets: gitlab.ReleaseAssets{
			Links: []*gitlab.ReleaseLink{
				// Listed out of order on purpose: the lowest ID keeps the plain file name.
				{ID: 5, Name: "Second report", URL: "https://gitlab.example.com/g/p/-/jobs/2/artifacts/raw/report.txt"},
				{ID: 4, Name: "First report", URL: "https://gitlab.example.com/g/p/-/jobs/1/artifacts/raw/report.txt"},
				{ID: 3, Name: "Package", URL: "https://gitlab.example.com/api/v4/projects/1/packages/generic/app/1.0/app.bin", LinkType: gitlab.PackageLinkType},
				{ID: 2, Name: "Docs", URL: "https://docs.example.com/app"},
				{
					ID:             1,
					Name:           "Download",
					URL:            "https://gitlab.example.com/g/p/-/jobs/3/artifacts/download",
					DirectAssetURL: "https://gitlab.example.com/g/p/-/releases/release%2F1.0/downloads/bin/app-linux",
				},
				nil,
			},
		},
	}

	plans := planReleaseAssets(root, release)
	if len(plans) != 5 {
		t.Fatalf("expected 5 plans, got %d", len(plans))
	}

	byName := make(map[string]*releaseAssetPlan, len(plans))
	for _, plan := range plans {
		byName[plan.Link.Name] = plan
	}

	if plan := byName["Docs"]; plan.Kind != releaseAssetExternal {
		t.Errorf("Docs: expected an external link, got kind %d", plan.Kind)
	}

	if plan := byName["Package"]; plan.Kind != releaseAssetGenericPackage || plan.Package != (genericPackageFile{ProjectID: "1", Name: "app", Version: "1.0", FileName: "app.bin"}) {
		t.Errorf("Package: unexpected plan %+v", plan)
	}

	expectedFiles := map[string]string{
		"Download":      "app-linux",
		"First report":  "report.txt",
		"Second report": "5-report.txt",
	}
	for name, fileName := range expectedFiles {
		plan := byName[name]
		want := genericPackageFile{Name: releaseAssetsPackageName, Version: "release-1.0", FileName: fileName}
		if plan.Kind != releaseAssetInstanceFile || plan.Package != want {
			t.Errorf("%s: got kind %d package %+v, want instance file %+v", name, plan.Kind, plan.Package, want)
		}
	}

	if got := byName["Download"].DirectAssetPath; got != "/bin/app-linux" {
		t.Errorf("Download: direct asset path = %q, want %q", got, "/bin/app-linux")
	}
}

// releaseAssetsTestServers wires a source and a destination fake GitLab instance
// for the release assets mirroring tests, and records what reaches the destination.
type releaseAssetsTestServers struct {
	source         *GitlabInstance
	destination    *GitlabInstance
	sourceMux      *http.ServeMux
	destinationMux *http.ServeMux

	mu              sync.Mutex
	uploads         map[string]string
	createdLinks    []map[string]any
	updatedLinks    []map[string]any
	deletedLinks    []string
	deletedFiles    []string
	deletedReleases []string
	// goneReleases are the tags whose release deletion answers 404, as when GitLab
	// deleted the release along with its tag.
	goneReleases    map[string]bool
	createdReleases []string
	// destinationPackages are the destination generic packages, by "name/version".
	destinationPackages map[string]testPackage
	// sourcePackageFiles is the JSON answered for the files of the source generic
	// package app/1.0.0 (package 10).
	sourcePackageFiles string
	// sourceFileMissing makes the download of the source app-linux.tar.gz answer 404.
	sourceFileMissing bool
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
	s.sourceMux, s.source = setupEmptyTestServer(t, ROLE_SOURCE, INSTANCE_SIZE_SMALL)
	s.destinationMux, s.destination = setupEmptyTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)

	s.destinationMux.HandleFunc(fmt.Sprintf("POST /api/v4/projects/%d/releases", TEST_PROJECT_2.ID), func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		s.createdReleases = append(s.createdReleases, fmt.Sprint(body["tag_name"]))
		s.mu.Unlock()
		writeJSONResponse(w, http.StatusCreated, TEST_RELEASE_STRING)
	})
	s.destinationMux.HandleFunc(fmt.Sprintf("PUT /api/v4/projects/%d/packages/generic/{file...}", TEST_PROJECT_2.ID), func(w http.ResponseWriter, r *http.Request) {
		content, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.uploads[r.PathValue("file")] = string(content)
		s.mu.Unlock()
		writeJSONResponse(w, http.StatusCreated, `{"message": "201 Created"}`)
	})
	s.destinationMux.HandleFunc(fmt.Sprintf("POST /api/v4/projects/%d/releases/{tag}/assets/links", TEST_PROJECT_2.ID), func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["tag"] = r.PathValue("tag")
		s.mu.Lock()
		s.createdLinks = append(s.createdLinks, body)
		s.mu.Unlock()
		writeJSONResponse(w, http.StatusCreated, `{"id": 1}`)
	})
	s.destinationMux.HandleFunc(fmt.Sprintf("PUT /api/v4/projects/%d/releases/{tag}/assets/links/{link}", TEST_PROJECT_2.ID), func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["tag"] = r.PathValue("tag")
		body["id"] = r.PathValue("link")
		s.mu.Lock()
		s.updatedLinks = append(s.updatedLinks, body)
		s.mu.Unlock()
		writeJSONResponse(w, http.StatusOK, `{"id": 1}`)
	})
	s.destinationMux.HandleFunc(fmt.Sprintf("GET /api/v4/projects/%d/packages", TEST_PROJECT_2.ID), func(w http.ResponseWriter, r *http.Request) {
		name, version := r.URL.Query().Get("package_name"), r.URL.Query().Get("package_version")
		pkg, ok := s.destinationPackages[name+"/"+version]
		if !ok {
			writeJSONResponse(w, http.StatusOK, "[]")
			return
		}
		writeJSONResponse(w, http.StatusOK, fmt.Sprintf(`[{"id": %d, "name": %q, "version": %q, "package_type": "generic"}]`, pkg.id, name, version))
	})
	s.destinationMux.HandleFunc(fmt.Sprintf("GET /api/v4/projects/%d/packages/{package}/package_files", TEST_PROJECT_2.ID), func(w http.ResponseWriter, r *http.Request) {
		for _, pkg := range s.destinationPackages {
			if fmt.Sprint(pkg.id) == r.PathValue("package") {
				writeJSONResponse(w, http.StatusOK, pkg.files)
				return
			}
		}
		http.NotFound(w, r)
	})
	s.destinationMux.HandleFunc(fmt.Sprintf("DELETE /api/v4/projects/%d/packages/{package}/package_files/{file}", TEST_PROJECT_2.ID), func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.deletedFiles = append(s.deletedFiles, r.PathValue("package")+"/"+r.PathValue("file"))
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	s.destinationMux.HandleFunc(fmt.Sprintf("DELETE /api/v4/projects/%d/releases/{tag}", TEST_PROJECT_2.ID), func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.deletedReleases = append(s.deletedReleases, r.PathValue("tag"))
		s.mu.Unlock()
		writeJSONResponse(w, http.StatusOK, TEST_RELEASE_STRING)
	})
	s.destinationMux.HandleFunc(fmt.Sprintf("DELETE /api/v4/projects/%d/releases/{tag}/assets/links/{link}", TEST_PROJECT_2.ID), func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.deletedLinks = append(s.deletedLinks, r.PathValue("tag")+"/"+r.PathValue("link"))
		s.mu.Unlock()
		writeJSONResponse(w, http.StatusOK, `{"id": 1}`)
	})

	return s
}

// destinationPackageURL is the URL the destination link of the app-linux generic package file must point to.
func (s *releaseAssetsTestServers) destinationPackageURL() string {
	return fmt.Sprintf("%sprojects/%d/packages/generic/app/1.0.0/app-linux.tar.gz", s.destination.Gitlab.BaseURL().String(), TEST_PROJECT_2.ID)
}

func (s *releaseAssetsTestServers) serveReleases(mux *http.ServeMux, project *gitlab.Project, releases string) {
	mux.HandleFunc(fmt.Sprintf("GET /api/v4/projects/%d/releases", project.ID), func(w http.ResponseWriter, _ *http.Request) {
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
	}]`, root, TEST_PROJECT.ID)
}

// sourcePackageFileSHA256 is the checksum of the source app-linux.tar.gz generic package file.
const sourcePackageFileSHA256 = "5f1ab0b5e1b8c7d6"

func (s *releaseAssetsTestServers) serveSourceFiles() {
	s.sourceMux.HandleFunc(fmt.Sprintf("GET /api/v4/projects/%d/packages", TEST_PROJECT.ID), func(w http.ResponseWriter, r *http.Request) {
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
	s.sourceMux.HandleFunc(fmt.Sprintf("GET /api/v4/projects/%d/packages/10/package_files", TEST_PROJECT.ID), func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, s.sourcePackageFiles)
	})
	s.sourceMux.HandleFunc(fmt.Sprintf("GET /api/v4/projects/%d/packages/generic/app/1.0.0/app-linux.tar.gz", TEST_PROJECT.ID), func(w http.ResponseWriter, _ *http.Request) {
		if s.sourceFileMissing {
			writeJSONResponse(w, http.StatusNotFound, `{"message": "404 Package Not Found"}`)
			return
		}
		fmt.Fprint(w, "package-content")
	})
	s.sourceMux.HandleFunc("GET /group/project/-/jobs/42/artifacts/raw/dist/report.txt", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Private-Token") == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, "report-content")
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

func (s *releaseAssetsTestServers) deletions() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Sorted(slices.Values(s.deletedLinks)), slices.Sorted(slices.Values(s.deletedFiles))
}

func (s *releaseAssetsTestServers) updatedLinksCopy() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.updatedLinks...)
}

func TestMirrorReleasesWithAssets(t *testing.T) {
	helpers.ResetReported()
	t.Cleanup(helpers.ResetReported)

	s := newReleaseAssetsTestServers(t)
	s.serveReleases(s.sourceMux, TEST_PROJECT, s.sourceReleaseWithAssets())
	s.serveReleases(s.destinationMux, TEST_PROJECT_2, "[]")
	s.serveSourceFiles()

	s.destination.MirrorReleases(s.source, TEST_PROJECT, TEST_PROJECT_2, ReleasesMirroringOptions{MirrorAssets: true})

	if got := helpers.ExitCode(); got != 0 {
		t.Fatalf("unexpected error when mirroring releases with assets: exit code %d", got)
	}

	if len(s.createdReleases) != 1 || s.createdReleases[0] != "v1.0.0" {
		t.Errorf("expected release v1.0.0 to be created, got %v", s.createdReleases)
	}

	expectedUploads := map[string]string{
		"app/1.0.0/app-linux.tar.gz":       "package-content",
		"release-assets/v1.0.0/report.txt": "report-content",
	}
	for file, content := range expectedUploads {
		if s.uploads[file] != content {
			t.Errorf("upload %s = %q, want %q (uploads: %v)", file, s.uploads[file], content, s.uploads)
		}
	}
	if len(s.uploads) != len(expectedUploads) {
		t.Errorf("expected %d uploads, got %v", len(expectedUploads), s.uploads)
	}

	destinationAPI := s.destination.Gitlab.BaseURL().String()
	expectedLinks := map[string]map[string]any{
		"app-linux": {
			"url":               fmt.Sprintf("%sprojects/%d/packages/generic/app/1.0.0/app-linux.tar.gz", destinationAPI, TEST_PROJECT_2.ID),
			"direct_asset_path": "/bin/app-linux",
			"link_type":         "package",
		},
		"report": {
			"url":       fmt.Sprintf("%sprojects/%d/packages/generic/release-assets/v1.0.0/report.txt", destinationAPI, TEST_PROJECT_2.ID),
			"link_type": "other",
		},
		"docs": {
			"url":       "https://docs.example.com/app",
			"link_type": "runbook",
		},
	}

	links := s.linksByName()
	if len(links) != len(expectedLinks) {
		t.Fatalf("expected %d links, got %v", len(expectedLinks), links)
	}
	for name, expected := range expectedLinks {
		link, ok := links[name]
		if !ok {
			t.Errorf("link %s was not created", name)
			continue
		}
		if link["tag"] != "v1.0.0" {
			t.Errorf("link %s created on tag %v, want v1.0.0", name, link["tag"])
		}
		for key, value := range expected {
			if link[key] != value {
				t.Errorf("link %s: %s = %v, want %v", name, key, link[key], value)
			}
		}
		if _, ok := expected["direct_asset_path"]; !ok && link["direct_asset_path"] != nil {
			t.Errorf("link %s: unexpected direct_asset_path %v", name, link["direct_asset_path"])
		}
	}
}

func TestMirrorReleasesWithAssetsExistingRelease(t *testing.T) {
	helpers.ResetReported()
	t.Cleanup(helpers.ResetReported)

	s := newReleaseAssetsTestServers(t)
	s.serveReleases(s.sourceMux, TEST_PROJECT, s.sourceReleaseWithAssets())
	s.serveReleases(s.destinationMux, TEST_PROJECT_2, `[{
		"tag_name": "v1.0.0",
		"name": "v1.0.0",
		"assets": {"links": [
			{"id": 9, "name": "app-linux", "url": "https://example.com/already-there"},
			{"id": 8, "name": "report", "url": "https://example.com/report"}
		]}
	}]`)
	s.serveSourceFiles()

	s.destination.MirrorReleases(s.source, TEST_PROJECT, TEST_PROJECT_2, ReleasesMirroringOptions{MirrorAssets: true})

	if got := helpers.ExitCode(); got != 0 {
		t.Fatalf("unexpected error when mirroring releases with assets: exit code %d", got)
	}

	if len(s.createdReleases) != 0 {
		t.Errorf("expected no release to be created, got %v", s.createdReleases)
	}

	// The generic package file is missing on the destination: it is copied, and the
	// existing link is pointed to the copy.
	if s.uploads["app/1.0.0/app-linux.tar.gz"] != "package-content" {
		t.Errorf("expected the missing generic package file to be copied, got uploads %v", s.uploads)
	}
	updated := s.updatedLinksCopy()
	if len(updated) != 1 || updated[0]["id"] != "9" || updated[0]["url"] != s.destinationPackageURL() || updated[0]["direct_asset_path"] != "/bin/app-linux" {
		t.Errorf("expected link 9 to be pointed to the destination copy, got %v", updated)
	}

	// Other links are matched by name only: the existing report link is left alone.
	if _, ok := s.uploads["release-assets/v1.0.0/report.txt"]; ok {
		t.Error("the report already linked on the destination should not be copied again")
	}

	links := s.linksByName()
	if len(links) != 1 || links["docs"] == nil {
		t.Errorf("expected only the missing docs link to be created, got %v", links)
	}
}

func TestMirrorGenericPackageAssetChecksums(t *testing.T) {
	tests := []struct {
		name string
		// destinationFiles is the JSON of the destination package files, empty when the package is missing.
		destinationFiles string
		// existingLinkURL is the URL of the destination app-linux link, empty when there is none.
		existingLinkURL string
		expectUpload    bool
		expectCreate    bool
		expectUpdate    bool
	}{
		{
			name:         "missing file and link",
			expectUpload: true,
			expectCreate: true,
		},
		{
			name:             "up to date",
			destinationFiles: fmt.Sprintf(`[{"id": 200, "file_name": "app-linux.tar.gz", "size": 15, "file_sha256": %q}]`, strings.ToUpper(sourcePackageFileSHA256)),
			existingLinkURL:  "destination",
		},
		{
			name:             "up to date but the link points elsewhere",
			destinationFiles: fmt.Sprintf(`[{"id": 200, "file_name": "app-linux.tar.gz", "size": 15, "file_sha256": %q}]`, sourcePackageFileSHA256),
			existingLinkURL:  "https://example.com/elsewhere",
			expectUpdate:     true,
		},
		{
			name:             "up to date but the link is missing",
			destinationFiles: fmt.Sprintf(`[{"id": 200, "file_name": "app-linux.tar.gz", "size": 15, "file_sha256": %q}]`, sourcePackageFileSHA256),
			expectCreate:     true,
		},
		{
			name:             "checksum differs",
			destinationFiles: `[{"id": 200, "file_name": "app-linux.tar.gz", "size": 15, "file_sha256": "0000"}]`,
			existingLinkURL:  "destination",
			expectUpload:     true,
		},
		{
			name: "newest destination file is compared",
			destinationFiles: fmt.Sprintf(`[
				{"id": 201, "file_name": "app-linux.tar.gz", "size": 15, "file_sha256": "0000"},
				{"id": 200, "file_name": "app-linux.tar.gz", "size": 15, "file_sha256": %q}
			]`, sourcePackageFileSHA256),
			existingLinkURL: "destination",
			expectUpload:    true,
		},
		{
			name:             "only another file in the destination package",
			destinationFiles: `[{"id": 200, "file_name": "app-windows.zip", "size": 3, "file_sha256": "ffff"}]`,
			existingLinkURL:  "destination",
			expectUpload:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			helpers.ResetReported()
			t.Cleanup(helpers.ResetReported)

			s := newReleaseAssetsTestServers(t)
			s.serveSourceFiles()
			if tt.destinationFiles != "" {
				s.setDestinationPackage("app", "1.0.0", 20, tt.destinationFiles)
			}

			var existingLinks []*gitlab.ReleaseLink
			switch tt.existingLinkURL {
			case "":
			case "destination":
				existingLinks = []*gitlab.ReleaseLink{{ID: 9, Name: "app-linux", URL: s.destinationPackageURL()}}
			default:
				existingLinks = []*gitlab.ReleaseLink{{ID: 9, Name: "app-linux", URL: tt.existingLinkURL}}
			}

			release := &gitlab.Release{
				TagName: "v1.0.0",
				Assets: gitlab.ReleaseAssets{Links: []*gitlab.ReleaseLink{{
					ID:       1,
					Name:     "app-linux",
					URL:      fmt.Sprintf("%sapi/v4/projects/%d/packages/generic/app/1.0.0/app-linux.tar.gz", s.sourceRoot(), TEST_PROJECT.ID),
					LinkType: gitlab.PackageLinkType,
				}}},
			}

			s.destination.MirrorReleaseAssets(s.source, TEST_PROJECT_2, release, existingLinks, nil)

			if got := helpers.ExitCode(); got != 0 {
				t.Fatalf("unexpected error when mirroring the asset: exit code %d", got)
			}
			if _, uploaded := s.uploads["app/1.0.0/app-linux.tar.gz"]; uploaded != tt.expectUpload {
				t.Errorf("uploaded = %v, want %v", uploaded, tt.expectUpload)
			}
			if created := len(s.linksByName()) == 1; created != tt.expectCreate {
				t.Errorf("link created = %v, want %v (links: %v)", created, tt.expectCreate, s.linksByName())
			}
			updated := s.updatedLinksCopy()
			if (len(updated) == 1) != tt.expectUpdate {
				t.Errorf("link updated = %v, want %v (updates: %v)", len(updated) == 1, tt.expectUpdate, updated)
			}
			if tt.expectUpdate && updated[0]["url"] != s.destinationPackageURL() {
				t.Errorf("link updated to %v, want %v", updated[0]["url"], s.destinationPackageURL())
			}
		})
	}
}

// The git mirroring deletes the destination tags the source does not have, and
// GitLab deletes the release of a deleted tag with it: the release sync must still
// clean up the files of such a release, known from the snapshot taken before git ran.
func TestMirrorReleasesWithAssetsCleansUpReleasesRemovedByGit(t *testing.T) {
	helpers.ResetReported()
	t.Cleanup(helpers.ResetReported)

	s := newReleaseAssetsTestServers(t)
	s.serveReleases(s.sourceMux, TEST_PROJECT, s.sourceReleaseWithAssets())
	s.serveSourceFiles()
	s.serveReleases(s.destinationMux, TEST_PROJECT_2, "[]")
	s.setDestinationPackage("extra", "1.0", 30, `[{"id": 300, "file_name": "extra.bin", "size": 1}]`)

	releasesBeforeGit := []*gitlab.Release{{
		TagName: "v0.9.0",
		Assets: gitlab.ReleaseAssets{Links: []*gitlab.ReleaseLink{{
			ID:   20,
			Name: "extra.bin",
			URL:  fmt.Sprintf("%sprojects/%d/packages/generic/extra/1.0/extra.bin", s.destination.Gitlab.BaseURL().String(), TEST_PROJECT_2.ID),
		}}},
	}}

	s.destination.MirrorReleases(s.source, TEST_PROJECT, TEST_PROJECT_2, ReleasesMirroringOptions{MirrorAssets: true, DestinationReleasesBeforeGit: releasesBeforeGit})

	if got := helpers.ExitCode(); got != 0 {
		t.Fatalf("a release already deleted with its tag must not be reported: exit code %d", got)
	}
	if _, deletedFiles := s.deletions(); !slices.Equal(deletedFiles, []string{"30/300"}) {
		t.Errorf("deleted files = %v, want the file of the release removed by git", deletedFiles)
	}
	// GitLab answers 403 when deleting a release that no longer exists.
	if deleted := s.deletedReleasesCopy(); len(deleted) != 0 {
		t.Errorf("a release already deleted with its tag must not be deleted again, got %v", deleted)
	}
}

func TestVanishedReleases(t *testing.T) {
	release := func(tag string) *gitlab.Release { return &gitlab.Release{TagName: tag} }

	source := releasesByTag([]*gitlab.Release{release("v1")})
	existing := releasesByTag([]*gitlab.Release{release("v1"), release("v2")})
	beforeGit := []*gitlab.Release{release("v1"), release("v2"), release("v3"), nil}

	vanished := vanishedReleases(source, existing, beforeGit)
	if len(vanished) != 1 || vanished[0].TagName != "v3" {
		t.Errorf("vanishedReleases() = %v, want only v3 (v1 is on the source, v2 still exists)", vanished)
	}
}

func TestMirrorReleaseAssetsDeletesStaleLinks(t *testing.T) {
	helpers.ResetReported()
	t.Cleanup(helpers.ResetReported)

	s := newReleaseAssetsTestServers(t)
	s.serveSourceFiles()
	s.setDestinationPackage("app", "1.0.0", 20, fmt.Sprintf(`[{"id": 200, "file_name": "app-linux.tar.gz", "size": 15, "file_sha256": %q}]`, sourcePackageFileSHA256))
	s.setDestinationPackage("release-assets", "v1.0.0", 21, `[
		{"id": 210, "file_name": "old.bin", "size": 1},
		{"id": 211, "file_name": "old.bin", "size": 2},
		{"id": 212, "file_name": "kept.bin", "size": 3}
	]`)

	destinationAPI := s.destination.Gitlab.BaseURL().String()
	release := &gitlab.Release{
		TagName: "v1.0.0",
		Assets: gitlab.ReleaseAssets{Links: []*gitlab.ReleaseLink{{
			ID:   1,
			Name: "app-linux",
			URL:  fmt.Sprintf("%sapi/v4/projects/%d/packages/generic/app/1.0.0/app-linux.tar.gz", s.sourceRoot(), TEST_PROJECT.ID),
		}}},
	}
	existingLinks := []*gitlab.ReleaseLink{
		{ID: 9, Name: "app-linux", URL: s.destinationPackageURL()},
		// Removed from the source: its copy in the destination project goes with it.
		{ID: 10, Name: "old", URL: fmt.Sprintf("%sprojects/%d/packages/generic/release-assets/v1.0.0/old.bin", destinationAPI, TEST_PROJECT_2.ID)},
		// Removed from the source, but its file is still what app-linux is copied to.
		{ID: 11, Name: "renamed", URL: fmt.Sprintf("%sprojects/%s/packages/generic/app/1.0.0/app-linux.tar.gz", destinationAPI, url.PathEscape(TEST_PROJECT_2.PathWithNamespace))},
		// Removed from the source, pointing to another project: only the link goes.
		{ID: 12, Name: "other-project", URL: fmt.Sprintf("%sprojects/99/packages/generic/release-assets/v1.0.0/kept.bin", destinationAPI)},
		// Removed from the source, external: only the link goes.
		{ID: 13, Name: "external", URL: "https://example.com/app"},
	}

	s.destination.MirrorReleaseAssets(s.source, TEST_PROJECT_2, release, existingLinks, nil)

	if got := helpers.ExitCode(); got != 0 {
		t.Fatalf("unexpected error when mirroring the assets: exit code %d", got)
	}

	deletedLinks, deletedFiles := s.deletions()
	if want := []string{"v1.0.0/10", "v1.0.0/11", "v1.0.0/12", "v1.0.0/13"}; !slices.Equal(deletedLinks, want) {
		t.Errorf("deleted links = %v, want %v", deletedLinks, want)
	}
	if want := []string{"21/210", "21/211"}; !slices.Equal(deletedFiles, want) {
		t.Errorf("deleted files = %v, want %v", deletedFiles, want)
	}
	if len(s.uploads) != 0 || len(s.linksByName()) != 0 || len(s.updatedLinksCopy()) != 0 {
		t.Errorf("expected the up to date asset to be left alone, got uploads %v, links %v, updates %v", s.uploads, s.linksByName(), s.updatedLinksCopy())
	}
}

func TestMirrorReleaseAssetsSharedFileIsKept(t *testing.T) {
	helpers.ResetReported()
	t.Cleanup(helpers.ResetReported)

	s := newReleaseAssetsTestServers(t)
	s.setDestinationPackage("release-assets", "v1.0.0", 21, `[{"id": 210, "file_name": "shared.bin", "size": 1}]`)

	sharedFile := fmt.Sprintf("%sprojects/%d/packages/generic/release-assets/v1.0.0/shared.bin", s.destination.Gitlab.BaseURL().String(), TEST_PROJECT_2.ID)
	// Another release of the project still has a link copied to shared.bin.
	referenced := map[genericPackageFile]struct{}{{Name: "release-assets", Version: "v1.0.0", FileName: "shared.bin"}: {}}

	s.destination.MirrorReleaseAssets(s.source, TEST_PROJECT_2, &gitlab.Release{TagName: "v2.0.0"}, []*gitlab.ReleaseLink{{ID: 10, Name: "shared", URL: sharedFile}}, referenced)

	deletedLinks, deletedFiles := s.deletions()
	if len(deletedLinks) != 1 || len(deletedFiles) != 0 {
		t.Errorf("expected only the link to be deleted, got links %v and files %v", deletedLinks, deletedFiles)
	}
}

func TestMirrorGenericPackageAssetSourceFileDeleted(t *testing.T) {
	tests := []struct {
		name string
		// sourceFiles is the JSON of the source package files.
		sourceFiles       string
		sourceFileMissing bool
		expectDeleted     []string
		expectUpload      bool
	}{
		{
			name:              "deleted on the source",
			sourceFiles:       `[{"id": 102, "file_name": "app-windows.zip", "size": 3}]`,
			sourceFileMissing: true,
			expectDeleted:     []string{"20/200", "20/201"},
		},
		{
			name:         "missing from the listing but downloadable",
			sourceFiles:  `[{"id": 102, "file_name": "app-windows.zip", "size": 3}]`,
			expectUpload: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			helpers.ResetReported()
			t.Cleanup(helpers.ResetReported)

			s := newReleaseAssetsTestServers(t)
			s.sourcePackageFiles = tt.sourceFiles
			s.sourceFileMissing = tt.sourceFileMissing
			s.serveSourceFiles()
			s.setDestinationPackage("app", "1.0.0", 20, `[
				{"id": 200, "file_name": "app-linux.tar.gz", "size": 15, "file_sha256": "0000"},
				{"id": 201, "file_name": "app-linux.tar.gz", "size": 15, "file_sha256": "1111"}
			]`)

			release := &gitlab.Release{
				TagName: "v1.0.0",
				Assets: gitlab.ReleaseAssets{Links: []*gitlab.ReleaseLink{{
					ID:   1,
					Name: "app-linux",
					URL:  fmt.Sprintf("%sapi/v4/projects/%d/packages/generic/app/1.0.0/app-linux.tar.gz", s.sourceRoot(), TEST_PROJECT.ID),
				}}},
			}
			existingLinks := []*gitlab.ReleaseLink{{ID: 9, Name: "app-linux", URL: s.destinationPackageURL()}}

			s.destination.MirrorReleaseAssets(s.source, TEST_PROJECT_2, release, existingLinks, nil)

			if got := helpers.ExitCode(); got != 0 {
				t.Fatalf("unexpected error when mirroring the asset: exit code %d", got)
			}
			deletedLinks, deletedFiles := s.deletions()
			if !slices.Equal(deletedFiles, tt.expectDeleted) {
				t.Errorf("deleted files = %v, want %v", deletedFiles, tt.expectDeleted)
			}
			if len(deletedLinks) != 0 {
				t.Errorf("the link is still on the source release and must be kept, got deleted links %v", deletedLinks)
			}
			if _, uploaded := s.uploads["app/1.0.0/app-linux.tar.gz"]; uploaded != tt.expectUpload {
				t.Errorf("uploaded = %v, want %v", uploaded, tt.expectUpload)
			}
		})
	}
}

func TestMirrorGenericPackageAssetUnreadableSourceDeletesNothing(t *testing.T) {
	helpers.ResetReported()
	t.Cleanup(helpers.ResetReported)

	// The source package listing is not served: the source answers 404 for it, as it
	// does for a project the token can no longer read.
	s := newReleaseAssetsTestServers(t)
	s.setDestinationPackage("app", "1.0.0", 20, `[{"id": 200, "file_name": "app-linux.tar.gz", "size": 15, "file_sha256": "0000"}]`)

	release := &gitlab.Release{
		TagName: "v1.0.0",
		Assets: gitlab.ReleaseAssets{Links: []*gitlab.ReleaseLink{{
			ID:   1,
			Name: "app-linux",
			URL:  fmt.Sprintf("%sapi/v4/projects/%d/packages/generic/app/1.0.0/app-linux.tar.gz", s.sourceRoot(), TEST_PROJECT.ID),
		}}},
	}

	s.destination.MirrorReleaseAssets(s.source, TEST_PROJECT_2, release, []*gitlab.ReleaseLink{{ID: 9, Name: "app-linux", URL: s.destinationPackageURL()}}, nil)

	if got := helpers.ExitCode(); got == 0 {
		t.Error("expected the unreadable source to be reported")
	}
	if deletedLinks, deletedFiles := s.deletions(); len(deletedLinks) != 0 || len(deletedFiles) != 0 {
		t.Errorf("nothing may be deleted when the source cannot be read, got links %v and files %v", deletedLinks, deletedFiles)
	}
}

func TestPackageFilesMatch(t *testing.T) {
	tests := []struct {
		name        string
		source      gitlab.PackageFile
		destination gitlab.PackageFile
		want        bool
	}{
		{name: "same sha256", source: gitlab.PackageFile{Size: 1, FileSHA256: "abc"}, destination: gitlab.PackageFile{Size: 1, FileSHA256: "ABC"}, want: true},
		{name: "different sha256", source: gitlab.PackageFile{Size: 1, FileSHA256: "abc", FileSHA1: "x"}, destination: gitlab.PackageFile{Size: 1, FileSHA256: "abd", FileSHA1: "x"}, want: false},
		{name: "different size", source: gitlab.PackageFile{Size: 1, FileSHA256: "abc"}, destination: gitlab.PackageFile{Size: 2, FileSHA256: "abc"}, want: false},
		{name: "falls back to sha1", source: gitlab.PackageFile{Size: 1, FileSHA1: "x"}, destination: gitlab.PackageFile{Size: 1, FileSHA256: "abc", FileSHA1: "x"}, want: true},
		{name: "falls back to md5", source: gitlab.PackageFile{Size: 1, FileMD5: "m"}, destination: gitlab.PackageFile{Size: 1, FileMD5: "m"}, want: true},
		{name: "no common checksum", source: gitlab.PackageFile{Size: 1, FileSHA256: "abc"}, destination: gitlab.PackageFile{Size: 1, FileSHA1: "x"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := packageFilesMatch(&tt.source, &tt.destination); got != tt.want {
				t.Errorf("packageFilesMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMirrorReleasesWithoutAssetsIgnoresLinks(t *testing.T) {
	helpers.ResetReported()
	t.Cleanup(helpers.ResetReported)

	s := newReleaseAssetsTestServers(t)
	s.serveReleases(s.sourceMux, TEST_PROJECT, s.sourceReleaseWithAssets())
	// Only on the destination: without mirror_release_assets, it must be left alone.
	s.serveReleases(s.destinationMux, TEST_PROJECT_2, `[{"tag_name": "v0.9.0", "name": "v0.9.0"}]`)

	s.destination.MirrorReleases(s.source, TEST_PROJECT, TEST_PROJECT_2, ReleasesMirroringOptions{})

	if got := helpers.ExitCode(); got != 0 {
		t.Fatalf("unexpected error when mirroring releases: exit code %d", got)
	}
	if len(s.createdReleases) != 1 {
		t.Errorf("expected the release to be created, got %v", s.createdReleases)
	}
	if len(s.uploads) != 0 || len(s.linksByName()) != 0 {
		t.Errorf("expected no asset to be mirrored, got uploads %v and links %v", s.uploads, s.linksByName())
	}
	if deleted := s.deletedReleasesCopy(); len(deleted) != 0 {
		t.Errorf("expected no release to be deleted without mirror_release_assets, got %v", deleted)
	}
}

func TestMirrorReleasesWithAssetsDeletesStaleReleases(t *testing.T) {
	helpers.ResetReported()
	t.Cleanup(helpers.ResetReported)

	s := newReleaseAssetsTestServers(t)
	s.serveReleases(s.sourceMux, TEST_PROJECT, s.sourceReleaseWithAssets())
	s.serveSourceFiles()

	destinationAPI := s.destination.Gitlab.BaseURL().String()
	s.serveReleases(s.destinationMux, TEST_PROJECT_2, fmt.Sprintf(`[
		{"tag_name": "v0.9.0", "name": "v0.9.0", "assets": {"links": [
			{"id": 20, "name": "old", "url": "%[1]sprojects/%[2]d/packages/generic/release-assets/v0.9.0/old.bin"},
			{"id": 21, "name": "app-linux", "url": "%[1]sprojects/%[2]d/packages/generic/app/1.0.0/app-linux.tar.gz"},
			{"id": 22, "name": "docs", "url": "https://docs.example.com/app"}
		]}},
		{"tag_name": "v0.8.0", "name": "v0.8.0"}
	]`, destinationAPI, TEST_PROJECT_2.ID))
	s.setDestinationPackage("release-assets", "v0.9.0", 30, `[{"id": 300, "file_name": "old.bin", "size": 1}]`)
	s.setDestinationPackage("app", "1.0.0", 20, fmt.Sprintf(`[{"id": 200, "file_name": "app-linux.tar.gz", "size": 15, "file_sha256": %q}]`, sourcePackageFileSHA256))

	s.destination.MirrorReleases(s.source, TEST_PROJECT, TEST_PROJECT_2, ReleasesMirroringOptions{MirrorAssets: true})

	if got := helpers.ExitCode(); got != 0 {
		t.Fatalf("unexpected error when mirroring releases with assets: exit code %d", got)
	}

	if deleted, want := s.deletedReleasesCopy(), []string{"v0.8.0", "v0.9.0"}; !slices.Equal(deleted, want) {
		t.Errorf("deleted releases = %v, want %v", deleted, want)
	}

	// old.bin only belonged to v0.9.0; app-linux.tar.gz is still what the source
	// v1.0.0 app-linux link is copied to, so it must stay.
	_, deletedFiles := s.deletions()
	if want := []string{"30/300"}; !slices.Equal(deletedFiles, want) {
		t.Errorf("deleted files = %v, want %v", deletedFiles, want)
	}

	if len(s.createdReleases) != 1 || s.createdReleases[0] != "v1.0.0" {
		t.Errorf("expected the source release v1.0.0 to be created, got %v", s.createdReleases)
	}
}

func TestMirrorReleaseAssetSignInRedirect(t *testing.T) {
	helpers.ResetReported()
	t.Cleanup(helpers.ResetReported)

	s := newReleaseAssetsTestServers(t)
	s.sourceMux.HandleFunc("GET /group/project/-/jobs/42/artifacts/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/users/sign_in", http.StatusFound)
	})
	s.sourceMux.HandleFunc("GET /users/sign_in", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(HEADER_CONTENT_TYPE, "text/html")
		fmt.Fprint(w, "<html>sign in</html>")
	})

	release := &gitlab.Release{
		TagName: "v1.0.0",
		Assets: gitlab.ReleaseAssets{Links: []*gitlab.ReleaseLink{
			{ID: 1, Name: "artifacts", URL: s.sourceRoot() + "group/project/-/jobs/42/artifacts/download"},
		}},
	}

	s.destination.MirrorReleaseAssets(s.source, TEST_PROJECT_2, release, nil, nil)

	if got := helpers.ExitCode(); got == 0 {
		t.Error("expected the sign in redirect to be reported as an error")
	}
	if len(s.uploads) != 0 || len(s.linksByName()) != 0 {
		t.Errorf("expected nothing to be mirrored, got uploads %v and links %v", s.uploads, s.linksByName())
	}
}

func TestDryRunReleasesWithAssets(t *testing.T) {
	s := newReleaseAssetsTestServers(t)
	s.serveReleases(s.sourceMux, TEST_PROJECT, s.sourceReleaseWithAssets())
	s.serveReleases(s.destinationMux, TEST_PROJECT_2, `[
		{"tag_name": "v1.0.0", "assets": {"links": [{"id": 10, "name": "old", "url": "https://example.com/old"}]}},
		{"tag_name": "v0.9.0"}
	]`)
	s.destination.AddProject(TEST_PROJECT_2)

	err := s.destination.DryRunReleases(s.source, TEST_PROJECT, &utils.MirroringOptions{
		DestinationPath:     TEST_PROJECT_2.PathWithNamespace,
		MirrorReleases:      new(true),
		MirrorReleaseAssets: new(true),
	})
	if err != nil {
		t.Errorf("unexpected error when dry running releases with assets: %v", err)
	}
	deletedLinks, deletedFiles := s.deletions()
	if len(s.uploads) != 0 || len(s.linksByName()) != 0 || len(s.createdReleases) != 0 || len(deletedLinks) != 0 || len(deletedFiles) != 0 || len(s.deletedReleasesCopy()) != 0 {
		t.Error("dry run must not write anything to the destination")
	}
}
