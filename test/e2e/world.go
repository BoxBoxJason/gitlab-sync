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
)

// binaryPath is the gitlab-sync binary every scenario runs, built once by
// TestMain with coverage instrumentation.
var binaryPath string

// coverDir is where the instrumented binary writes its coverage data.
var coverDir string

// defaultFlags are the flags every run starts from, in this order. A scenario
// overrides one by passing the same flag, or drops it with `without`.
var defaultFlags = []string{"--source-url", "--source-token", "--destination-url", "--destination-token", "--mirror-mapping", "--no-prompt"}

// flagAliases maps the short flags to the long name used to tell which default
// flag a scenario overrides.
var flagAliases = map[string]string{"-n": "--no-prompt"}

// run is the outcome of one execution of the binary.
type run struct {
	// started is, per fake instance, how many requests the instance had
	// received before the run, so that assertions only look at this run.
	started map[*gitlabmock.Server]int
	at      time.Time
	args    []string
	stdout  string
	stderr  string
	exit    int
}

// world is the state of one scenario: its fake instances, the files it wrote
// and the runs it made.
type world struct {
	source      *gitlabmock.Server
	destination *gitlabmock.Server
	// unreachable holds, per role, a URL nothing listens on.
	unreachable map[string]string
	work        string
	mappingPath string
	runs        []*run
	counter     atomic.Int64
}

func newWorld() (*world, error) {
	work, err := os.MkdirTemp("", "gitlab-sync-scenario-*")
	if err != nil {
		return nil, fmt.Errorf("create the work directory: %w", err)
	}

	source, err := gitlabmock.New()
	if err != nil {
		return nil, err
	}

	destination, err := gitlabmock.New()
	if err != nil {
		source.Close()

		return nil, err
	}

	return &world{source: source, destination: destination, work: work, unreachable: make(map[string]string)}, nil
}

// close stops the fake instances and removes the scenario files.
func (w *world) close() {
	for _, server := range w.servers() {
		server.Close()
	}

	_ = os.RemoveAll(w.work)
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
		return err
	}

	w.unreachable[role] = "http://" + listener.Addr().String()

	return listener.Close()
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

	return os.WriteFile(w.mappingPath, []byte(w.expand(content)), 0o600)
}

// flagsFor builds the command line of a run: the default flags (minus the
// overridden and the dropped ones), then the extra flags.
func (w *world) flagsFor(extra, without []string) []string {
	skipped := make(map[string]bool)

	for _, name := range append(append([]string(nil), without...), flagNames(extra)...) {
		skipped[canonicalFlag(name)] = true
	}

	values := map[string]string{
		"--source-url":        w.url(roleSource),
		"--source-token":      sourceToken,
		"--destination-url":   w.url(roleDestination),
		"--destination-token": destinationToken,
		"--mirror-mapping":    w.mappingPath,
	}

	var args []string

	for _, name := range defaultFlags {
		if skipped[name] {
			continue
		}

		if name == "--no-prompt" {
			args = append(args, name)

			continue
		}

		if values[name] != "" {
			args = append(args, name, values[name])
		}
	}

	return append(args, extra...)
}

func canonicalFlag(name string) string {
	if long, ok := flagAliases[name]; ok {
		return long
	}

	return name
}

// flagNames lists the flags (not their values) among args.
func flagNames(args []string) []string {
	var names []string

	for _, arg := range args {
		if name, _, _ := strings.Cut(arg, "="); strings.HasPrefix(name, "-") {
			names = append(names, name)
		}
	}

	return names
}

// splitArgs splits a command line the way a shell would for plain words and
// double quoted strings.
func splitArgs(line string) []string {
	var (
		args    []string
		current strings.Builder
		quoted  bool
		started bool
	)

	for _, char := range line {
		switch {
		case char == '"':
			quoted, started = !quoted, true
		case (char == ' ' || char == '\t') && !quoted:
			if started {
				args = append(args, current.String())
				current.Reset()

				started = false
			}
		default:
			current.WriteRune(char)

			started = true
		}
	}

	if started {
		args = append(args, current.String())
	}

	return args
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

	command := exec.CommandContext(ctx, binaryPath, args...) //nolint:gosec // the binary is the one TestMain built
	command.Dir = w.work
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + w.work,
		"TMPDIR=" + w.work,
		"GOCOVERDIR=" + coverDir,
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
