package helpers

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"go.uber.org/zap"
)

const (
	// DefaultGitUser is the user name used for git operations over SSH and HTTP.
	DefaultGitUser = "git"
	// mirrorRefSpec force-maps every ref - branches, tags, everything under
	// refs/ - onto the same name on the other side. Pushes do not use it: see
	// pushableRefSpecs.
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

	// The GitLab push options below keep a mirroring push from running CI on the
	// destination. go-git always sends push options as "key=value"; GitLab reads
	// the empty value as true and ignores the keys it does not know, so each one
	// is safe to send to any GitLab version.

	// ciSkipPushOption (`git push -o ci.skip`) is honoured by every GitLab version
	// with push options, but GitLab still records one "skipped" pipeline per
	// updated ref when the project has a CI configuration.
	ciSkipPushOption = "ci.skip"
	// ciNoPipelinePushOption keeps the pipeline from being created at all. GitLab
	// checks it before ci.skip, but only from 18.11 and when the
	// ci_no_pipeline_push_option feature flag is enabled on the instance.
	ciNoPipelinePushOption = "ci.no_pipeline"
	// integrationsSkipCIPushOption keeps the CI integrations (Jenkins, ...) from
	// being triggered, which ci.skip does not cover.
	integrationsSkipCIPushOption = "integrations.skip_ci"
)

// MirrorPushOptions tunes the push of a mirroring run.
type MirrorPushOptions struct {
	// SkipPipelines asks the destination GitLab not to run CI/CD (pipelines and
	// CI integrations) for the branches and tags the push updates.
	SkipPipelines bool
}

var (
	// skipPipelinesGitOptions are the push options of a push that must not run CI.
	// They are shared by every push and must never be modified.
	//
	//nolint:gochecknoglobals // read-only, shared to avoid rebuilding it on every push
	skipPipelinesGitOptions = map[string]string{
		ciSkipPushOption:             "",
		ciNoPipelinePushOption:       "",
		integrationsSkipCIPushOption: "",
	}
)

// gitOptions returns the push options a mirroring push carries.
//
// A server that does not advertise the push-options capability never receives
// them (go-git drops them), so they are harmless for non-GitLab destinations.
func (o MirrorPushOptions) gitOptions() map[string]string {
	if !o.SkipPipelines {
		return nil
	}

	return skipPipelinesGitOptions
}

func cleanupTempDir(path string) {
	removeErr := os.RemoveAll(path)
	if removeErr != nil {
		zap.L().Warn("failed to remove temporary directory", zap.String("path", path), zap.Error(removeErr))
	}
}

// MirrorRepo clones the source remote as a bare repo and force-pushes all refs
// (branches, tags and any other namespace GitLab accepts, see pushableRefSpecs) to
// the destination, deleting the destination branches and tags the source no longer has.
//
// When cache is nil the clone lands in a temporary directory that is removed
// before returning, so every run downloads the whole history again. With a cache
// the bare clone is kept between runs at a location derived from the source URL
// and is refreshed with a pruning fetch, which leaves it an exact copy of the
// source without re-downloading what it already holds.
//
// pushOptions tunes the push itself: with SkipPipelines, the destination GitLab
// is asked not to run CI for the branches and tags the mirroring updates.
//
// The destination HEAD (its default branch) is not touched here: it cannot be set
// over the git transport, and the caller aligns it through the GitLab API when it
// syncs the project attributes.
func MirrorRepo(cache *GitCache, sourceURL, destinationURL string, pullAuth, pushAuth transport.AuthMethod, pushOptions MirrorPushOptions) error {
	if cache != nil {
		return cache.mirrorRepo(sourceURL, destinationURL, pullAuth, pushAuth, pushOptions)
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

	return pushMirror(srcRepo, destinationURL, pushAuth, pushOptions)
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
// ends up an exact copy of what was cloned or fetched. pushOptions decides the
// push options it carries (see MirrorPushOptions.gitOptions).
func pushMirror(repo *git.Repository, destinationURL string, pushAuth transport.AuthMethod, pushOptions MirrorPushOptions) error {
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
		Options:    pushOptions.gitOptions(),
	}

	err = remote.Push(pushOpts)
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("failed to push to destination repository: %w", err)
	}

	return nil
}

// BuildHTTPAuth creates an HTTP BasicAuth object using a username and token.
func BuildHTTPAuth(username, token string) transport.AuthMethod {
	if token == "" && username == "" {
		return nil
	}

	if strings.TrimSpace(username) == "" {
		username = DefaultGitUser
	}

	return &http.BasicAuth{
		Username: username,
		Password: token,
	}
}
