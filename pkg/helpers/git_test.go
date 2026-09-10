package helpers

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

const (
	FILE_SCHEME   = "file://"
	githubHTTPURL = "https://github.com/BoxBoxJason/gitlab-sync.git"
)

func TestBuildHTTPAuth(t *testing.T) {
	tests := []struct {
		name             string
		username, token  string
		wantUser, wantPW string
	}{
		{"both provided", "alice", "secr3t", "alice", "secr3t"},
		{"empty username", "", "tk", DEFAULT_GIT_USER, "tk"},
		{"spaces username", "   ", "x", DEFAULT_GIT_USER, "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := BuildHTTPAuth(tt.username, tt.token)
			basic, ok := auth.(*http.BasicAuth)
			if !ok {
				t.Fatalf("BuildHTTPAuth returned non-BasicAuth: %T", auth)
			}
			if basic.Username != tt.wantUser {
				t.Errorf("Username = %q; want %q", basic.Username, tt.wantUser)
			}
			if basic.Password != tt.wantPW {
				t.Errorf("Password = %q; want %q", basic.Password, tt.wantPW)
			}
		})
	}
}

func TestMirrorRepo(t *testing.T) {
	t.Run("mirror via HTTPS public repo", func(t *testing.T) {
		t.Parallel()
		destDir, err := os.MkdirTemp("/tmp", "destrepo-*.git")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(destDir)

		// Initialize a bare git repository at the destination
		_, err = git.PlainInit(destDir, true) // true indicates a bare repository
		if err != nil {
			t.Fatalf("failed to initialize bare repository at destination: %v", err)
		}

		if err := MirrorRepo(githubHTTPURL, FILE_SCHEME+destDir, nil, nil); err != nil {
			t.Fatalf("MirrorRepo(HTTPS) failed: %v", err)
		}

		destRepo, err := git.PlainOpen(destDir)
		if err != nil {
			t.Fatal(err)
		}

		// MirrorRepo intentionally leaves the destination HEAD alone (it cannot be
		// set over the git transport); only the refs themselves must have landed.
		branches, _ := destRepo.Branches()
		found := false
		_ = branches.ForEach(func(r *plumbing.Reference) error {
			if r.Hash().IsZero() {
				t.Errorf("branch %s has a zero hash", r.Name())
			}
			found = true

			return storer.ErrStop
		})
		if !found {
			t.Error("no branches found in mirrored repo")
		}

		tags, _ := destRepo.Tags()
		foundTag := false
		_ = tags.ForEach(func(r *plumbing.Reference) error {
			foundTag = true

			return storer.ErrStop
		})
		if !foundTag {
			t.Error("no tags found in mirrored repo")
		}
	})

	t.Run("mirroring an already up to date destination is not an error", func(t *testing.T) {
		t.Parallel()

		srcDir, err := os.MkdirTemp("", "srcrepo-*.git")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(srcDir)

		destDir, err := os.MkdirTemp("", "destrepo-uptodate-*.git")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(destDir)

		seedBareRepo(t, srcDir)

		if _, err := git.PlainInit(destDir, true); err != nil {
			t.Fatalf("failed to initialize bare repository at destination: %v", err)
		}

		if err := MirrorRepo(FILE_SCHEME+srcDir, FILE_SCHEME+destDir, nil, nil); err != nil {
			t.Fatalf("first MirrorRepo failed: %v", err)
		}

		// A second run has nothing to push; go-git signals that with
		// git.NoErrAlreadyUpToDate, which must not surface as a failure.
		if err := MirrorRepo(FILE_SCHEME+srcDir, FILE_SCHEME+destDir, nil, nil); err != nil {
			t.Fatalf("second MirrorRepo failed: %v", err)
		}
	})

	t.Run("error on invalid source", func(t *testing.T) {
		t.Parallel()
		destDir, err := os.MkdirTemp("", "destrepo-bad")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(destDir)

		// Initialize a bare git repository at the destination
		_, err = git.PlainInit(destDir, true)
		if err != nil {
			t.Fatalf("failed to initialize bare repository at destination: %v", err)
		}

		err = MirrorRepo("file:///no/such/path", FILE_SCHEME+destDir, nil, nil)
		if err == nil {
			t.Error("expected error for invalid source URL, got nil")
		}
	})
}

// seedBareRepo creates a bare repository at path holding a single commit on the
// repository's initial branch.
func seedBareRepo(t *testing.T, path string) {
	t.Helper()

	worktreeDir := t.TempDir()

	repo, err := git.PlainInit(worktreeDir, false)
	if err != nil {
		t.Fatalf("failed to init source worktree: %v", err)
	}

	if err := os.WriteFile(filepath.Join(worktreeDir, "file.txt"), []byte("content"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatalf("failed to open source worktree: %v", err)
	}

	if _, err := worktree.Add("file.txt"); err != nil {
		t.Fatalf("failed to stage source file: %v", err)
	}

	_, err = worktree.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{Name: "tester", Email: "tester@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("failed to commit in source repository: %v", err)
	}

	if _, err := git.PlainClone(path, true, &git.CloneOptions{URL: FILE_SCHEME + worktreeDir, Mirror: true}); err != nil {
		t.Fatalf("failed to create bare source repository: %v", err)
	}
}
