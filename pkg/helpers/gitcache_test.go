package helpers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

const reuseMarker = "reused-entry-marker"

func TestSplitRepoURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rawURL   string
		wantHost string
		wantPath string
	}{
		{"https", "https://gitlab.example.com/group/sub/repo.git", "gitlab.example.com", "group/sub/repo.git"},
		{"https with port", "https://gitlab.example.com:8443/group/repo.git", "gitlab.example.com", "group/repo.git"},
		{"ssh scheme", "ssh://git@gitlab.example.com:22/group/repo.git", "gitlab.example.com", "group/repo.git"},
		{"scp like", "git@gitlab.example.com:group/repo.git", "gitlab.example.com", "group/repo.git"},
		{"file", "file:///tmp/repos/repo.git", "", "tmp/repos/repo.git"},
		{"bare path", "/tmp/repos/repo.git", "", "tmp/repos/repo.git"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			host, path := splitRepoURL(tt.rawURL)
			if host != tt.wantHost || path != tt.wantPath {
				t.Errorf("splitRepoURL(%q) = (%q, %q); want (%q, %q)", tt.rawURL, host, path, tt.wantHost, tt.wantPath)
			}
		})
	}
}

func TestGitCacheRepoPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	cache, err := NewGitCache(root, 0)
	if err != nil {
		t.Fatalf("NewGitCache failed: %v", err)
	}

	tests := []struct {
		name      string
		sourceURL string
		want      string
		wantErr   bool
	}{
		{
			name:      "https url keeps its namespace",
			sourceURL: "https://gitlab.example.com/group/sub/repo.git",
			want:      filepath.Join(root, "gitlab.example.com", "group", "sub", "repo.git"),
		},
		{
			name:      "the .git suffix is added when the url omits it",
			sourceURL: "https://gitlab.example.com/group/repo",
			want:      filepath.Join(root, "gitlab.example.com", "group", "repo.git"),
		},
		{
			name:      "a hostless url lands under the local directory",
			sourceURL: "file:///tmp/repo.git",
			want:      filepath.Join(root, unknownHostDir, "tmp", "repo.git"),
		},
		{
			name:      "traversal segments cannot escape the root",
			sourceURL: "https://gitlab.example.com/../../etc/repo.git",
			want:      filepath.Join(root, "gitlab.example.com", "etc", "repo.git"),
		},
		{
			name:      "separators inside a segment are neutralised",
			sourceURL: "https://gitlab.example.com/gr%2Foup/repo.git",
			want:      filepath.Join(root, "gitlab.example.com", "gr_2Foup", "repo.git"),
		},
		{
			name:      "a url without a repository is rejected",
			sourceURL: "https://gitlab.example.com",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := cache.repoPath(tt.sourceURL)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("repoPath(%q) = %q; want an error", tt.sourceURL, got)
				}

				return
			}

			if err != nil {
				t.Fatalf("repoPath(%q) failed: %v", tt.sourceURL, err)
			}

			if got != tt.want {
				t.Errorf("repoPath(%q) = %q; want %q", tt.sourceURL, got, tt.want)
			}
		})
	}
}

func TestGitCacheRepoPathIsDeterministic(t *testing.T) {
	t.Parallel()

	cache, err := NewGitCache(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("NewGitCache failed: %v", err)
	}

	const sourceURL = "https://gitlab.example.com/group/repo.git"

	first, err := cache.repoPath(sourceURL)
	if err != nil {
		t.Fatalf("repoPath failed: %v", err)
	}

	second, err := cache.repoPath(sourceURL)
	if err != nil {
		t.Fatalf("repoPath failed: %v", err)
	}

	if first != second {
		t.Errorf("repoPath is not deterministic: %q != %q", first, second)
	}
}

// TestGitCacheMirrorRepo walks a repository through two runs: the first one
// populates the cache, the second one reuses it and must still leave the
// destination an exact copy of the source.
func TestGitCacheMirrorRepo(t *testing.T) {
	t.Parallel()

	sourceDir := filepath.Join(t.TempDir(), "source.git")
	seedBareRepo(t, sourceDir)

	sourceRepo, err := git.PlainOpen(sourceDir)
	if err != nil {
		t.Fatalf("failed to open the source repository: %v", err)
	}

	head, err := sourceRepo.Head()
	if err != nil {
		t.Fatalf("failed to read the source HEAD: %v", err)
	}

	setRef(t, sourceRepo, "refs/heads/feature", head.Hash())
	setRef(t, sourceRepo, "refs/tags/v1.0.0", head.Hash())

	destinationDir := filepath.Join(t.TempDir(), "destination.git")
	if _, err := git.PlainInit(destinationDir, true); err != nil {
		t.Fatalf("failed to initialize the destination repository: %v", err)
	}

	cacheRoot := t.TempDir()

	cache, err := NewGitCache(cacheRoot, 0)
	if err != nil {
		t.Fatalf("NewGitCache failed: %v", err)
	}

	sourceURL := FILE_SCHEME + sourceDir
	destinationURL := FILE_SCHEME + destinationDir

	if err := MirrorRepo(cache, sourceURL, destinationURL, nil, nil); err != nil {
		t.Fatalf("first MirrorRepo failed: %v", err)
	}

	entryPath, err := cache.repoPath(sourceURL)
	if err != nil {
		t.Fatalf("repoPath failed: %v", err)
	}

	if _, err := git.PlainOpen(entryPath); err != nil {
		t.Fatalf("the cache entry at %q is not a repository: %v", entryPath, err)
	}

	if _, err := os.Stat(filepath.Join(entryPath, lastUsedFileName)); err != nil {
		t.Errorf("the cache entry carries no usage marker: %v", err)
	}

	assertRef(t, destinationDir, "refs/heads/feature", true)
	assertRef(t, destinationDir, "refs/tags/v1.0.0", true)

	// A file dropped inside the entry survives only if the second run reuses it
	// instead of cloning the repository again.
	markerPath := filepath.Join(entryPath, reuseMarker)
	if err := os.WriteFile(markerPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write the reuse marker: %v", err)
	}

	// The source drops a branch and gains another one; both changes must reach
	// the cache entry and the destination.
	removeRef(t, sourceRepo, "refs/heads/feature")
	setRef(t, sourceRepo, "refs/heads/other", head.Hash())

	if err := MirrorRepo(cache, sourceURL, destinationURL, nil, nil); err != nil {
		t.Fatalf("second MirrorRepo failed: %v", err)
	}

	if _, err := os.Stat(markerPath); err != nil {
		t.Errorf("the second run did not reuse the cache entry: %v", err)
	}

	assertRef(t, entryPath, "refs/heads/feature", false)
	assertRef(t, entryPath, "refs/heads/other", true)
	assertRef(t, destinationDir, "refs/heads/feature", false)
	assertRef(t, destinationDir, "refs/heads/other", true)
	assertRef(t, destinationDir, "refs/tags/v1.0.0", true)
}

// TestGitCacheMirrorRepoRebuildsBrokenEntry checks that a cache entry which is
// not a usable repository is thrown away instead of failing the sync.
func TestGitCacheMirrorRepoRebuildsBrokenEntry(t *testing.T) {
	t.Parallel()

	sourceDir := filepath.Join(t.TempDir(), "source.git")
	seedBareRepo(t, sourceDir)

	destinationDir := filepath.Join(t.TempDir(), "destination.git")
	if _, err := git.PlainInit(destinationDir, true); err != nil {
		t.Fatalf("failed to initialize the destination repository: %v", err)
	}

	cache, err := NewGitCache(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("NewGitCache failed: %v", err)
	}

	sourceURL := FILE_SCHEME + sourceDir

	entryPath, err := cache.repoPath(sourceURL)
	if err != nil {
		t.Fatalf("repoPath failed: %v", err)
	}

	// Leave a directory that is not a repository where the entry belongs.
	if err := os.MkdirAll(entryPath, 0o700); err != nil {
		t.Fatalf("failed to create the broken entry: %v", err)
	}

	if err := os.WriteFile(filepath.Join(entryPath, "garbage"), []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write the broken entry: %v", err)
	}

	if err := MirrorRepo(cache, sourceURL, FILE_SCHEME+destinationDir, nil, nil); err != nil {
		t.Fatalf("MirrorRepo failed on a broken cache entry: %v", err)
	}

	if _, err := git.PlainOpen(entryPath); err != nil {
		t.Errorf("the broken cache entry was not rebuilt: %v", err)
	}
}

func TestGitCachePrune(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	freshEntry := filepath.Join(root, "gitlab.example.com", "group", "fresh.git")
	expiredEntry := filepath.Join(root, "gitlab.example.com", "group", "expired.git")

	writeCacheEntry(t, freshEntry, time.Now())
	writeCacheEntry(t, expiredEntry, time.Now().Add(-48*time.Hour))

	cache, err := NewGitCache(root, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewGitCache failed: %v", err)
	}

	cache.Prune()

	if _, err := os.Stat(freshEntry); err != nil {
		t.Errorf("the fresh entry was removed: %v", err)
	}

	if _, err := os.Stat(expiredEntry); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the expired entry was kept (stat error: %v)", err)
	}
}

func TestGitCachePruneDisabled(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	entry := filepath.Join(root, "gitlab.example.com", "group", "ancient.git")
	writeCacheEntry(t, entry, time.Now().Add(-10000*time.Hour))

	cache, err := NewGitCache(root, 0)
	if err != nil {
		t.Fatalf("NewGitCache failed: %v", err)
	}

	cache.Prune()

	if _, err := os.Stat(entry); err != nil {
		t.Errorf("an entry was expired although expiry is disabled: %v", err)
	}
}

// TestGitCachePruneSkipsLockedEntry makes sure housekeeping never pulls an
// entry out from under a run that is using it.
func TestGitCachePruneSkipsLockedEntry(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	entry := filepath.Join(root, "gitlab.example.com", "group", "busy.git")
	writeCacheEntry(t, entry, time.Now().Add(-48*time.Hour))

	cache, err := NewGitCache(root, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewGitCache failed: %v", err)
	}

	release, acquired := cache.tryLock(entry)
	if !acquired {
		t.Fatal("failed to lock a free cache entry")
	}

	cache.Prune()

	if _, err := os.Stat(entry); err != nil {
		t.Errorf("a locked entry was pruned: %v", err)
	}

	release()

	cache.Prune()

	if _, err := os.Stat(entry); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the entry was not pruned once released (stat error: %v)", err)
	}
}

func TestGitCacheTryLockIsExclusive(t *testing.T) {
	t.Parallel()

	cache, err := NewGitCache(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("NewGitCache failed: %v", err)
	}

	entry := filepath.Join(cache.Root(), "gitlab.example.com", "group", "repo.git")

	release, acquired := cache.tryLock(entry)
	if !acquired {
		t.Fatal("failed to lock a free cache entry")
	}

	if _, taken := cache.tryLock(entry); taken {
		t.Error("the same cache entry was locked twice")
	}

	release()

	secondRelease, acquired := cache.tryLock(entry)
	if !acquired {
		t.Fatal("failed to lock a released cache entry")
	}

	secondRelease()

	if _, err := os.Stat(entry + cacheLockSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the lock file outlived the lock (stat error: %v)", err)
	}
}

// TestBreakStaleLock covers the recovery path for a lock left behind by a run
// that died: the lock file is there, but nothing refreshes it any more.
func TestBreakStaleLock(t *testing.T) {
	t.Parallel()

	lockPath := filepath.Join(t.TempDir(), "repo.git.lock")

	if err := os.WriteFile(lockPath, []byte("1234\n"), 0o600); err != nil {
		t.Fatalf("failed to write the lock file: %v", err)
	}

	if breakStaleLock(lockPath) {
		t.Error("a freshly refreshed lock was broken")
	}

	stale := time.Now().Add(-2 * lockStaleAfter)
	if err := os.Chtimes(lockPath, stale, stale); err != nil {
		t.Fatalf("failed to age the lock file: %v", err)
	}

	if !breakStaleLock(lockPath) {
		t.Error("a stale lock was not broken")
	}

	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the stale lock file was not removed (stat error: %v)", err)
	}
}

// writeCacheEntry creates a cache entry whose last sync happened at lastUsed.
func writeCacheEntry(t *testing.T, entryPath string, lastUsed time.Time) {
	t.Helper()

	if err := os.MkdirAll(entryPath, 0o700); err != nil {
		t.Fatalf("failed to create the cache entry %q: %v", entryPath, err)
	}

	markerPath := filepath.Join(entryPath, lastUsedFileName)
	if err := os.WriteFile(markerPath, []byte(lastUsed.UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		t.Fatalf("failed to write the usage marker of %q: %v", entryPath, err)
	}

	if err := os.Chtimes(markerPath, lastUsed, lastUsed); err != nil {
		t.Fatalf("failed to age the usage marker of %q: %v", entryPath, err)
	}
}

func setRef(t *testing.T, repo *git.Repository, name string, hash plumbing.Hash) {
	t.Helper()

	if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(name), hash)); err != nil {
		t.Fatalf("failed to set %s: %v", name, err)
	}
}

func removeRef(t *testing.T, repo *git.Repository, name string) {
	t.Helper()

	if err := repo.Storer.RemoveReference(plumbing.ReferenceName(name)); err != nil {
		t.Fatalf("failed to remove %s: %v", name, err)
	}
}

// assertRef checks whether a repository holds a reference.
func assertRef(t *testing.T, repoPath, name string, want bool) {
	t.Helper()

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		t.Fatalf("failed to open %q: %v", repoPath, err)
	}

	_, err = repo.Reference(plumbing.ReferenceName(name), false)

	switch {
	case err == nil && !want:
		t.Errorf("%s still exists in %q", name, repoPath)
	case errors.Is(err, plumbing.ErrReferenceNotFound) && want:
		t.Errorf("%s is missing from %q", name, repoPath)
	case err != nil && !errors.Is(err, plumbing.ErrReferenceNotFound):
		t.Fatalf("failed to read %s in %q: %v", name, repoPath, err)
	}
}
