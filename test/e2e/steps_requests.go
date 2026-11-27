//go:build e2e

package e2e

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/cucumber/godog"
)

func (s *steps) registerRequestSteps(scenario *godog.ScenarioContext) {
	scenario.Step(`^the `+role+` received no write request$`, s.noWriteRequest)
	scenario.Step(`^the `+role+` received a `+method+` request on "([^"]+)"$`, s.receivedRequest)
	scenario.Step(`^the `+role+` received a `+method+` request on "([^"]+)" with the query "([^"]*)"$`, s.receivedRequestWithQuery)
	scenario.Step(`^the `+role+` received no `+method+` request on "([^"]+)"$`, s.receivedNoRequest)
	scenario.Step(`^the `+role+` received (\d+) `+method+` requests? on "([^"]+)"$`, s.receivedRequestCount)
	scenario.Step(`^the `+role+` received no `+method+` request on "([^"]+)" with the body "(.*)"$`, s.receivedNoRequestWithBody)
	scenario.Step(`^the destination received the push option "([^"]+)"$`, s.pushOption)
	scenario.Step(`^the destination received no push option$`, s.noPushOption)
	scenario.Step(`^every request to the `+role+` carried the User-Agent "(.*)"$`, func(role, pattern string) error {
		return s.userAgent(role, pattern, "")
	})
	scenario.Step(`^every API request to the `+role+` carried the User-Agent "(.*)"$`, func(role, pattern string) error {
		return s.userAgent(role, pattern, "/api/")
	})
	scenario.Step(`^every request to the `+role+` was authenticated with the `+role+` token$`, s.authenticatedWith)
	scenario.Step(`^run (\d+) downloaded less from the source than run (\d+)$`, s.downloadedLess)
	scenario.Step(`^run (\d+) downloaded about as much from the source as run (\d+)$`, s.downloadedSame)
}

// requestsOfRun returns what a role received during run number n (counted from 1).
func (s *steps) requestsOfRun(role string, number int) ([]*request, error) {
	running, ok := s.w.proxies[role]
	if !ok {
		return nil, errNotObserved
	}

	if number < 1 || number > len(s.w.runs) {
		return nil, fmt.Errorf("there is no run %d, %d run(s) were made", number, len(s.w.runs))
	}

	journal := running.journal()
	end := len(journal)

	// A run's requests end where the next run's start.
	if number < len(s.w.runs) {
		end = s.w.runs[number].started[role]
	}

	return journal[s.w.runs[number-1].started[role]:end], nil
}

// requests returns what a role received during the last run.
func (s *steps) requests(role string) ([]*request, error) {
	return s.requestsOfRun(role, len(s.w.runs))
}

func (s *steps) matching(role, method, pattern string) ([]*request, error) {
	received, err := s.requests(role)
	if err != nil {
		return nil, err
	}

	pattern = s.w.expand(pattern)

	return slices.DeleteFunc(slices.Clone(received), func(candidate *request) bool {
		return candidate.method != method || !globMatches(pattern, candidate.path)
	}), nil
}

// listing lists what a role received during the last run, to explain a failure.
func (s *steps) listing(role string) string {
	received, err := s.requests(role)
	if err != nil {
		return ""
	}

	lines := []string{"\nrequests received by the " + role + ":"}

	for _, candidate := range received {
		lines = append(lines, "  "+candidate.String())
	}

	return strings.Join(lines, "\n")
}

func (s *steps) noWriteRequest(role string) error {
	received, err := s.requests(role)
	if err != nil {
		return err
	}

	var writes []string

	for _, candidate := range received {
		if candidate.isWrite() {
			writes = append(writes, candidate.String())
		}
	}

	if len(writes) > 0 {
		return fmt.Errorf("the %s received write requests: %s", role, strings.Join(writes, ", "))
	}

	return nil
}

func (s *steps) receivedRequest(role, method, pattern string) error {
	matched, err := s.matching(role, method, pattern)
	if err != nil {
		return err
	}

	if len(matched) == 0 {
		return fmt.Errorf("the %s received no %s request on %s%s", role, method, s.w.expand(pattern), s.listing(role))
	}

	return nil
}

func (s *steps) receivedRequestWithQuery(role, method, pattern, query string) error {
	matched, err := s.matching(role, method, pattern)
	if err != nil {
		return err
	}

	if slices.ContainsFunc(matched, func(candidate *request) bool { return strings.Contains(candidate.query, query) }) {
		return nil
	}

	return fmt.Errorf("the %s received no %s request on %s with the query %q%s", role, method, s.w.expand(pattern), query, s.listing(role))
}

func (s *steps) receivedNoRequest(role, method, pattern string) error {
	matched, err := s.matching(role, method, pattern)
	if err != nil {
		return err
	}

	if len(matched) > 0 {
		return fmt.Errorf("the %s received %d %s request(s) on %s, first one: %s", role, len(matched), method, s.w.expand(pattern), matched[0])
	}

	return nil
}

func (s *steps) receivedRequestCount(role string, count int, method, pattern string) error {
	matched, err := s.matching(role, method, pattern)
	if err != nil {
		return err
	}

	if len(matched) != count {
		return fmt.Errorf("the %s received %d %s request(s) on %s, want %d%s", role, len(matched), method, s.w.expand(pattern), count, s.listing(role))
	}

	return nil
}

func (s *steps) receivedNoRequestWithBody(role, method, pattern, body string) error {
	matched, err := s.matching(role, method, pattern)
	if err != nil {
		return err
	}

	expression, err := regexp.Compile(body)
	if err != nil {
		return fmt.Errorf("bad pattern %q: %w", body, err)
	}

	for _, candidate := range matched {
		if expression.Match(candidate.body) {
			return fmt.Errorf("the %s received %s with a body matching %q: %s", role, candidate, body, candidate.body)
		}
	}

	return nil
}
