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
	DEFAULT_GIT_USER = "git"
)

func cleanupTempDir(path string) {
	removeErr := os.RemoveAll(path)
	if removeErr != nil {
		zap.L().Warn("failed to remove temporary directory", zap.String("path", path), zap.Error(removeErr))
	}
}

// MirrorRepo clones the source remote as a bare repo and force-pushes all refs
// (branches and tags) to the destination.
//
// The destination HEAD (its default branch) is not touched here: it cannot be set
// over the git transport, and the caller aligns it through the GitLab API when it
// syncs the project attributes.
func MirrorRepo(sourceURL, destinationURL string, pullAuth, pushAuth transport.AuthMethod) error {
	tmpDir, err := os.MkdirTemp("", "bare-mirror-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}

	defer cleanupTempDir(tmpDir)

	pullOpts := &git.CloneOptions{
		URL:    sourceURL,
		Mirror: true,
	}
	if pullAuth != nil {
		pullOpts.Auth = pullAuth
	}

	zap.L().Debug("Cloning source repository", zap.String("sourceURL", sourceURL), zap.String("destinationURL", destinationURL))

	srcRepo, err := git.PlainClone(tmpDir, true, pullOpts)
	if err != nil {
		return fmt.Errorf("failed to clone source repository locally: %w", err)
	}

	zap.L().Debug("Adding destination remote", zap.String("destinationURL", destinationURL))

	_, err = srcRepo.CreateRemote(&config.RemoteConfig{
		Name: "destination",
		URLs: []string{destinationURL},
	})
	if err != nil {
		return fmt.Errorf("failed to create remote for destination: %w", err)
	}

	zap.L().Debug("Pushing to destination repository", zap.String("destinationURL", destinationURL))

	pushOpts := &git.PushOptions{
		RemoteName: "destination",
		Force:      true,
		RefSpecs: []config.RefSpec{
			// force-update everything (branches, tags, etc)
			config.RefSpec("+refs/*:refs/*"),
		},
	}
	if pushAuth != nil {
		pushOpts.Auth = pushAuth
	}

	err = srcRepo.Push(pushOpts)
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
		username = DEFAULT_GIT_USER
	}

	return &http.BasicAuth{
		Username: username,
		Password: token,
	}
}
