package mirroring

import (
	"fmt"
	"strings"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"

	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

func TestMirrorReleasesWithAssets(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
	helpers.ResetReported()
	t.Cleanup(helpers.ResetReported)

	s := newReleaseAssetsTestServers(t)
	serveReleases(s.sourceMux, testProject, s.sourceReleaseWithAssets())
	serveReleases(s.destinationMux, testProject2, "[]")
	s.serveSourceFiles()

	s.destination.MirrorReleases(s.source, testProject, testProject2, ReleasesMirroringOptions{MirrorAssets: true})

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
			"url":               fmt.Sprintf("%sprojects/%d/packages/generic/app/1.0.0/app-linux.tar.gz", destinationAPI, testProject2.ID),
			"direct_asset_path": "/bin/app-linux",
			"link_type":         "package",
		},
		"report": {
			"url":       fmt.Sprintf("%sprojects/%d/packages/generic/release-assets/v1.0.0/report.txt", destinationAPI, testProject2.ID),
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
		if _, hasPath := expected["direct_asset_path"]; !hasPath && link["direct_asset_path"] != nil {
			t.Errorf("link %s: unexpected direct_asset_path %v", name, link["direct_asset_path"])
		}
	}
}

func TestMirrorReleasesWithAssetsExistingRelease(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
	helpers.ResetReported()
	t.Cleanup(helpers.ResetReported)

	s := newReleaseAssetsTestServers(t)
	serveReleases(s.sourceMux, testProject, s.sourceReleaseWithAssets())
	serveReleases(s.destinationMux, testProject2, `[{
		"tag_name": "v1.0.0",
		"name": "v1.0.0",
		"assets": {"links": [
			{"id": 9, "name": "app-linux", "url": "https://example.com/already-there"},
			{"id": 8, "name": "report", "url": "https://example.com/report"}
		]}
	}]`)
	s.serveSourceFiles()

	s.destination.MirrorReleases(s.source, testProject, testProject2, ReleasesMirroringOptions{MirrorAssets: true})

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

func TestMirrorGenericPackageAssetChecksums(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
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

	for _, tt := range tests { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
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
					URL:      fmt.Sprintf("%sapi/v4/projects/%d/packages/generic/app/1.0.0/app-linux.tar.gz", s.sourceRoot(), testProject.ID),
					LinkType: gitlab.PackageLinkType,
				}}},
			}

			s.destination.MirrorReleaseAssets(s.source, testProject2, release, existingLinks, nil)

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
