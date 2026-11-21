package helpers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"go.uber.org/zap"
)

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
	default:
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
