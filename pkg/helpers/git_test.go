package helpers

import (
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

const (
	fileScheme    = "file://"
	githubHTTPURL = "https://github.com/BoxBoxJason/gitlab-sync.git"
)

func TestMirrorRepo(t *testing.T) {
	t.Parallel()

	t.Run("mirror via HTTPS public repo", func(t *testing.T) {
		t.Parallel()
		destDir := t.TempDir()

		// Initialize a bare git repository at the destination
		_, err := git.PlainInit(destDir, true) // true indicates a bare repository
		if err != nil {
			t.Fatalf("failed to initialize bare repository at destination: %v", err)
		}

		if err = MirrorRepo(nil, githubHTTPURL, fileScheme+destDir, nil, nil, MirrorPushOptions{}, nil); err != nil {
			t.Fatalf("MirrorRepo(HTTPS) failed: %v", err)
		}

		destRepo, err := git.PlainOpen(destDir)
		if err != nil {
			t.Fatal(err)
		}

		// MirrorRepo intentionally leaves the destination HEAD alone (it cannot be
		// set over the git transport); only the refs themselves must have landed.
		branches, err := destRepo.Branches()
		if err != nil {
			t.Fatalf("failed to list branches: %v", err)
		}

		found := false

		err = branches.ForEach(func(r *plumbing.Reference) error {
			if r.Hash().IsZero() {
				t.Errorf("branch %s has a zero hash", r.Name())
			}

			found = true

			return storer.ErrStop
		})
		if err != nil {
			t.Fatalf("failed to iterate branches: %v", err)
		}

		if !found {
			t.Error("no branches found in mirrored repo")
		}

		tags, err := destRepo.Tags()
		if err != nil {
			t.Fatalf("failed to list tags: %v", err)
		}

		foundTag := false

		err = tags.ForEach(func(r *plumbing.Reference) error {
			foundTag = r.Name().IsTag()

			return storer.ErrStop
		})
		if err != nil {
			t.Fatalf("failed to iterate tags: %v", err)
		}

		if !foundTag {
			t.Error("no tags found in mirrored repo")
		}
	})

	t.Run("mirroring an already up to date destination is not an error", func(t *testing.T) {
		t.Parallel()

		srcDir := t.TempDir()

		destDir := t.TempDir()

		seedBareRepo(t, srcDir)

		if _, err := git.PlainInit(destDir, true); err != nil {
			t.Fatalf("failed to initialize bare repository at destination: %v", err)
		}

		if err := MirrorRepo(nil, fileScheme+srcDir, fileScheme+destDir, nil, nil, MirrorPushOptions{}, nil); err != nil {
			t.Fatalf("first MirrorRepo failed: %v", err)
		}

		// A second run has nothing to push; go-git signals that with
		// git.NoErrAlreadyUpToDate, which must not surface as a failure.
		if err := MirrorRepo(nil, fileScheme+srcDir, fileScheme+destDir, nil, nil, MirrorPushOptions{}, nil); err != nil {
			t.Fatalf("second MirrorRepo failed: %v", err)
		}
	})

	t.Run("refs dropped by the source are deleted at the destination", func(t *testing.T) {
		t.Parallel()

		srcDir := filepath.Join(t.TempDir(), "srcrepo.git")
		seedBareRepo(t, srcDir)

		srcRepo, err := git.PlainOpen(srcDir)
		if err != nil {
			t.Fatal(err)
		}

		head, err := srcRepo.Head()
		if err != nil {
			t.Fatal(err)
		}

		setRef(t, srcRepo, "refs/heads/feature", head.Hash())
		setRef(t, srcRepo, "refs/tags/v1.0.0", head.Hash())

		destDir := filepath.Join(t.TempDir(), "destrepo.git")
		if _, err = git.PlainInit(destDir, true); err != nil {
			t.Fatal(err)
		}

		if err = MirrorRepo(nil, fileScheme+srcDir, fileScheme+destDir, nil, nil, MirrorPushOptions{}, nil); err != nil {
			t.Fatalf("first MirrorRepo failed: %v", err)
		}

		assertRef(t, destDir, "refs/heads/feature", true)
		assertRef(t, destDir, "refs/tags/v1.0.0", true)

		removeRef(t, srcRepo, "refs/heads/feature")
		removeRef(t, srcRepo, "refs/tags/v1.0.0")

		if err = MirrorRepo(nil, fileScheme+srcDir, fileScheme+destDir, nil, nil, MirrorPushOptions{}, nil); err != nil {
			t.Fatalf("second MirrorRepo failed: %v", err)
		}

		assertRef(t, destDir, "refs/heads/feature", false)
		assertRef(t, destDir, "refs/tags/v1.0.0", false)
	})

	// GitLab refuses any update to the namespaces it reserves ("deny updating a
	// hidden ref"), and a mirror clone of a GitLab project holds some of them.
	t.Run("GitLab reserved refs are not pushed", func(t *testing.T) {
		t.Parallel()

		srcDir := filepath.Join(t.TempDir(), "srcrepo.git")
		seedBareRepo(t, srcDir)

		srcRepo, err := git.PlainOpen(srcDir)
		if err != nil {
			t.Fatal(err)
		}

		head, err := srcRepo.Head()
		if err != nil {
			t.Fatal(err)
		}

		// refs/stash is refused by every receive-pack ("funny refname").
		hidden := []string{"refs/stash", "refs/merge-requests/1/head", "refs/pipelines/42", "refs/environments/prod/deployments/1", "refs/keep-around/" + head.Hash().String(), "refs/tmp/x", "refs/remotes/origin/main"}
		for _, name := range hidden {
			setRef(t, srcRepo, name, head.Hash())
		}
		setRef(t, srcRepo, "refs/tags/v1.0.0", head.Hash())
		setRef(t, srcRepo, "refs/notes/commits", head.Hash())

		destDir := filepath.Join(t.TempDir(), "destrepo.git")
		if _, err = git.PlainInit(destDir, true); err != nil {
			t.Fatal(err)
		}

		if err = MirrorRepo(nil, fileScheme+srcDir, fileScheme+destDir, nil, nil, MirrorPushOptions{}, nil); err != nil {
			t.Fatalf("MirrorRepo failed: %v", err)
		}

		for _, name := range hidden {
			assertRef(t, destDir, name, false)
		}
		assertRef(t, destDir, head.Name().String(), true)
		assertRef(t, destDir, "refs/tags/v1.0.0", true)
		assertRef(t, destDir, "refs/notes/commits", true)
	})

	// A server refuses to delete the branch its HEAD points at, so the mirroring
	// must leave that one in place instead of failing the whole push.
	t.Run("the destination default branch is never deleted", func(t *testing.T) {
		t.Parallel()

		srcDir := filepath.Join(t.TempDir(), "srcrepo.git")
		seedBareRepo(t, srcDir)

		srcRepo, err := git.PlainOpen(srcDir)
		if err != nil {
			t.Fatal(err)
		}

		head, err := srcRepo.Head()
		if err != nil {
			t.Fatal(err)
		}

		destDir := filepath.Join(t.TempDir(), "destrepo.git")
		if _, err = git.PlainInit(destDir, true); err != nil {
			t.Fatal(err)
		}

		if err = MirrorRepo(nil, fileScheme+srcDir, fileScheme+destDir, nil, nil, MirrorPushOptions{}, nil); err != nil {
			t.Fatalf("first MirrorRepo failed: %v", err)
		}

		// The source renames its only branch: the destination now holds a branch
		// the source lost, and that branch is the one its HEAD points at.
		setRef(t, srcRepo, "refs/heads/renamed", head.Hash())

		if err = srcRepo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/renamed")); err != nil {
			t.Fatal(err)
		}

		removeRef(t, srcRepo, head.Name().String())

		if err = MirrorRepo(nil, fileScheme+srcDir, fileScheme+destDir, nil, nil, MirrorPushOptions{}, nil); err != nil {
			t.Fatalf("second MirrorRepo failed: %v", err)
		}

		assertRef(t, destDir, "refs/heads/renamed", true)
		assertRef(t, destDir, head.Name().String(), true)
	})

	t.Run("error on invalid source", func(t *testing.T) {
		t.Parallel()
		destDir := t.TempDir()

		// Initialize a bare git repository at the destination
		_, err := git.PlainInit(destDir, true)
		if err != nil {
			t.Fatalf("failed to initialize bare repository at destination: %v", err)
		}

		err = MirrorRepo(nil, "file:///no/such/path", fileScheme+destDir, nil, nil, MirrorPushOptions{}, nil)
		if err == nil {
			t.Error("expected error for invalid source URL, got nil")
		}
	})
}
