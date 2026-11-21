//go:build e2e

// Package e2e runs the gitlab-sync binary against fake GitLab instances (see the
// gitlabmock package) and checks what it does, through Gherkin scenarios run by
// godog. Run them with `make e2e`.
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cucumber/godog"
)

const (
	coverDirPerm = 0o750

	// concurrency is how many scenarios run at the same time.
	concurrency = 4
)

// buildBinary builds gitlab-sync with coverage instrumentation, once for all the
// scenarios. The coverage data of the runs lands in E2E_COVERDIR when it is set.
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

// TestFeatures runs every scenario of features/, except the ones tagged @bug:
// they describe a behavior that is wrong today, see the comment above each.
// Run them on purpose with E2E_TAGS=@bug.
func TestFeatures(t *testing.T) {
	t.Parallel()

	tags := os.Getenv("E2E_TAGS")
	if tags == "" {
		tags = "~@bug"
	}

	suite := godog.TestSuite{
		Name:                "gitlab-sync",
		ScenarioInitializer: initializeScenario(buildBinary(t)),
		Options: &godog.Options{
			Format:      "pretty",
			Paths:       []string{"features"},
			Tags:        tags,
			Concurrency: concurrency,
			Strict:      true,
			TestingT:    t,
		},
	}

	if suite.Run() != 0 {
		t.Fatal("some scenarios failed")
	}
}
