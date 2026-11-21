package helpers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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

	if _, err = os.Stat(entry); err != nil {
		t.Errorf("a locked entry was pruned: %v", err)
	}

	release()

	cache.Prune()

	if _, err = os.Stat(entry); !errors.Is(err, os.ErrNotExist) {
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

	if _, err = os.Stat(entry + cacheLockSuffix); !errors.Is(err, os.ErrNotExist) {
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
