package cmd

import (
	"strings"
	"testing"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

func TestDefaultUserAgent(t *testing.T) {
	t.Parallel()

	got := defaultUserAgent()

	if want := binaryName + "/" + resolveVersion(); got != want {
		t.Errorf("expected %q, got %q", want, got)
	}

	if strings.ContainsAny(got, " \t\r\n") {
		t.Errorf("expected a single User-Agent product token, got %q", got)
	}
}

func TestUserAgentFlag(t *testing.T) {
	t.Run("defaults to the program version", func(t *testing.T) {
		t.Setenv("GITLAB_SYNC_USER_AGENT", "")

		var args mirrorcfg.ParserArgs

		var mirrorMappingPath, logFile string

		buildRootCmd(&args, &mirrorMappingPath, &logFile)

		if args.UserAgent != defaultUserAgent() {
			t.Errorf("expected %q, got %q", defaultUserAgent(), args.UserAgent)
		}
	})

	t.Run("environment overrides the default", func(t *testing.T) {
		t.Setenv("GITLAB_SYNC_USER_AGENT", "  my-sync-job/1.0  ")

		var args mirrorcfg.ParserArgs

		var mirrorMappingPath, logFile string

		buildRootCmd(&args, &mirrorMappingPath, &logFile)

		if args.UserAgent != "my-sync-job/1.0" {
			t.Errorf("expected %q, got %q", "my-sync-job/1.0", args.UserAgent)
		}
	})

	t.Run("flag overrides the environment", func(t *testing.T) {
		t.Setenv("GITLAB_SYNC_USER_AGENT", "my-sync-job/1.0")

		var args mirrorcfg.ParserArgs

		var mirrorMappingPath, logFile string

		rootCmd := buildRootCmd(&args, &mirrorMappingPath, &logFile)
		if err := rootCmd.ParseFlags([]string{"--user-agent", "nightly/2.0"}); err != nil {
			t.Fatal(err)
		}

		if args.UserAgent != "nightly/2.0" {
			t.Errorf("expected %q, got %q", "nightly/2.0", args.UserAgent)
		}
	})
}

func TestResolveExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                     string
		code                     int
		exitSuccessOnNonBlocking bool
		want                     int
	}{
		{"ok", helpers.ExitOK, false, helpers.ExitOK},
		{"ok with override", helpers.ExitOK, true, helpers.ExitOK},
		{"blocking", helpers.ExitBlocking, false, helpers.ExitBlocking},
		{"blocking with override", helpers.ExitBlocking, true, helpers.ExitBlocking},
		{"non-blocking", helpers.ExitNonBlocking, false, helpers.ExitNonBlocking},
		{"non-blocking with override", helpers.ExitNonBlocking, true, helpers.ExitOK},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := resolveExitCode(test.code, &mirrorcfg.ParserArgs{ExitSuccessOnNonBlocking: test.exitSuccessOnNonBlocking}); got != test.want {
				t.Errorf("resolveExitCode(%d, %v) = %d, want %d", test.code, test.exitSuccessOnNonBlocking, got, test.want)
			}
		})
	}
}
