package helpers

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
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

// errLockHeld reports that another run currently holds the lock on a cache entry.
var errLockHeld = errors.New("cache entry is locked by another run")

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
func (c *GitCache) mirrorRepo(sourceURL, destinationURL string, pullAuth, pushAuth transport.AuthMethod) error {
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

	err = pushMirror(repo, destinationURL, pushAuth)
	if err != nil {
		return err
	}

	touchLastUsed(repoPath)

	return nil
}

// ===========================================================================
//                              ENTRY LOCATION                              //
// ===========================================================================

// repoPath returns the deterministic location of the bare mirror backing
// sourceURL: <root>/<host>/<namespace...>/<repository>.git
//
// Every segment is sanitised, and the result is checked against the cache root,
// so a repository URL can never place an entry outside of the cache.
func (c *GitCache) repoPath(sourceURL string) (string, error) {
	segments := repoURLSegments(sourceURL)
	if len(segments) == 0 {
		return "", fmt.Errorf("failed to derive a cache path from the repository URL %q", sourceURL)
	}

	last := len(segments) - 1
	segments[last] = strings.TrimSuffix(segments[last], cacheRepoSuffix) + cacheRepoSuffix

	repoPath := filepath.Join(append([]string{c.root}, segments...)...)
	if !strings.HasPrefix(repoPath, c.root+string(os.PathSeparator)) {
		return "", fmt.Errorf("refusing to use the cache path %q, which escapes the cache root %q", repoPath, c.root)
	}

	return repoPath, nil
}

// repoURLSegments splits a repository URL into the sanitised path segments its
// cache entry is nested under. The host comes first, so two instances serving
// the same namespace never collide on a single entry.
//
// It returns nil when the URL carries nothing that identifies a repository.
func repoURLSegments(sourceURL string) []string {
	host, repoPath := splitRepoURL(strings.TrimSpace(sourceURL))

	segments := make([]string, 0, strings.Count(repoPath, "/")+2) //nolint:mnd // host segment + path segments
	segments = appendSanitizedSegment(segments, host, unknownHostDir)

	for segment := range strings.SplitSeq(repoPath, "/") {
		segments = appendSanitizedSegment(segments, segment, "")
	}

	// The first segment is the host: on its own it does not name a repository.
	if len(segments) < 2 { //nolint:mnd // a host and at least one path segment
		return nil
	}

	return segments
}

// splitRepoURL extracts the host and the repository path from the URL forms a
// GitLab instance hands out: https://host/group/repo.git, ssh://git@host/group/repo.git,
// the scp-like git@host:group/repo.git, and the file:// URLs used by the tests.
//
// Both values are returned empty rather than guessed when the URL cannot be
// understood; the caller decides what to do with that.
func splitRepoURL(rawURL string) (string, string) {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Scheme != "" {
		// The escaped form is kept on purpose: decoding it would turn a %2F
		// inside a segment into a directory separator, letting two different
		// repositories share one cache entry.
		return parsed.Hostname(), strings.Trim(parsed.EscapedPath(), "/")
	}

	// scp-like syntax carries no scheme: [user@]host:path
	host, repoPath, found := strings.Cut(rawURL, ":")
	if found && !strings.Contains(host, "/") {
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}

		return host, strings.Trim(repoPath, "/")
	}

	return "", strings.Trim(rawURL, "/")
}

// appendSanitizedSegment appends segment to segments once it has been reduced to
// characters that are safe in a path. An empty result is replaced by fallback,
// or dropped when fallback is empty.
func appendSanitizedSegment(segments []string, segment, fallback string) []string {
	sanitized := sanitizePathSegment(segment)
	if sanitized == "" {
		sanitized = fallback
	}

	if sanitized == "" {
		return segments
	}

	return append(segments, sanitized)
}

// sanitizePathSegment reduces a single URL segment to a safe directory name:
// anything outside [A-Za-z0-9._-] becomes an underscore, and the "." and ".."
// traversal segments are rejected outright.
func sanitizePathSegment(segment string) string {
	sanitized := strings.Map(func(char rune) rune {
		switch {
		case char >= 'a' && char <= 'z',
			char >= 'A' && char <= 'Z',
			char >= '0' && char <= '9',
			char == '.', char == '_', char == '-':
			return char
		default:
			return '_'
		}
	}, segment)

	if sanitized == "." || sanitized == ".." {
		return ""
	}

	return sanitized
}

// ===========================================================================
//                            ENTRY SYNCHRONISATION                         //
// ===========================================================================

// syncCachedRepo brings the bare mirror at repoPath in line with sourceURL,
// cloning it when the cache holds no usable entry yet.
//
// An entry that cannot be opened or refreshed - an interrupted run, a truncated
// packfile, a source whose history was rewritten past what a fetch can
// reconcile - is discarded and cloned again rather than failing the sync: the
// cache is an optimisation, never a source of truth.
func syncCachedRepo(repoPath, sourceURL string, pullAuth transport.AuthMethod) (*git.Repository, error) {
	repo := openCachedRepo(repoPath, sourceURL)
	if repo != nil {
		err := fetchMirror(repo, sourceURL, pullAuth)
		if err == nil {
			return repo, nil
		}

		zap.L().Warn("Failed to refresh the cached repository, cloning it again", zap.String("path", repoPath), zap.String("sourceURL", sourceURL), zap.Error(err))
	}

	// Either the cache holds nothing usable at that path, or what it holds could
	// not be refreshed: start over from a clean directory.
	err := os.RemoveAll(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to discard the unusable cache entry %q: %w", repoPath, err)
	}

	return cloneCachedRepo(repoPath, sourceURL, pullAuth)
}

// openCachedRepo opens the bare mirror at repoPath, returning nil when the cache
// holds nothing usable there.
func openCachedRepo(repoPath, sourceURL string) *git.Repository {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		if !errors.Is(err, git.ErrRepositoryNotExists) {
			zap.L().Warn("Ignoring an unreadable cache entry", zap.String("path", repoPath), zap.Error(err))
		}

		return nil
	}

	err = alignOriginRemote(repo, sourceURL)
	if err != nil {
		zap.L().Warn("Ignoring a cache entry whose remote could not be realigned", zap.String("path", repoPath), zap.Error(err))

		return nil
	}

	return repo
}

// alignOriginRemote points the entry's origin remote at sourceURL.
//
// The URL is part of the stored configuration, so an instance that moved (or a
// project reached over a different scheme) would otherwise leave the entry
// fetching from an address that no longer serves it.
func alignOriginRemote(repo *git.Repository, sourceURL string) error {
	remote, err := repo.Remote(git.DefaultRemoteName)

	switch {
	case err == nil && len(remote.Config().URLs) > 0 && remote.Config().URLs[0] == sourceURL:
		return nil
	case err == nil:
		deleteErr := repo.DeleteRemote(git.DefaultRemoteName)
		if deleteErr != nil {
			return fmt.Errorf("failed to drop the outdated origin remote: %w", deleteErr)
		}
	case !errors.Is(err, git.ErrRemoteNotFound):
		return fmt.Errorf("failed to read the origin remote: %w", err)
	}

	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name:   git.DefaultRemoteName,
		URLs:   []string{sourceURL},
		Fetch:  []config.RefSpec{config.RefSpec(mirrorRefSpec)},
		Mirror: true,
	})
	if err != nil {
		return fmt.Errorf("failed to point the origin remote at %s: %w", sourceURL, err)
	}

	return nil
}

// fetchMirror refreshes every ref of a cached entry from its source.
//
// The refspec is forced and pruning is on, so the entry mirrors the source
// exactly: branches and tags deleted upstream disappear here too, new ones are
// downloaded, and a rewritten history replaces the one held locally. Without the
// prune, a cached entry would slowly become the union of every run.
func fetchMirror(repo *git.Repository, sourceURL string, pullAuth transport.AuthMethod) error {
	zap.L().Debug("Refreshing the cached source repository", zap.String("sourceURL", sourceURL))

	fetchOpts := &git.FetchOptions{
		RemoteName: git.DefaultRemoteName,
		RemoteURL:  sourceURL,
		RefSpecs:   []config.RefSpec{config.RefSpec(mirrorFetchRefSpec)},
		Force:      true,
		Prune:      true,
		// Every tag is already covered by the mirror refspec; following tags on
		// top of it would only make the result depend on the server's
		// capabilities.
		Tags: git.NoTags,
		Auth: pullAuth,
	}

	err := repo.Fetch(fetchOpts)
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("failed to fetch %s: %w", sourceURL, err)
	}

	return nil
}

// cloneCachedRepo populates a fresh cache entry at repoPath.
//
// A clone that fails halfway is removed: leaving it behind would hand the next
// run a repository that opens but holds an incomplete history.
func cloneCachedRepo(repoPath, sourceURL string, pullAuth transport.AuthMethod) (*git.Repository, error) {
	zap.L().Debug("Cloning the source repository into the cache", zap.String("sourceURL", sourceURL), zap.String("path", repoPath))

	parent := filepath.Dir(repoPath)

	err := os.MkdirAll(parent, cacheDirPermission)
	if err != nil {
		return nil, fmt.Errorf("failed to create the cache directory %q: %w", parent, err)
	}

	repo, err := cloneBareMirror(repoPath, sourceURL, pullAuth)
	if err != nil {
		removeErr := os.RemoveAll(repoPath)
		if removeErr != nil {
			zap.L().Warn("Failed to remove a partial cache entry", zap.String("path", repoPath), zap.Error(removeErr))
		}

		return nil, err
	}

	return repo, nil
}

// touchLastUsed records that the entry was synced now, which is what the pruner
// ages entries on. A failure here only costs the entry an earlier expiry.
func touchLastUsed(repoPath string) {
	markerPath := filepath.Join(repoPath, lastUsedFileName)

	err := os.WriteFile(markerPath, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), cacheFilePermission)
	if err != nil {
		zap.L().Debug("Failed to record the cache entry usage", zap.String("path", markerPath), zap.Error(err))
	}
}

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

// lock takes the lock on an entry, waiting up to lockWaitTimeout for whoever
// holds it. The returned function releases it.
func (c *GitCache) lock(repoPath string) (func(), error) {
	release, acquired, err := c.acquireLock(repoPath, lockWaitTimeout)

	switch {
	case err != nil:
		return nil, err
	case !acquired:
		return nil, fmt.Errorf("timed out after %s waiting for the cache lock on %q", lockWaitTimeout, repoPath)
	}

	return release, nil
}

// tryLock takes the lock on an entry only if it is free right now, reporting
// whether it succeeded.
func (c *GitCache) tryLock(repoPath string) (func(), bool) {
	release, acquired, err := c.acquireLock(repoPath, 0)
	if err != nil {
		zap.L().Debug("Failed to lock a cache entry", zap.String("path", repoPath), zap.Error(err))

		return nil, false
	}

	return release, acquired
}

// acquireLock takes both halves of an entry's lock: a mutex serialising the
// goroutines of this run, and a lock file keeping concurrent runs - CI jobs
// sharing a cache volume, an overlapping cron - out of the same entry.
//
// A timeout of zero makes it a single attempt.
func (c *GitCache) acquireLock(repoPath string, timeout time.Duration) (func(), bool, error) {
	value, _ := c.locks.LoadOrStore(repoPath, &sync.Mutex{})

	mutex, ok := value.(*sync.Mutex)
	if !ok {
		return nil, false, fmt.Errorf("the cache lock on %q has an unexpected type %T", repoPath, value)
	}

	if timeout <= 0 {
		if !mutex.TryLock() {
			return nil, false, nil
		}
	} else {
		mutex.Lock()
	}

	lock, acquired, err := acquireFileLock(repoPath+cacheLockSuffix, timeout)
	if err != nil || !acquired {
		mutex.Unlock()

		return nil, false, err
	}

	return func() {
		lock.release()
		mutex.Unlock()
	}, true, nil
}

// fileLock is an advisory cross-process lock materialised by an exclusively
// created file. Its modification time is refreshed while it is held, so a lock
// left behind by a run that died can be told apart from one a long clone is
// still legitimately using.
type fileLock struct {
	stop chan struct{}
	done chan struct{}
	path string
}

// acquireFileLock creates the lock file at path, waiting up to timeout for a
// concurrent holder to release it. It reports whether the lock was taken.
func acquireFileLock(path string, timeout time.Duration) (*fileLock, bool, error) {
	parent := filepath.Dir(path)

	err := os.MkdirAll(parent, cacheDirPermission)
	if err != nil {
		return nil, false, fmt.Errorf("failed to create the cache directory %q: %w", parent, err)
	}

	deadline := time.Now().Add(timeout)

	for {
		lock, err := createLockFile(path)

		switch {
		case err == nil:
			return lock, true, nil
		case !errors.Is(err, errLockHeld):
			return nil, false, err
		}

		// The holder stopped refreshing the lock: it is gone, take over.
		if breakStaleLock(path) {
			continue
		}

		if !time.Now().Before(deadline) {
			return nil, false, nil
		}

		time.Sleep(lockPollInterval)
	}
}

// createLockFile creates the lock file exclusively, returning errLockHeld when
// another run got there first.
func createLockFile(path string) (*fileLock, error) {
	//nolint:gosec // the path is the operator's own cache directory, and the lock is created, never read back
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, cacheFilePermission)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, errLockHeld
		}

		return nil, fmt.Errorf("failed to create the cache lock file %q: %w", path, err)
	}

	// The owner is written for the benefit of whoever inspects a stuck cache; it
	// is not read back, so a failure here does not invalidate the lock.
	_, err = file.WriteString(strconv.Itoa(os.Getpid()) + "\n")
	if err != nil {
		zap.L().Debug("Failed to write the cache lock owner", zap.String("path", path), zap.Error(err))
	}

	err = file.Close()
	if err != nil {
		zap.L().Debug("Failed to close the cache lock file", zap.String("path", path), zap.Error(err))
	}

	return startFileLock(path), nil
}

// breakStaleLock removes a lock file whose owner stopped refreshing it, which
// means the run holding it died. It reports whether the lock was removed.
//
// The lock is renamed before being removed: rename is atomic, so two runs racing
// to break the same stale lock cannot both conclude they won it.
func breakStaleLock(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		// The lock vanished on its own; retrying immediately is the right move.
		return errors.Is(err, os.ErrNotExist)
	}

	if time.Since(info.ModTime()) < lockStaleAfter {
		return false
	}

	stalePath := path + cacheStaleLockSuffix + "." + strconv.Itoa(os.Getpid())

	err = os.Rename(path, stalePath)
	if err != nil {
		// Another run broke it first, or replaced it with its own lock.
		return false
	}

	zap.L().Warn("Broke a stale git cache lock", zap.String("path", path), zap.Time("lastRefresh", info.ModTime()))

	err = os.Remove(stalePath)
	if err != nil {
		zap.L().Warn("Failed to remove a broken git cache lock", zap.String("path", stalePath), zap.Error(err))
	}

	return true
}

// startFileLock begins refreshing a freshly created lock file.
func startFileLock(path string) *fileLock {
	lock := &fileLock{
		path: path,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}

	go lock.refresh()

	return lock
}

// refresh keeps the lock file's modification time current, so a concurrent run
// does not mistake a long clone for a crashed one.
func (l *fileLock) refresh() {
	defer close(l.done)

	ticker := time.NewTicker(lockHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-l.stop:
			return
		case now := <-ticker.C:
			err := os.Chtimes(l.path, now, now)
			if err != nil {
				zap.L().Debug("Failed to refresh the cache lock", zap.String("path", l.path), zap.Error(err))
			}
		}
	}
}

// release stops the heartbeat and removes the lock file.
func (l *fileLock) release() {
	close(l.stop)
	<-l.done

	err := os.Remove(l.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		zap.L().Warn("Failed to remove the cache lock", zap.String("path", l.path), zap.Error(err))
	}
}
