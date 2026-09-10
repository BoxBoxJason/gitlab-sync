package helpers

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"go.uber.org/zap"
)

const (
	DEFAULT_GIT_USER = "git"
	// mirrorRefSpec force-maps every ref - branches, tags, everything under
	// refs/ - onto the same name on the other side.
	mirrorRefSpec = "+refs/*:refs/*"
	// mirrorFetchRefSpec is the same mapping without the force marker; a fetch
	// forces its updates through FetchOptions.Force instead.
	//
	// The distinction is not cosmetic. Pruning makes go-git reverse the refspec
	// to work out which local refs the remote no longer has, and RefSpec.Reverse
	// only swaps the two halves around the colon: "+refs/*:refs/*" reverses to
	// the malformed "refs/*:+refs/*", whose destination pattern then expands to
	// "+refs/heads/main". Nothing ever matches that, so a pruning fetch with a
	// forced refspec deletes every local ref instead of the missing ones.
	mirrorFetchRefSpec = "refs/*:refs/*"
	// destinationRemoteName names the in-memory remote a mirroring push goes
	// through. It is never written to the repository configuration, which keeps
	// a cached clone free of destination-specific state.
	destinationRemoteName = "destination"
)

func cleanupTempDir(path string) {
	removeErr := os.RemoveAll(path)
	if removeErr != nil {
		zap.L().Warn("failed to remove temporary directory", zap.String("path", path), zap.Error(removeErr))
	}
}

// MirrorRepo clones the source remote as a bare repo and force-pushes all refs
// (branches and tags) to the destination, deleting the destination branches and
// tags the source no longer has.
//
// When cache is nil the clone lands in a temporary directory that is removed
// before returning, so every run downloads the whole history again. With a cache
// the bare clone is kept between runs at a location derived from the source URL
// and is refreshed with a pruning fetch, which leaves it an exact copy of the
// source without re-downloading what it already holds.
//
// The destination HEAD (its default branch) is not touched here: it cannot be set
// over the git transport, and the caller aligns it through the GitLab API when it
// syncs the project attributes.
func MirrorRepo(cache *GitCache, sourceURL, destinationURL string, pullAuth, pushAuth transport.AuthMethod) error {
	if cache != nil {
		return cache.mirrorRepo(sourceURL, destinationURL, pullAuth, pushAuth)
	}

	tmpDir, err := os.MkdirTemp("", "bare-mirror-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}

	defer cleanupTempDir(tmpDir)

	zap.L().Debug("Cloning source repository", zap.String("sourceURL", sourceURL), zap.String("destinationURL", destinationURL))

	srcRepo, err := cloneBareMirror(tmpDir, sourceURL, pullAuth)
	if err != nil {
		return err
	}

	return pushMirror(srcRepo, destinationURL, pushAuth)
}

// cloneBareMirror mirror-clones sourceURL into path as a bare repository.
func cloneBareMirror(path, sourceURL string, pullAuth transport.AuthMethod) (*git.Repository, error) {
	cloneOpts := &git.CloneOptions{
		URL:    sourceURL,
		Mirror: true,
		Auth:   pullAuth,
	}

	repo, err := git.PlainClone(path, true, cloneOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to clone source repository locally: %w", err)
	}

	return repo, nil
}

// pushMirror force-pushes every ref of repo to destinationURL and deletes the
// destination branches and tags that are not held locally, so the destination
// ends up an exact copy of what was cloned or fetched.
func pushMirror(repo *git.Repository, destinationURL string, pushAuth transport.AuthMethod) error {
	zap.L().Debug("Pushing to destination repository", zap.String("destinationURL", destinationURL))

	remote := git.NewRemote(repo.Storer, &config.RemoteConfig{
		Name: destinationRemoteName,
		URLs: []string{destinationURL},
	})

	refSpecs, err := mirrorPushRefSpecs(repo, remote, pushAuth)
	if err != nil {
		return err
	}

	pushOpts := &git.PushOptions{
		RemoteName: destinationRemoteName,
		Force:      true,
		RefSpecs:   refSpecs,
		Auth:       pushAuth,
	}

	err = remote.Push(pushOpts)
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("failed to push to destination repository: %w", err)
	}

	return nil
}

// mirrorPushRefSpecs builds the refspecs of a mirroring push: one forced
// wildcard update carrying everything held locally, followed by one explicit
// deletion per destination ref that no longer exists at the source.
//
// The deletions are spelled out rather than left to PushOptions.Prune, which
// go-git derives from the reversed refspec and therefore gets wrong for a
// forced wildcard (see mirrorFetchRefSpec): it would ask the destination to
// delete every ref, including the ones the very same push updates.
func mirrorPushRefSpecs(repo *git.Repository, remote *git.Remote, pushAuth transport.AuthMethod) ([]config.RefSpec, error) {
	refSpecs := []config.RefSpec{config.RefSpec(mirrorRefSpec)}

	staleRefs, err := staleDestinationRefs(repo, remote, pushAuth)
	if err != nil {
		return nil, err
	}

	for _, name := range staleRefs {
		refSpecs = append(refSpecs, config.RefSpec(":"+name.String()))
	}

	return refSpecs, nil
}

// staleDestinationRefs lists the destination branches and tags the local copy
// no longer holds.
//
// Only branches and tags are considered: the other namespaces a GitLab remote
// advertises (refs/merge-requests, refs/pipelines, refs/keep-around, ...) belong
// to the instance itself and are rejected on write, so asking to delete one
// would fail the whole push. The branch the destination HEAD points at is left
// alone for the same reason - a server refuses to delete its current branch -
// and it disappears on the next run, once the API side has moved the default
// branch to one the source still has.
func staleDestinationRefs(repo *git.Repository, remote *git.Remote, pushAuth transport.AuthMethod) ([]plumbing.ReferenceName, error) {
	destinationRefs, err := remote.List(&git.ListOptions{
		Auth: pushAuth,
		// Peeled entries ("refs/tags/v1^{}") are not refs of their own; asking
		// the server to delete one would fail the push.
		PeelingOption: git.IgnorePeeled,
	})
	if err != nil {
		if errors.Is(err, transport.ErrEmptyRemoteRepository) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to list the destination references: %w", err)
	}

	heldRefs, err := localRefNames(repo)
	if err != nil {
		return nil, err
	}

	currentBranch := advertisedHeadBranch(destinationRefs)

	var staleRefs []plumbing.ReferenceName

	for _, ref := range destinationRefs {
		name := ref.Name()

		if ref.Type() != plumbing.HashReference || !name.IsBranch() && !name.IsTag() {
			continue
		}

		if _, held := heldRefs[name]; held {
			continue
		}

		if name == currentBranch {
			zap.L().Debug("Keeping the destination default branch although the source dropped it", zap.String("ref", name.String()))

			continue
		}

		staleRefs = append(staleRefs, name)
	}

	return staleRefs, nil
}

// localRefNames collects the names of the refs the local copy resolves to an
// object, which is what the destination is compared against.
func localRefNames(repo *git.Repository) (map[plumbing.ReferenceName]struct{}, error) {
	iter, err := repo.References()
	if err != nil {
		return nil, fmt.Errorf("failed to list the local references: %w", err)
	}

	names := make(map[plumbing.ReferenceName]struct{})

	err = iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() == plumbing.HashReference {
			names[ref.Name()] = struct{}{}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read the local references: %w", err)
	}

	return names, nil
}

// advertisedHeadBranch returns the branch the remote HEAD points at, or an empty
// name when the remote does not advertise it.
func advertisedHeadBranch(refs []*plumbing.Reference) plumbing.ReferenceName {
	for _, ref := range refs {
		if ref.Name() == plumbing.HEAD && ref.Type() == plumbing.SymbolicReference {
			return ref.Target()
		}
	}

	return ""
}

// BuildHTTPAuth creates an HTTP BasicAuth object using a username and token.
func BuildHTTPAuth(username, token string) transport.AuthMethod {
	if token == "" && username == "" {
		return nil
	}

	if strings.TrimSpace(username) == "" {
		username = DEFAULT_GIT_USER
	}

	return &http.BasicAuth{
		Username: username,
		Password: token,
	}
}
