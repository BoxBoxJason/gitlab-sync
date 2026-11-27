//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// execute runs the binary with args, without any prompt possible (stdin is
// /dev/null) and with a timeout, and records the outcome.
func (w *world) execute(args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	result := &outcome{args: args, started: make(map[string]int)}
	for role, running := range w.proxies {
		result.started[role] = len(running.journal())
	}

	command := exec.CommandContext(ctx, w.env.bin.path, args...) //nolint:gosec // the binary is the one the suite built
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

	for _, running := range w.proxies {
		running.drain()
	}

	result.stdout, result.stderr = stdout.String(), stderr.String()
	w.runs = append(w.runs, result)

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

// lastRun is the most recent run.
func (w *world) lastRun() (*outcome, error) {
	if len(w.runs) == 0 {
		return nil, errors.New("gitlab-sync has not been run yet")
	}

	return w.runs[len(w.runs)-1], nil
}

// describe explains the last run, to append to the failures about it.
func (w *world) describe() string {
	last, err := w.lastRun()
	if err != nil {
		return ""
	}

	return fmt.Sprintf("\ncommand: gitlab-sync %s\nexit code: %d\nstdout:\n%s\nstderr:\n%s", strings.Join(last.args, " "), last.exit, last.stdout, last.stderr)
}

// eventually retries a check until it passes or the instance had the time to settle.
func eventually(check func() error) error {
	deadline := time.Now().Add(settleTimeout)

	for {
		err := check()
		if err == nil || time.Now().After(deadline) {
			return err
		}

		time.Sleep(settleInterval)
	}
}
