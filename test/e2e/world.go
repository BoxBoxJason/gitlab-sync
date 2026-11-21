//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/boxboxjason/gitlab-sync/test/e2e/gitlabmock"
)

const (
	// runTimeout fails a run that hangs instead of blocking the CI.
	runTimeout = 60 * time.Second

	roleSource      = "source"
	roleDestination = "destination"

	sourceToken      = "source-token"
	destinationToken = "destination-token"

	flagSourceURL        = "--source-url"
	flagSourceToken      = "--source-token"
	flagDestinationURL   = "--destination-url"
	flagDestinationToken = "--destination-token"
	flagMirrorMapping    = "--mirror-mapping"
	flagNoPrompt         = "--no-prompt"

	mappingFilePerm = 0o600
)

// binary is the gitlab-sync binary every scenario runs, built once with
// coverage instrumentation.
type binary struct {
	path string
	// coverDir is where the instrumented binary writes its coverage data.
	coverDir string
}

// run is the outcome of one execution of the binary.
type run struct {
	at      time.Time
	started map[*gitlabmock.Server]int
	stdout  string
	stderr  string
	args    []string
	exit    int
}

// world is the state of one scenario: its fake instances, the files it wrote
// and the runs it made.
type world struct {
	bin         binary
	source      *gitlabmock.Server
	destination *gitlabmock.Server
	// unreachable holds, per role, a URL nothing listens on.
	unreachable map[string]string
	work        string
	mappingPath string
	runs        []*run
	counter     atomic.Int64
}

func newWorld(bin binary) (*world, error) {
	work, err := os.MkdirTemp("", "gitlab-sync-scenario-*")
	if err != nil {
		return nil, fmt.Errorf("create the work directory: %w", err)
	}

	source, err := gitlabmock.New()
	if err != nil {
		return nil, fmt.Errorf("start the source instance: %w", err)
	}

	destination, err := gitlabmock.New()
	if err != nil {
		closeErr := source.Close()

		return nil, errors.Join(fmt.Errorf("start the destination instance: %w", err), closeErr)
	}

	return &world{bin: bin, source: source, destination: destination, work: work, unreachable: make(map[string]string)}, nil
}

// close stops the fake instances and removes the scenario files.
func (w *world) close() error {
	var errs []error

	for _, server := range w.servers() {
		errs = append(errs, server.Close())
	}

	err := os.RemoveAll(w.work)
	if err != nil {
		errs = append(errs, fmt.Errorf("remove the scenario files: %w", err))
	}

	return errors.Join(errs...)
}

// servers lists the fake instances once each (source and destination can be the
// same one).
func (w *world) servers() []*gitlabmock.Server {
	if w.source == w.destination {
		return []*gitlabmock.Server{w.source}
	}

	return []*gitlabmock.Server{w.source, w.destination}
}

func (w *world) server(role string) *gitlabmock.Server {
	if role == roleSource {
		return w.source
	}

	return w.destination
}

// url is the URL the binary is given for a role.
func (w *world) url(role string) string {
	if url, ok := w.unreachable[role]; ok {
		return url
	}

	return w.server(role).URL()
}

// makeUnreachable gives a role a URL nothing listens on.
func (w *world) makeUnreachable(role string) error {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("reserve a port: %w", err)
	}

	w.unreachable[role] = "http://" + listener.Addr().String()

	err = listener.Close()
	if err != nil {
		return fmt.Errorf("release the port: %w", err)
	}

	return nil
}

// expand replaces the placeholders a scenario can use in its arguments.
func (w *world) expand(text string) string {
	return strings.NewReplacer(
		"{source}", w.url(roleSource),
		"{destination}", w.url(roleDestination),
		"{work}", w.work,
		"{cache}", filepath.Join(w.work, "cache"),
	).Replace(text)
}

// unique returns a number no other call of the scenario returned.
func (w *world) unique() int64 { return w.counter.Add(1) }

// lastRun is the most recent run.
func (w *world) lastRun() (*run, error) {
	if len(w.runs) == 0 {
		return nil, errors.New("gitlab-sync has not been run yet")
	}

	return w.runs[len(w.runs)-1], nil
}

// requests returns what a role received during the last run.
func (w *world) requests(role string) ([]gitlabmock.Request, error) {
	last, err := w.lastRun()
	if err != nil {
		return nil, err
	}

	server := w.server(role)

	return server.Requests()[last.started[server]:], nil
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

// execute runs the binary with args, without any prompt possible (stdin is
// /dev/null) and with a timeout, and records the outcome.
func (w *world) execute(args []string) error {
	started := make(map[*gitlabmock.Server]int)
	for _, server := range w.servers() {
		started[server] = len(server.Requests())
	}

	began := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, w.bin.path, args...) //nolint:gosec // the binary is the one the suite built
	command.Dir = w.work
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + w.work,
		"TMPDIR=" + w.work,
		"GOCOVERDIR=" + w.bin.coverDir,
	}

	var stdout, stderr bytes.Buffer

	command.Stdout, command.Stderr = &stdout, &stderr

	err := command.Run()

	result := &run{args: args, stdout: stdout.String(), stderr: stderr.String(), started: started, at: began}
	w.runs = append(w.runs, result)

	if ctx.Err() != nil {
		return fmt.Errorf("gitlab-sync did not finish within %s\nstdout:\n%s\nstderr:\n%s", runTimeout, result.stdout, result.stderr)
	}

	var exitErr *exec.ExitError

	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		result.exit = exitErr.ExitCode()
	default:
		return fmt.Errorf("start gitlab-sync: %w", err)
	}

	return nil
}

// describeLastRun is appended to the failures about a run, to explain them.
func (w *world) describeLastRun() string {
	last, err := w.lastRun()
	if err != nil {
		return ""
	}

	return fmt.Sprintf("\ncommand: gitlab-sync %s\nexit code: %d\nstdout:\n%s\nstderr:\n%s", strings.Join(last.args, " "), last.exit, last.stdout, last.stderr)
}
