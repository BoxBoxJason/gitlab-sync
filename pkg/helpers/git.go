package helpers

import (
	"fmt"
	"net/url"
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
)

// MirrorRepo clones the source remote as a bare repo and pushes all refs
// (branches, tags, and then fixes the bare-repo HEAD) to the destination.
func MirrorRepo(sourceURL, destinationURL string, pullAuth, pushAuth transport.AuthMethod) error {
	// Clone source into a temp bare repo:
	tmpDir, err := os.MkdirTemp("", "bare-mirror-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

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

	// Add destination as a remote
	zap.L().Debug("Adding destination remote", zap.String("destinationURL", destinationURL))
	_, err = srcRepo.CreateRemote(&config.RemoteConfig{
		Name: "destination",
		URLs: []string{destinationURL},
	})
	if err != nil {
		return fmt.Errorf("failed to create remote for destination: %w", err)
	}

	// Push *all* refs up to it
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

	if err := srcRepo.Push(pushOpts); err != nil {
		return fmt.Errorf("failed to push to destination repository: %w", err)
	}

	// Finally, repair the bare-repo HEAD to point at the right branch
	if err := fixBareRepoHEAD(destinationURL, srcRepo); err != nil {
		return fmt.Errorf("failed to set destination HEAD: %w", err)
	}

	return nil
}

// fixBareRepoHEAD will open the bare repo on disk (via file:// URL),
// read the srcRepo’s HEAD symbolic name (e.g. refs/heads/main), and then
// rewrite the bare repo’s HEAD to point there.
// (This is necessary because bare repos do not have a working tree,
// so they cannot automatically determine the HEAD branch.)
func fixBareRepoHEAD(destinationURL string, srcRepo *git.Repository) error {
	u, err := url.Parse(destinationURL)
	if err != nil {
		return err
	}
	path := u.Path

	destRepo, err := git.PlainOpen(path)
	if err != nil {
		return err
	}

	// figure out what branch the source HEAD was on
	srcHead, err := srcRepo.Head()
	if err != nil {
		return err
	}

	// write a new symbolic HEAD in the bare repo
	zap.L().Debug("Setting HEAD in destination repository", zap.String("destinationURL", destinationURL), zap.String("branch", srcHead.Name().String()))
	sym := plumbing.NewSymbolicReference(plumbing.HEAD, srcHead.Name())
	return destRepo.Storer.SetReference(sym)
}

// BuildHTTPAuth creates an HTTP BasicAuth object using a username and token.
func BuildHTTPAuth(username string, token string) transport.AuthMethod {
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
