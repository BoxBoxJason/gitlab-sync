package helpers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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

	if _, err = os.Stat(freshEntry); err != nil {
		t.Errorf("the fresh entry was removed: %v", err)
	}

	if _, err = os.Stat(expiredEntry); !errors.Is(err, os.ErrNotExist) {
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

	if _, err = os.Stat(entry); err != nil {
		t.Errorf("an entry was expired although expiry is disabled: %v", err)
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
