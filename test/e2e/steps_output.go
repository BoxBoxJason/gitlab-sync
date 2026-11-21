//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
)

func (s *steps) exitCode(want int) error {
	last, err := s.w.lastRun()
	if err != nil {
		return err
	}

	if last.exit != want {
		return fmt.Errorf("the exit code is %d, want %d%s", last.exit, want, s.w.describeLastRun())
	}

	return nil
}

func (s *steps) exitCodeNotZero() error {
	last, err := s.w.lastRun()
	if err != nil {
		return err
	}

	if last.exit == 0 {
		return fmt.Errorf("the exit code is 0, want a failure%s", s.w.describeLastRun())
	}

	return nil
}

func (s *steps) outputOf(stream string) (string, error) {
	last, err := s.w.lastRun()
	if err != nil {
		return "", err
	}

	switch stream {
	case "stdout":
		return last.stdout, nil
	case "stderr":
		return last.stderr, nil
	default:
		return last.stdout + last.stderr, nil
	}
}

func (s *steps) outputMatches(stream, pattern string) error {
	output, err := s.outputOf(stream)
	if err != nil {
		return err
	}

	matched, err := regexp.MatchString(pattern, output)
	if err != nil {
		return fmt.Errorf("bad pattern %q: %w", pattern, err)
	}

	if !matched {
		return fmt.Errorf("the %s does not match %q%s", stream, pattern, s.w.describeLastRun())
	}

	return nil
}

func (s *steps) outputDoesNotMatch(stream, pattern string) error {
	output, err := s.outputOf(stream)
	if err != nil {
		return err
	}

	matched, err := regexp.MatchString(pattern, output)
	if err != nil {
		return fmt.Errorf("bad pattern %q: %w", pattern, err)
	}

	if matched {
		return fmt.Errorf("the %s matches %q but should not%s", stream, pattern, s.w.describeLastRun())
	}

	return nil
}

// downloaded is how many bytes of git objects the source sent during run number
// n (counted from 1).
func (s *steps) downloaded(runNumber int) (int64, error) {
	if runNumber < 1 || runNumber > len(s.w.runs) {
		return 0, fmt.Errorf("there is no run %d, %d run(s) were made", runNumber, len(s.w.runs))
	}

	requests := s.w.source.Requests()
	window := requests[s.w.runs[runNumber-1].started[s.w.source]:]

	// A run's requests end where the next run's start.
	if runNumber < len(s.w.runs) {
		window = requests[s.w.runs[runNumber-1].started[s.w.source]:s.w.runs[runNumber].started[s.w.source]]
	}

	var total int64

	for _, request := range window {
		if request.Method == http.MethodPost && strings.HasSuffix(request.Path, "/git-upload-pack") {
			total += request.Bytes
		}
	}

	return total, nil
}

func (s *steps) downloadedLess(later, earlier int) error {
	laterBytes, err := s.downloaded(later)
	if err != nil {
		return err
	}

	earlierBytes, err := s.downloaded(earlier)
	if err != nil {
		return err
	}

	if laterBytes >= earlierBytes {
		return fmt.Errorf("run %d downloaded %d bytes from the source, run %d downloaded %d: want less", later, laterBytes, earlier, earlierBytes)
	}

	return nil
}

func (s *steps) downloadedSame(later, earlier int) error {
	laterBytes, err := s.downloaded(later)
	if err != nil {
		return err
	}

	earlierBytes, err := s.downloaded(earlier)
	if err != nil {
		return err
	}

	if laterBytes != earlierBytes {
		return fmt.Errorf("run %d downloaded %d bytes from the source, run %d downloaded %d: want the same", later, laterBytes, earlier, earlierBytes)
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
