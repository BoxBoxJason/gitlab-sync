package helpers

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

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
	if _, err = git.PlainInit(destinationDir, true); err != nil {
		t.Fatalf("failed to initialize the destination repository: %v", err)
	}

	cacheRoot := t.TempDir()

	cache, err := NewGitCache(cacheRoot, 0)
	if err != nil {
		t.Fatalf("NewGitCache failed: %v", err)
	}

	sourceURL := fileScheme + sourceDir
	destinationURL := fileScheme + destinationDir

	if err = MirrorRepo(cache, sourceURL, destinationURL, nil, nil, MirrorPushOptions{}, nil); err != nil {
		t.Fatalf("first MirrorRepo failed: %v", err)
	}

	entryPath, err := cache.repoPath(sourceURL)
	if err != nil {
		t.Fatalf("repoPath failed: %v", err)
	}

	if _, err = git.PlainOpen(entryPath); err != nil {
		t.Fatalf("the cache entry at %q is not a repository: %v", entryPath, err)
	}

	if _, err = os.Stat(filepath.Join(entryPath, lastUsedFileName)); err != nil {
		t.Errorf("the cache entry carries no usage marker: %v", err)
	}

	assertRef(t, destinationDir, "refs/heads/feature", true)
	assertRef(t, destinationDir, "refs/tags/v1.0.0", true)

	// A file dropped inside the entry survives only if the second run reuses it
	// instead of cloning the repository again.
	markerPath := filepath.Join(entryPath, reuseMarker)
	if err = os.WriteFile(markerPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write the reuse marker: %v", err)
	}

	// The source drops a branch and gains another one; both changes must reach
	// the cache entry and the destination.
	removeRef(t, sourceRepo, "refs/heads/feature")
	setRef(t, sourceRepo, "refs/heads/other", head.Hash())

	if err = MirrorRepo(cache, sourceURL, destinationURL, nil, nil, MirrorPushOptions{}, nil); err != nil {
		t.Fatalf("second MirrorRepo failed: %v", err)
	}

	if _, err = os.Stat(markerPath); err != nil {
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

	sourceURL := fileScheme + sourceDir

	entryPath, err := cache.repoPath(sourceURL)
	if err != nil {
		t.Fatalf("repoPath failed: %v", err)
	}

	// Leave a directory that is not a repository where the entry belongs.
	if err = os.MkdirAll(entryPath, 0o700); err != nil {
		t.Fatalf("failed to create the broken entry: %v", err)
	}

	if err = os.WriteFile(filepath.Join(entryPath, "garbage"), []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write the broken entry: %v", err)
	}

	if err = MirrorRepo(cache, sourceURL, fileScheme+destinationDir, nil, nil, MirrorPushOptions{}, nil); err != nil {
		t.Fatalf("MirrorRepo failed on a broken cache entry: %v", err)
	}

	if _, err = git.PlainOpen(entryPath); err != nil {
		t.Errorf("the broken cache entry was not rebuilt: %v", err)
	}
}

// TestGitCacheMirrorRepoTakesSlotAfterLock checks that a mirroring waiting for
// the cache entry lock holds no transfer slot: the slot is only taken once the
// lock is free, and released when the mirroring is over.
func TestGitCacheMirrorRepoTakesSlotAfterLock(t *testing.T) {
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

	sourceURL := fileScheme + sourceDir

	entryPath, err := cache.repoPath(sourceURL)
	if err != nil {
		t.Fatalf("repoPath failed: %v", err)
	}

	releaseLock, err := cache.lock(entryPath)
	if err != nil {
		t.Fatalf("failed to lock the cache entry: %v", err)
	}

	var acquired, released atomic.Bool

	acquireSlot := func() func() {
		acquired.Store(true)

		return func() { released.Store(true) }
	}

	done := make(chan error, 1)

	go func() {
		done <- MirrorRepo(cache, sourceURL, fileScheme+destinationDir, nil, nil, MirrorPushOptions{}, acquireSlot)
	}()

	time.Sleep(100 * time.Millisecond)

	if acquired.Load() {
		t.Fatal("the slot was taken while the cache entry was still locked")
	}

	releaseLock()

	if err = <-done; err != nil {
		t.Fatalf("MirrorRepo failed: %v", err)
	}

	if !acquired.Load() || !released.Load() {
		t.Errorf("slot acquired = %t, released = %t; want both", acquired.Load(), released.Load())
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
	default:
	}
}
