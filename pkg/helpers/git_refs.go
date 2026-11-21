package helpers

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"go.uber.org/zap"
)

// mirrorPushRefSpecs builds the refspecs of a mirroring push: one forced
// wildcard update per ref namespace held locally (see pushableRefSpecs), followed
// by one explicit deletion per destination ref that no longer exists at the source.
//
// The deletions are spelled out rather than left to PushOptions.Prune, which
// go-git derives from the reversed refspec and therefore gets wrong for a
// forced wildcard (see mirrorFetchRefSpec): it would ask the destination to
// delete every ref, including the ones the very same push updates.
func mirrorPushRefSpecs(repo *git.Repository, remote *git.Remote, pushAuth transport.AuthMethod) ([]config.RefSpec, error) {
	refSpecs, err := pushableRefSpecs(repo)
	if err != nil {
		return nil, err
	}

	staleRefs, err := staleDestinationRefs(repo, remote, pushAuth)
	if err != nil {
		return nil, err
	}

	for _, name := range staleRefs {
		refSpecs = append(refSpecs, config.RefSpec(":"+name.String()))
	}

	return refSpecs, nil
}

// isGitLabHiddenRefNamespace tells whether namespace (the first element of a ref
// name after "refs/") is one GitLab reserves for itself. GitLab advertises some of
// them to fetches (a mirror clone of a GitLab project holds refs/merge-requests/*),
// but its receive-pack refuses every update to them with "deny updating a hidden
// ref", which would fail the push.
func isGitLabHiddenRefNamespace(namespace string) bool {
	switch namespace {
	case "merge-requests", "pipelines", "environments", "keep-around", "tmp", "remotes":
		return true
	default:
		return false
	}
}

// pushableRefSpecs returns one forced refspec per ref namespace held locally
// ("+refs/heads/*:refs/heads/*", "+refs/notes/*:refs/notes/*", ...), leaving out
// the namespaces GitLab reserves for itself (see isGitLabHiddenRefNamespace) and
// the refs right under refs/, which no git server accepts.
// A single "+refs/*:refs/*" cannot be used for that: go-git has no negative refspecs.
func pushableRefSpecs(repo *git.Repository) ([]config.RefSpec, error) {
	heldRefs, err := localRefNames(repo)
	if err != nil {
		return nil, err
	}

	refSpecs := make(map[config.RefSpec]struct{})

	for name := range heldRefs {
		relativeName, underRefs := strings.CutPrefix(name.String(), "refs/")
		if !underRefs {
			continue
		}

		// A ref right under refs/ (refs/stash, ...) is refused by receive-pack
		// ("funny refname"), so there is nothing to push for it.
		namespace, _, nested := strings.Cut(relativeName, "/")
		if !nested || isGitLabHiddenRefNamespace(namespace) {
			continue
		}

		refSpecs[config.RefSpec("+refs/"+namespace+"/*:refs/"+namespace+"/*")] = struct{}{}
	}

	return slices.Sorted(maps.Keys(refSpecs)), nil
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
