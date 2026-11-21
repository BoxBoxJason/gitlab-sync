package helpers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
)

// lock takes the lock on an entry, waiting up to lockWaitTimeout for whoever
// holds it. The returned function releases it.
func (c *GitCache) lock(repoPath string) (func(), error) {
	release, err := c.acquireLock(repoPath, lockWaitTimeout)

	if errors.Is(err, errLockBusy) {
		return nil, fmt.Errorf("timed out after %s waiting for the cache lock on %q", lockWaitTimeout, repoPath)
	}

	if err != nil {
		return nil, err
	}

	return release, nil
}

// tryLock takes the lock on an entry only if it is free right now, reporting
// whether it succeeded.
func (c *GitCache) tryLock(repoPath string) (func(), bool) {
	release, err := c.acquireLock(repoPath, 0)
	if err != nil {
		if !errors.Is(err, errLockBusy) {
			zap.L().Debug("Failed to lock a cache entry", zap.String("path", repoPath), zap.Error(err))
		}

		return nil, false
	}

	return release, true
}

// acquireLock takes both halves of an entry's lock: a mutex serialising the
// goroutines of this run, and a lock file keeping concurrent runs - CI jobs
// sharing a cache volume, an overlapping cron - out of the same entry.
//
// A timeout of zero makes it a single attempt. errLockBusy is returned when the
// lock is held by someone else.
func (c *GitCache) acquireLock(repoPath string, timeout time.Duration) (func(), error) {
	value, _ := c.locks.LoadOrStore(repoPath, &sync.Mutex{})

	mutex, ok := value.(*sync.Mutex)
	if !ok {
		return nil, fmt.Errorf("the cache lock on %q has an unexpected type %T", repoPath, value)
	}

	if timeout <= 0 {
		if !mutex.TryLock() {
			return nil, errLockBusy
		}
	} else {
		mutex.Lock()
	}

	lock, err := acquireFileLock(repoPath+cacheLockSuffix, timeout)
	if err != nil {
		mutex.Unlock()

		return nil, err
	}

	return func() {
		lock.release()
		mutex.Unlock()
	}, nil
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
// concurrent holder to release it. errLockBusy is returned when it stays held.
func acquireFileLock(path string, timeout time.Duration) (*fileLock, error) {
	parent := filepath.Dir(path)

	err := os.MkdirAll(parent, cacheDirPermission)
	if err != nil {
		return nil, fmt.Errorf("failed to create the cache directory %q: %w", parent, err)
	}

	deadline := time.Now().Add(timeout)

	for {
		lock, lockErr := createLockFile(path)

		switch {
		case lockErr == nil:
			return lock, nil
		case !errors.Is(lockErr, errLockHeld):
			return nil, lockErr
		default:
		}

		// The holder stopped refreshing the lock: it is gone, take over.
		if breakStaleLock(path) {
			continue
		}

		if !time.Now().Before(deadline) {
			return nil, errLockBusy
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
