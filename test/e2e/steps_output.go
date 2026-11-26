//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"regexp"

	"github.com/cucumber/godog"
)

func (s *steps) registerOutputSteps(scenario *godog.ScenarioContext) {
	scenario.Step(`^the exit code is (\d+)$`, s.exitCode)
	scenario.Step(`^the exit code is not 0$`, s.exitCodeNotZero)
	scenario.Step(`^the (output|stdout|stderr) matches "(.*)"$`, s.outputMatches)
	scenario.Step(`^the (output|stdout|stderr) does not match "(.*)"$`, s.outputDoesNotMatch)
	scenario.Step(`^the file "([^"]+)" exists$`, s.fileExists)
	scenario.Step(`^the file "([^"]+)" matches "(.*)"$`, s.fileMatches)
	scenario.Step(`^"([^"]+)" does not exist$`, s.pathMissing)
}

func (s *steps) exitCode(want int) error {
	last, err := s.w.lastRun()
	if err != nil {
		return err
	}

	if last.exit != want {
		return fmt.Errorf("the exit code is %d, want %d%s", last.exit, want, s.w.describe())
	}

	return nil
}

func (s *steps) exitCodeNotZero() error {
	last, err := s.w.lastRun()
	if err != nil {
		return err
	}

	if last.exit == 0 {
		return fmt.Errorf("the exit code is 0, want a failure%s", s.w.describe())
	}

	return nil
}

// match tells whether a stream of the last run matches a pattern.
func (s *steps) match(stream, pattern string) (bool, error) {
	last, err := s.w.lastRun()
	if err != nil {
		return false, err
	}

	var output string

	switch stream {
	case "stdout":
		output = last.stdout
	case "stderr":
		output = last.stderr
	default:
		output = last.stdout + last.stderr
	}

	matched, err := regexp.MatchString(s.w.expand(pattern), output)
	if err != nil {
		return false, fmt.Errorf("bad pattern %q: %w", pattern, err)
	}

	return matched, nil
}

func (s *steps) outputMatches(stream, pattern string) error {
	matched, err := s.match(stream, pattern)
	if err != nil {
		return err
	}

	if !matched {
		return fmt.Errorf("the %s does not match %q%s", stream, s.w.expand(pattern), s.w.describe())
	}

	return nil
}

func (s *steps) outputDoesNotMatch(stream, pattern string) error {
	matched, err := s.match(stream, pattern)
	if err != nil {
		return err
	}

	if matched {
		return fmt.Errorf("the %s matches %q but should not%s", stream, s.w.expand(pattern), s.w.describe())
	}

	return nil
}

func (s *steps) fileExists(path string) error {
	_, err := os.Stat(s.w.expand(path))
	if err != nil {
		return fmt.Errorf("the file %s does not exist: %w", path, err)
	}

	return nil
}

func (s *steps) fileMatches(path, pattern string) error {
	content, err := os.ReadFile(s.w.expand(path))
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	matched, err := regexp.Match(pattern, content)
	if err != nil {
		return fmt.Errorf("bad pattern %q: %w", pattern, err)
	}

	if !matched {
		return fmt.Errorf("the file %s does not match %q, it holds:\n%s", path, pattern, content)
	}

	return nil
}

func (s *steps) pathMissing(path string) error {
	_, err := os.Stat(s.w.expand(path))
	if err == nil {
		return fmt.Errorf("%s exists", path)
	}

	return nil
}
