package helpers

import (
	"os"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
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
		headRef, err := destRepo.Head()
		if err != nil {
			t.Fatalf("dest HEAD error: %v", err)
		}
		if headRef.Hash().IsZero() {
			t.Error("dest HEAD hash is zero")
		}
		branches, _ := destRepo.Branches()
		found := false
		_ = branches.ForEach(func(r *plumbing.Reference) error {
			found = true
			return storer.ErrStop
		})
		if !found {
			t.Error("no branches found in mirrored repo")
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
