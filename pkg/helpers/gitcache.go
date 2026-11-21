package helpers

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"go.uber.org/zap"
)

const (
	// cacheDirPermission is applied to every directory the cache creates: cached
	// clones hold the content of private repositories, so they stay readable by
	// their owner only.
	cacheDirPermission = 0o700
	// cacheFilePermission is applied to the lock and bookkeeping files.
	cacheFilePermission = 0o600
	// cacheRepoSuffix marks a directory as a cached bare repository, which is
	// what tells the pruner where a cache entry starts.
	cacheRepoSuffix = ".git"
	// cacheLockSuffix builds the path of an entry's lock file. The lock sits
	// next to the repository, never inside it, so discarding a broken entry
	// cannot delete the lock protecting it.
	cacheLockSuffix = ".lock"
	// cacheStaleLockSuffix names the throwaway path a stale lock is renamed to
	// before being removed.
	cacheStaleLockSuffix = ".stale"
	// lastUsedFileName is written inside a cache entry after every successful
	// sync; its modification time is the age the pruner works from.
	lastUsedFileName = "gitlab-sync-last-used"
	// unknownHostDir holds the entries of repository URLs that carry no host,
	// such as the file:// URLs used by the tests.
	unknownHostDir = "_local"
	// lockHeartbeatInterval is how often the holder of a lock refreshes it.
	lockHeartbeatInterval = 30 * time.Second
	// lockStaleAfter is how long a lock may go unrefreshed before it is taken to
	// belong to a run that died. It is comfortably above the heartbeat interval
	// so a loaded machine cannot make a live lock look abandoned.
	lockStaleAfter = 5 * time.Minute
	// lockPollInterval is how long a run waits before re-checking a held lock.
	lockPollInterval = 500 * time.Millisecond
	// lockWaitTimeout caps how long a run waits for another run to release the
	// lock on an entry. Cloning a very large repository over a slow link can
	// legitimately take a while, so this is generous.
	lockWaitTimeout = 30 * time.Minute
)

var (
	// errLockHeld reports that another run currently holds the lock on a cache entry.
	errLockHeld = errors.New("cache entry is locked by another run")
	// errLockBusy reports that the lock of an entry is held by someone else.
	errLockBusy = errors.New("the cache lock is held by another holder")
)

// GitCache keeps the bare mirror clones used by the freemium mirroring path on
// the filesystem, so consecutive runs refresh an existing clone instead of
// downloading every repository from scratch.
//
// Every entry lives at a location derived from the source repository URL, which
// makes a run reproducible: the same repository always maps to the same
// directory, and that directory always ends up an exact copy of the source
// (refs deleted upstream are deleted locally, new ones are fetched).
//
// The zero value is not usable; build one with NewGitCache.
type GitCache struct {
	// locks holds one mutex per entry, serialising the goroutines of this run.
	// Concurrent processes are kept apart by a lock file instead.
	locks  sync.Map
	root   string
	maxAge time.Duration
}

// NewGitCache prepares the repository cache rooted at dir, creating it when it
// does not exist yet.
//
// maxAge is how long an entry survives without being synced; Prune removes the
// older ones. A value of zero (or less) keeps entries forever.
func NewGitCache(dir string, maxAge time.Duration) (*GitCache, error) {
	root, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve the git cache directory %q: %w", dir, err)
	}

	err = os.MkdirAll(root, cacheDirPermission)
	if err != nil {
		return nil, fmt.Errorf("failed to create the git cache directory %q: %w", root, err)
	}

	zap.L().Info("Git repository cache enabled", zap.String("path", root), zap.Duration("maxAge", maxAge))

	return &GitCache{root: root, maxAge: maxAge}, nil
}

// Root returns the absolute path the cache is rooted at.
func (c *GitCache) Root() string {
	return c.root
}

// Prune removes the cache entries that have not been synced for longer than the
// configured maximum age. It does nothing when no maximum age is configured.
//
// Entries another run is working on are left alone, and every failure is logged
// and skipped: housekeeping the cache is never a reason to fail a mirroring run.
func (c *GitCache) Prune() {
	if c.maxAge <= 0 {
		return
	}

	zap.L().Debug("Pruning the git repository cache", zap.String("path", c.root), zap.Duration("maxAge", c.maxAge))

	for _, repoPath := range c.entries() {
		c.pruneEntry(repoPath)
	}
}

// mirrorRepo refreshes the cached bare mirror of sourceURL, then force-pushes it
// to destinationURL.
func (c *GitCache) mirrorRepo(sourceURL, destinationURL string, pullAuth, pushAuth transport.AuthMethod, pushOptions MirrorPushOptions) error {
	repoPath, err := c.repoPath(sourceURL)
	if err != nil {
		return err
	}

	release, err := c.lock(repoPath)
	if err != nil {
		return err
	}

	defer release()

	repo, err := syncCachedRepo(repoPath, sourceURL, pullAuth)
	if err != nil {
		return err
	}

	err = pushMirror(repo, destinationURL, pushAuth, pushOptions)
	if err != nil {
		return err
	}

	touchLastUsed(repoPath)

	return nil
}

// ===========================================================================
//                              ENTRY LOCATION                              //
// ===========================================================================

// ===========================================================================
//                            ENTRY SYNCHRONISATION                         //
// ===========================================================================

// ===========================================================================
//                                  PRUNING                                 //
// ===========================================================================

// entries lists the cache entries, that is every *.git directory below the cache
// root. It never descends into an entry.
func (c *GitCache) entries() []string {
	var repoPaths []string

	err := filepath.WalkDir(c.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			zap.L().Warn("Failed to walk the git repository cache", zap.String("path", path), zap.Error(err))

			return nil
		}

		if !entry.IsDir() || !strings.HasSuffix(entry.Name(), cacheRepoSuffix) {
			return nil
		}

		repoPaths = append(repoPaths, path)

		return fs.SkipDir
	})
	if err != nil {
		zap.L().Warn("Failed to list the git repository cache entries", zap.String("path", c.root), zap.Error(err))
	}

	return repoPaths
}

// pruneEntry removes a single expired entry.
func (c *GitCache) pruneEntry(repoPath string) {
	lastUsed, err := entryLastUsed(repoPath)
	if err != nil {
		zap.L().Warn("Failed to determine the age of a cache entry", zap.String("path", repoPath), zap.Error(err))

		return
	}

	if time.Since(lastUsed) <= c.maxAge {
		return
	}

	release, acquired := c.tryLock(repoPath)
	if !acquired {
		zap.L().Debug("Skipping an expired cache entry another run is using", zap.String("path", repoPath))

		return
	}

	defer release()

	zap.L().Info("Removing an expired cache entry", zap.String("path", repoPath), zap.Time("lastUsed", lastUsed))

	err = os.RemoveAll(repoPath)
	if err != nil {
		zap.L().Warn("Failed to remove an expired cache entry", zap.String("path", repoPath), zap.Error(err))
	}
}

// entryLastUsed reports when the entry was last synced, falling back to the
// directory's own modification time when the marker is missing (an entry left by
// a run that was interrupted before it could write one).
func entryLastUsed(repoPath string) (time.Time, error) {
	markerPath := filepath.Join(repoPath, lastUsedFileName)

	info, err := os.Stat(markerPath)
	if err == nil {
		return info.ModTime(), nil
	}

	if !errors.Is(err, os.ErrNotExist) {
		return time.Time{}, fmt.Errorf("failed to stat %q: %w", markerPath, err)
	}

	info, err = os.Stat(repoPath)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to stat %q: %w", repoPath, err)
	}

	return info.ModTime(), nil
}

// ===========================================================================
//                                  LOCKING                                 //
// ===========================================================================
