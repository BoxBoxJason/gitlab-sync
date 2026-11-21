//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cucumber/godog"
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

const coverDirPerm = 0o750

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

	env := environment{client: client, bin: buildBinary(t), url: url, token: token}

	suite := godog.TestSuite{
		Name:                "gitlab-sync-real",
		ScenarioInitializer: initializeScenario(env),
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			Tags:     os.Getenv("E2E_TAGS"),
			Strict:   true,
			TestingT: t,
			// One scenario at a time: the instance is small.
			Concurrency: 1,
		},
	}

	if suite.Run() != 0 {
		t.Fatal("some scenarios failed")
	}
}
