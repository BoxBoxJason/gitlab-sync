package helpers

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

func TestBuildHTTPAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		username, token  string
		wantUser, wantPW string
	}{
		{"both provided", "alice", "secr3t", "alice", "secr3t"},
		{"empty username", "", "tk", DefaultGitUser, "tk"},
		{"spaces username", "   ", "x", DefaultGitUser, "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			auth := BuildHTTPAuth(tt.username, tt.token)
			basic, ok := auth.(*http.BasicAuth)
			if !ok {
				t.Fatalf("BuildHTTPAuth returned non-BasicAuth: %T", auth)
			}
			if basic.Username != tt.wantUser {
				t.Errorf("Username = %q; want %q", basic.Username, tt.wantUser)
			}
			if basic.Password != tt.wantPW {
				t.Errorf("Password = %q; want %q", basic.Password, tt.wantPW)
			}
		})
	}
}

func TestMirrorPushOptionsGitOptions(t *testing.T) {
	t.Parallel()

	if got := (MirrorPushOptions{}).gitOptions(); got != nil {
		t.Errorf("gitOptions() without SkipPipelines = %v; want nil", got)
	}

	// Every option GitLab offers to keep a push from running CI: ci.skip for every
	// version, ci.no_pipeline so that no "skipped" pipeline is recorded where it
	// is supported, and integrations.skip_ci for Jenkins-like integrations.
	// The names are spelled out rather than taken from the constants, so that a
	// typo in a constant fails here.
	want := map[string]string{
		"ci.skip":              "",
		"ci.no_pipeline":       "",
		"integrations.skip_ci": "",
	}

	if got := (MirrorPushOptions{SkipPipelines: true}).gitOptions(); !maps.Equal(got, want) {
		t.Errorf("gitOptions() with SkipPipelines = %v; want %v", got, want)
	}
}

// TestMirrorRepoPushOptions checks the push options a destination actually
// receives, through a pre-receive hook recording them. The file transport runs
// the system git-receive-pack, which only accepts push options once
// receive.advertisePushOptions is set, as GitLab does.
func TestMirrorRepoPushOptions(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the recording hook is a shell script")
	}

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	// The options arrive in no particular order (go-git iterates a map), so the
	// expected lists are sorted like the received ones.
	tests := []struct {
		name          string
		wantOptions   []string
		skipPipelines bool
	}{
		{
			name:          "CI skipping options sent when skipping pipelines",
			skipPipelines: true,
			wantOptions:   []string{"ci.no_pipeline=", "ci.skip=", "integrations.skip_ci="},
		},
		{
			name:          "no option sent when triggering pipelines",
			skipPipelines: false,
			wantOptions:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srcDir := filepath.Join(t.TempDir(), "srcrepo.git")
			seedBareRepo(t, srcDir)

			destDir := filepath.Join(t.TempDir(), "destrepo.git")
			recordFile := filepath.Join(t.TempDir(), "push-options")
			initRecordingDestination(t, destDir, recordFile)

			pushOptions := MirrorPushOptions{SkipPipelines: tt.skipPipelines}
			if err := MirrorRepo(nil, fileScheme+srcDir, fileScheme+destDir, nil, nil, pushOptions); err != nil {
				t.Fatalf("MirrorRepo failed: %v", err)
			}

			recorded, err := os.ReadFile(recordFile)
			if err != nil {
				t.Fatalf("the pre-receive hook did not run: %v", err)
			}

			gotOptions := strings.Fields(string(recorded))
			slices.Sort(gotOptions)

			if !slices.Equal(gotOptions, tt.wantOptions) {
				t.Errorf("push options received = %q; want %q", gotOptions, tt.wantOptions)
			}
		})
	}
}

// initRecordingDestination creates a bare repository at path that advertises
// push options and writes the ones each push carries to recordFile, one per line.
func initRecordingDestination(t *testing.T, path, recordFile string) {
	t.Helper()

	repo, err := git.PlainInit(path, true)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := repo.Config()
	if err != nil {
		t.Fatal(err)
	}

	hooksDir := filepath.Join(path, "hooks")

	cfg.Raw.Section("receive").SetOption("advertisePushOptions", "true")
	// Pin the hooks directory locally, so a core.hooksPath set in the global or
	// system git configuration cannot make git-receive-pack skip the hook.
	cfg.Raw.Section("core").SetOption("hooksPath", hooksDir)

	if err = repo.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}

	// The record file path is single-quoted in the script: escape any quote it holds.
	quotedRecordFile := "'" + strings.ReplaceAll(recordFile, "'", `'\''`) + "'"

	hook := `#!/bin/sh
: > ` + quotedRecordFile + `
i=0
while [ "$i" -lt "${GIT_PUSH_OPTION_COUNT:-0}" ]; do
	eval "printf '%s\n' \"\$GIT_PUSH_OPTION_$i\"" >> ` + quotedRecordFile + `
	i=$((i + 1))
done
`

	if err = os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err = os.WriteFile(filepath.Join(hooksDir, "pre-receive"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
}

// seedBareRepo creates a bare repository at path holding a single commit on the
// repository's initial branch.
func seedBareRepo(t *testing.T, path string) {
	t.Helper()

	worktreeDir := t.TempDir()

	repo, err := git.PlainInit(worktreeDir, false)
	if err != nil {
		t.Fatalf("failed to init source worktree: %v", err)
	}

	if err = os.WriteFile(filepath.Join(worktreeDir, "file.txt"), []byte("content"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatalf("failed to open source worktree: %v", err)
	}

	if _, err = worktree.Add("file.txt"); err != nil {
		t.Fatalf("failed to stage source file: %v", err)
	}

	_, err = worktree.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{Name: "tester", Email: "tester@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("failed to commit in source repository: %v", err)
	}

	if _, err = git.PlainClone(path, true, &git.CloneOptions{URL: fileScheme + worktreeDir, Mirror: true}); err != nil {
		t.Fatalf("failed to create bare source repository: %v", err)
	}
}
