//go:build e2e

// Package e2e runs the gitlab-sync binary against fake GitLab instances (see the
// gitlabmock package) and checks what it does, through Gherkin scenarios run by
// godog. Run them with `make e2e`.
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cucumber/godog"
)

func TestMain(m *testing.M) {
	os.Exit(runSuite(m))
}

func runSuite(m *testing.M) int {
	buildDir, err := os.MkdirTemp("", "gitlab-sync-e2e-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create the build directory:", err)

		return 1
	}

	defer os.RemoveAll(buildDir) //nolint:errcheck // best effort

	coverDir = os.Getenv("E2E_COVERDIR")
	if coverDir == "" {
		coverDir = filepath.Join(buildDir, "cover")
	}

	if err = os.MkdirAll(coverDir, 0o750); err != nil {
		fmt.Fprintln(os.Stderr, "create the coverage directory:", err)

		return 1
	}

	binaryPath = filepath.Join(buildDir, "gitlab-sync")

	build := exec.Command("go", "build", "-cover", "-covermode=atomic", "-o", binaryPath, ".")
	build.Dir = filepath.Join("..", "..")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr

	if err = build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build gitlab-sync:", err)

		return 1
	}

	return m.Run()
}

// TestFeatures runs every scenario of features/, except the ones tagged @bug:
// they describe a behavior that is wrong today, see the comment above each.
// Run them on purpose with E2E_TAGS=@bug.
func TestFeatures(t *testing.T) {
	tags := os.Getenv("E2E_TAGS")
	if tags == "" {
		tags = "~@bug"
	}

	suite := godog.TestSuite{
		Name:                "gitlab-sync",
		ScenarioInitializer: initializeScenario,
		Options: &godog.Options{
			Format:      "pretty",
			Paths:       []string{"features"},
			Tags:        tags,
			Concurrency: 4,
			Strict:      true,
			TestingT:    t,
		},
	}

	if suite.Run() != 0 {
		t.Fatal("some scenarios failed")
	}
}
