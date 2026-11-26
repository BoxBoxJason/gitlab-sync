//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/cucumber/godog"
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

const (
	coverDirPerm = 0o750

	// defaultConcurrency is how many scenarios run at the same time. They are
	// independent: each one has its own groups, accounts and proxies.
	defaultConcurrency = 8

	// aloneTag marks the scenarios that run one at a time.
	aloneTag = "@alone"
)

// buildBinary builds gitlab-sync with coverage instrumentation. The coverage data
// of the runs lands in E2E_COVERDIR when it is set.
func buildBinary(t *testing.T) binary {
	t.Helper()

	buildDir := t.TempDir()

	coverDir := os.Getenv("E2E_COVERDIR")
	if coverDir == "" {
		coverDir = filepath.Join(buildDir, "cover")
	}

	err := os.MkdirAll(coverDir, coverDirPerm)
	if err != nil {
		t.Fatalf("create the coverage directory: %v", err)
	}

	built := binary{path: filepath.Join(buildDir, "gitlab-sync"), coverDir: coverDir}

	build := exec.CommandContext(t.Context(), "go", "build", "-cover", "-covermode=atomic", "-o", built.path, ".")
	build.Dir = filepath.Join("..", "..")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr

	err = build.Run()
	if err != nil {
		t.Fatalf("build gitlab-sync: %v", err)
	}

	return built
}

// TestFeatures runs the scenarios of features/ against the GitLab instance
// GITLAB_E2E_URL, as the user of the token GITLAB_E2E_TOKEN (an administrator of
// a throwaway instance: the scenarios create groups and projects in it).
func TestFeatures(t *testing.T) {
	t.Parallel()

	url, token := os.Getenv("GITLAB_E2E_URL"), os.Getenv("GITLAB_E2E_TOKEN")
	if url == "" || token == "" {
		t.Skip("set GITLAB_E2E_URL and GITLAB_E2E_TOKEN to run against a real GitLab (see test/e2e/gitlab)")
	}

	client, err := gitlab.NewClient(token, gitlab.WithBaseURL(url))
	if err != nil {
		t.Fatalf("create the GitLab client: %v", err)
	}

	// The scenarios tagged @bug describe a behavior that is wrong today, see the
	// comment above each. Run them on purpose with E2E_TAGS=@bug.
	tags := os.Getenv("E2E_TAGS")
	if tags == "" {
		tags = "~@bug"
	}

	concurrency := defaultConcurrency

	if value := os.Getenv("E2E_CONCURRENCY"); value != "" {
		concurrency, err = strconv.Atoi(value)
		if err != nil {
			t.Fatalf("parse E2E_CONCURRENCY: %v", err)
		}
	}

	root := createRootGroup(t, client)
	env := &environment{client: client, bin: buildBinary(t), url: url, token: token, root: root.FullPath, rootID: root.ID}

	// The scenarios tagged @alone create too many projects to share the instance:
	// the listings of the others would span several pages, which the deletions of
	// finished scenarios shift under their feet. They run after the others.
	run(t, env, tags+" && ~"+aloneTag, concurrency)
	run(t, env, tags+" && "+aloneTag, 1)
}

// createRootGroup creates the group holding the groups of the scenarios.
func createRootGroup(t *testing.T, client *gitlab.Client) *gitlab.Group {
	t.Helper()

	path := "e2e-" + strconv.FormatInt(time.Now().UnixNano(), 36)

	group, _, err := client.Groups.CreateGroup(&gitlab.CreateGroupOptions{Name: &path, Path: &path, Visibility: new(gitlab.PublicVisibility)})
	if err != nil {
		t.Fatalf("create the group %s: %v", path, err)
	}

	return group
}

// run runs the scenarios matching the tags.
func run(t *testing.T, env *environment, tags string, concurrency int) {
	t.Helper()

	suite := godog.TestSuite{
		Name:                "gitlab-sync",
		ScenarioInitializer: initializeScenario(env),
		Options: &godog.Options{
			Format:      "pretty",
			Paths:       []string{"features"},
			Tags:        tags,
			Strict:      true,
			TestingT:    t,
			Concurrency: concurrency,
		},
	}

	if suite.Run() != 0 {
		t.Error("some scenarios failed")
	}
}
