package mirroring

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

// The git mirroring deletes the destination tags the source does not have, and
// GitLab deletes the release of a deleted tag with it: the release sync must still
// clean up the files of such a release, known from the snapshot taken before git ran.
func TestMirrorReleasesWithAssetsCleansUpReleasesRemovedByGit(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
	helpers.ResetReported()
	t.Cleanup(helpers.ResetReported)

	s := newReleaseAssetsTestServers(t)
	serveReleases(s.sourceMux, testProject, s.sourceReleaseWithAssets())
	s.serveSourceFiles()
	serveReleases(s.destinationMux, testProject2, "[]")
	s.setDestinationPackage("extra", "1.0", 30, `[{"id": 300, "file_name": "extra.bin", "size": 1}]`)

	releasesBeforeGit := []*gitlab.Release{{
		TagName: "v0.9.0",
		Assets: gitlab.ReleaseAssets{Links: []*gitlab.ReleaseLink{{
			ID:   20,
			Name: "extra.bin",
			URL:  fmt.Sprintf("%sprojects/%d/packages/generic/extra/1.0/extra.bin", s.destination.Gitlab.BaseURL().String(), testProject2.ID),
		}}},
	}}

	s.destination.MirrorReleases(s.source, testProject, testProject2, ReleasesMirroringOptions{MirrorAssets: true, DestinationReleasesBeforeGit: releasesBeforeGit})

	if got := helpers.ExitCode(); got != 0 {
		t.Fatalf("a release already deleted with its tag must not be reported: exit code %d", got)
	}
	if deletedFiles := s.deletedFileList(); !slices.Equal(deletedFiles, []string{"30/300"}) {
		t.Errorf("deleted files = %v, want the file of the release removed by git", deletedFiles)
	}
	// GitLab answers 403 when deleting a release that no longer exists.
	if deleted := s.deletedReleasesCopy(); len(deleted) != 0 {
		t.Errorf("a release already deleted with its tag must not be deleted again, got %v", deleted)
	}
}

func TestVanishedReleases(t *testing.T) {
	t.Parallel()

	release := func(tag string) *gitlab.Release { return &gitlab.Release{TagName: tag} }

	source := releasesByTag([]*gitlab.Release{release("v1")})
	existing := releasesByTag([]*gitlab.Release{release("v1"), release("v2")})
	beforeGit := []*gitlab.Release{release("v1"), release("v2"), release("v3"), nil}

	vanished := vanishedReleases(source, existing, beforeGit)
	if len(vanished) != 1 || vanished[0].TagName != "v3" {
		t.Errorf("vanishedReleases() = %v, want only v3 (v1 is on the source, v2 still exists)", vanished)
	}
}

func TestPackageFilesMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source      gitlab.PackageFile
		destination gitlab.PackageFile
		name        string
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
			t.Parallel()

			if got := packageFilesMatch(&tt.source, &tt.destination); got != tt.want {
				t.Errorf("packageFilesMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMirrorReleasesWithoutAssetsIgnoresLinks(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
	helpers.ResetReported()
	t.Cleanup(helpers.ResetReported)

	s := newReleaseAssetsTestServers(t)
	serveReleases(s.sourceMux, testProject, s.sourceReleaseWithAssets())
	// Only on the destination: without mirror_release_assets, it must be left alone.
	serveReleases(s.destinationMux, testProject2, `[{"tag_name": "v0.9.0", "name": "v0.9.0"}]`)

	s.destination.MirrorReleases(s.source, testProject, testProject2, ReleasesMirroringOptions{})

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

func TestMirrorReleasesWithAssetsDeletesStaleReleases(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
	helpers.ResetReported()
	t.Cleanup(helpers.ResetReported)

	s := newReleaseAssetsTestServers(t)
	serveReleases(s.sourceMux, testProject, s.sourceReleaseWithAssets())
	s.serveSourceFiles()

	destinationAPI := s.destination.Gitlab.BaseURL().String()
	serveReleases(s.destinationMux, testProject2, fmt.Sprintf(`[
		{"tag_name": "v0.9.0", "name": "v0.9.0", "assets": {"links": [
			{"id": 20, "name": "old", "url": "%[1]sprojects/%[2]d/packages/generic/release-assets/v0.9.0/old.bin"},
			{"id": 21, "name": "app-linux", "url": "%[1]sprojects/%[2]d/packages/generic/app/1.0.0/app-linux.tar.gz"},
			{"id": 22, "name": "docs", "url": "https://docs.example.com/app"}
		]}},
		{"tag_name": "v0.8.0", "name": "v0.8.0"}
	]`, destinationAPI, testProject2.ID))
	s.setDestinationPackage("release-assets", "v0.9.0", 30, `[{"id": 300, "file_name": "old.bin", "size": 1}]`)
	s.setDestinationPackage("app", "1.0.0", 20, fmt.Sprintf(`[{"id": 200, "file_name": "app-linux.tar.gz", "size": 15, "file_sha256": %q}]`, sourcePackageFileSHA256))

	s.destination.MirrorReleases(s.source, testProject, testProject2, ReleasesMirroringOptions{MirrorAssets: true})

	if got := helpers.ExitCode(); got != 0 {
		t.Fatalf("unexpected error when mirroring releases with assets: exit code %d", got)
	}

	if deleted, want := s.deletedReleasesCopy(), []string{"v0.8.0", "v0.9.0"}; !slices.Equal(deleted, want) {
		t.Errorf("deleted releases = %v, want %v", deleted, want)
	}

	// old.bin only belonged to v0.9.0; app-linux.tar.gz is still what the source
	// v1.0.0 app-linux link is copied to, so it must stay.
	deletedFiles := s.deletedFileList()
	if want := []string{"30/300"}; !slices.Equal(deletedFiles, want) {
		t.Errorf("deleted files = %v, want %v", deletedFiles, want)
	}

	if len(s.createdReleases) != 1 || s.createdReleases[0] != "v1.0.0" {
		t.Errorf("expected the source release v1.0.0 to be created, got %v", s.createdReleases)
	}
}

func TestMirrorReleaseAssetSignInRedirect(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
	helpers.ResetReported()
	t.Cleanup(helpers.ResetReported)

	s := newReleaseAssetsTestServers(t)
	s.sourceMux.HandleFunc("GET /group/project/-/jobs/42/artifacts/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/users/sign_in", http.StatusFound)
	})
	s.sourceMux.HandleFunc("GET /users/sign_in", func(w http.ResponseWriter, r *http.Request) {
		if !requireGet(w, r) {
			return
		}

		w.Header().Set(headerContentType, "text/html")
		writeBody(w, "<html>sign in</html>")
	})

	release := &gitlab.Release{
		TagName: "v1.0.0",
		Assets: gitlab.ReleaseAssets{Links: []*gitlab.ReleaseLink{
			{ID: 1, Name: "artifacts", URL: s.sourceRoot() + "group/project/-/jobs/42/artifacts/download"},
		}},
	}

	s.destination.MirrorReleaseAssets(s.source, testProject2, release, nil, nil)

	if got := helpers.ExitCode(); got == 0 {
		t.Error("expected the sign in redirect to be reported as an error")
	}
	if len(s.uploads) != 0 || len(s.linksByName()) != 0 {
		t.Errorf("expected nothing to be mirrored, got uploads %v and links %v", s.uploads, s.linksByName())
	}
}

func TestDryRunReleasesWithAssets(t *testing.T) {
	t.Parallel()

	s := newReleaseAssetsTestServers(t)
	serveReleases(s.sourceMux, testProject, s.sourceReleaseWithAssets())
	serveReleases(s.destinationMux, testProject2, `[
		{"tag_name": "v1.0.0", "assets": {"links": [{"id": 10, "name": "old", "url": "https://example.com/old"}]}},
		{"tag_name": "v0.9.0"}
	]`)
	s.destination.AddProject(testProject2)

	err := s.destination.DryRunReleases(s.source, testProject, &mirrorcfg.MirroringOptions{
		DestinationPath:     testProject2.PathWithNamespace,
		MirrorReleases:      new(true),
		MirrorReleaseAssets: new(true),
	})
	if err != nil {
		t.Errorf("unexpected error when dry running releases with assets: %v", err)
	}
	deletedLinks, deletedFiles := s.deletedLinkList(), s.deletedFileList()
	if len(s.uploads) != 0 || len(s.linksByName()) != 0 || len(s.createdReleases) != 0 || len(deletedLinks) != 0 || len(deletedFiles) != 0 || len(s.deletedReleasesCopy()) != 0 {
		t.Error("dry run must not write anything to the destination")
	}
}
