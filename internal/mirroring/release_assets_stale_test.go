package mirroring

import (
	"fmt"
	"net/url"
	"slices"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"

	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

func TestMirrorReleaseAssetsDeletesStaleLinks(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
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
			URL:  fmt.Sprintf("%sapi/v4/projects/%d/packages/generic/app/1.0.0/app-linux.tar.gz", s.sourceRoot(), testProject.ID),
		}}},
	}
	existingLinks := []*gitlab.ReleaseLink{
		{ID: 9, Name: "app-linux", URL: s.destinationPackageURL()},
		// Removed from the source: its copy in the destination project goes with it.
		{ID: 10, Name: "old", URL: fmt.Sprintf("%sprojects/%d/packages/generic/release-assets/v1.0.0/old.bin", destinationAPI, testProject2.ID)},
		// Removed from the source, but its file is still what app-linux is copied to.
		{ID: 11, Name: "renamed", URL: fmt.Sprintf("%sprojects/%s/packages/generic/app/1.0.0/app-linux.tar.gz", destinationAPI, url.PathEscape(testProject2.PathWithNamespace))},
		// Removed from the source, pointing to another project: only the link goes.
		{ID: 12, Name: "other-project", URL: destinationAPI + "projects/99/packages/generic/release-assets/v1.0.0/kept.bin"},
		// Removed from the source, external: only the link goes.
		{ID: 13, Name: "external", URL: "https://example.com/app"},
	}

	s.destination.MirrorReleaseAssets(s.source, testProject2, release, existingLinks, nil)

	if got := helpers.ExitCode(); got != 0 {
		t.Fatalf("unexpected error when mirroring the assets: exit code %d", got)
	}

	deletedLinks, deletedFiles := s.deletedLinkList(), s.deletedFileList()
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

func TestMirrorReleaseAssetsSharedFileIsKept(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
	helpers.ResetReported()
	t.Cleanup(helpers.ResetReported)

	s := newReleaseAssetsTestServers(t)
	s.setDestinationPackage("release-assets", "v1.0.0", 21, `[{"id": 210, "file_name": "shared.bin", "size": 1}]`)

	sharedFile := fmt.Sprintf("%sprojects/%d/packages/generic/release-assets/v1.0.0/shared.bin", s.destination.Gitlab.BaseURL().String(), testProject2.ID)
	// Another release of the project still has a link copied to shared.bin.
	referenced := map[genericPackageFile]struct{}{{Name: "release-assets", Version: "v1.0.0", FileName: "shared.bin"}: {}}

	s.destination.MirrorReleaseAssets(s.source, testProject2, &gitlab.Release{TagName: "v2.0.0"}, []*gitlab.ReleaseLink{{ID: 10, Name: "shared", URL: sharedFile}}, referenced)

	deletedLinks, deletedFiles := s.deletedLinkList(), s.deletedFileList()
	if len(deletedLinks) != 1 || len(deletedFiles) != 0 {
		t.Errorf("expected only the link to be deleted, got links %v and files %v", deletedLinks, deletedFiles)
	}
}

func TestMirrorGenericPackageAssetSourceFileDeleted(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
	tests := []struct {
		name              string
		sourceFiles       string
		expectDeleted     []string
		sourceFileMissing bool
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

	for _, tt := range tests { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
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
					URL:  fmt.Sprintf("%sapi/v4/projects/%d/packages/generic/app/1.0.0/app-linux.tar.gz", s.sourceRoot(), testProject.ID),
				}}},
			}
			existingLinks := []*gitlab.ReleaseLink{{ID: 9, Name: "app-linux", URL: s.destinationPackageURL()}}

			s.destination.MirrorReleaseAssets(s.source, testProject2, release, existingLinks, nil)

			if got := helpers.ExitCode(); got != 0 {
				t.Fatalf("unexpected error when mirroring the asset: exit code %d", got)
			}
			deletedLinks, deletedFiles := s.deletedLinkList(), s.deletedFileList()
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

func TestMirrorGenericPackageAssetUnreadableSourceDeletesNothing(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
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
			URL:  fmt.Sprintf("%sapi/v4/projects/%d/packages/generic/app/1.0.0/app-linux.tar.gz", s.sourceRoot(), testProject.ID),
		}}},
	}

	s.destination.MirrorReleaseAssets(s.source, testProject2, release, []*gitlab.ReleaseLink{{ID: 9, Name: "app-linux", URL: s.destinationPackageURL()}}, nil)

	if got := helpers.ExitCode(); got == 0 {
		t.Error("expected the unreadable source to be reported")
	}
	if deletedLinks, deletedFiles := s.deletedLinkList(), s.deletedFileList(); len(deletedLinks) != 0 || len(deletedFiles) != 0 {
		t.Errorf("nothing may be deleted when the source cannot be read, got links %v and files %v", deletedLinks, deletedFiles)
	}
}
