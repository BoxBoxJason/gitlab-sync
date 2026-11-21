//go:build e2e

// Package e2e runs the gitlab-sync binary against a real GitLab instance
// (see test/e2e/gitlab), with a few scenarios that check what the fake GitLab of
// the other end-to-end tests can only approximate. They are skipped unless
// GITLAB_E2E_URL and GITLAB_E2E_TOKEN are set. Run them with `make e2e`.
package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

const (
	// runTimeout fails a run that hangs instead of blocking the CI.
	runTimeout = 5 * time.Minute

	roleSource      = "source"
	roleDestination = "destination"

	mappingFilePerm = 0o600
)

// binary is the gitlab-sync binary every scenario runs, built with coverage
// instrumentation.
type binary struct {
	path string
	// coverDir is where the instrumented binary writes its coverage data.
	coverDir string
}

// environment is what the scenarios share: the GitLab instance and the binary.
type environment struct {
	client *gitlab.Client
	bin    binary
	url    string
	token  string
}

// outcome is the result of one execution of the binary.
type outcome struct {
	stdout string
	stderr string
	exit   int
}

// world is the state of one scenario. The source and the destination are two
// top level groups of the same instance, named after the scenario so that
// scenarios never see each other's projects.
type world struct {
	branches    map[string][]string
	last        *outcome
	env         environment
	work        string
	mappingPath string
	suffix      string
	groupID     int64
	files       int
}

func newWorld(env environment) (*world, error) {
	work, err := os.MkdirTemp("", "gitlab-sync-real-*")
	if err != nil {
		return nil, fmt.Errorf("create the work directory: %w", err)
	}

	return &world{env: env, work: work, branches: make(map[string][]string), suffix: strconv.FormatInt(time.Now().UnixNano(), 36)}, nil
}

// close removes the scenario files. The groups are left on the instance, which
// is thrown away with its container.
func (w *world) close() error {
	err := os.RemoveAll(w.work)
	if err != nil {
		return fmt.Errorf("remove the scenario files: %w", err)
	}

	return nil
}

// groupPath is the top level group of a role.
func (w *world) groupPath(role string) string {
	if role == roleSource {
		return "src-" + w.suffix
	}

	return "dst-" + w.suffix
}

// projectPath is the full path of a project of a role.
func (w *world) projectPath(role, name string) string {
	return w.groupPath(role) + "/" + name
}

// expand replaces the placeholders a scenario can use in its mapping.
func (w *world) expand(text string) string {
	return strings.NewReplacer(
		"{source}", w.groupPath(roleSource),
		"{destination}", w.groupPath(roleDestination),
	).Replace(text)
}

// writeMapping stores the mirror mapping the next runs are given.
func (w *world) writeMapping(content string) error {
	w.mappingPath = filepath.Join(w.work, "mirror-mapping.json")

	err := os.WriteFile(w.mappingPath, []byte(w.expand(content)), mappingFilePerm)
	if err != nil {
		return fmt.Errorf("write the mirror mapping: %w", err)
	}

	return nil
}

// execute runs the binary against the instance (as source and destination),
// without any prompt possible, and records the outcome.
func (w *world) execute(extra []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	base := []string{
		"--source-url", w.env.url, "--source-token", w.env.token,
		"--destination-url", w.env.url, "--destination-token", w.env.token,
		"--mirror-mapping", w.mappingPath, "--no-prompt",
	}

	command := exec.CommandContext(ctx, w.env.bin.path, slices.Concat(base, extra)...) //nolint:gosec // the binary is the one the suite built
	command.Dir = w.work
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + w.work,
		"TMPDIR=" + w.work,
		"GOCOVERDIR=" + w.env.bin.coverDir,
	}

	var stdout, stderr bytes.Buffer

	command.Stdout, command.Stderr = &stdout, &stderr

	err := command.Run()
	result := &outcome{stdout: stdout.String(), stderr: stderr.String()}
	w.last = result

	var exitErr *exec.ExitError

	switch {
	case ctx.Err() != nil:
		return fmt.Errorf("gitlab-sync did not finish within %s%s", runTimeout, w.describe())
	case err == nil:
		return nil
	case errors.As(err, &exitErr):
		result.exit = exitErr.ExitCode()

		return nil
	default:
		return fmt.Errorf("start gitlab-sync: %w", err)
	}
}

// describe explains the last run, to append to the failures about it.
func (w *world) describe() string {
	if w.last == nil {
		return ""
	}

	return fmt.Sprintf("\nexit code: %d\nstdout:\n%s\nstderr:\n%s", w.last.exit, w.last.stdout, w.last.stderr)
}

// errNotRun is returned by the assertions about a run that has not happened.
var errNotRun = errors.New("gitlab-sync has not been run yet")
