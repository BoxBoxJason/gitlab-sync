//go:build e2e

package e2e

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/boxboxjason/gitlab-sync/test/e2e/gitlabmock"
)

func (s *steps) matching(role, method, pattern string) ([]gitlabmock.Request, error) {
	requests, err := s.w.requests(role)
	if err != nil {
		return nil, err
	}

	var matched []gitlabmock.Request

	for _, request := range requests {
		if request.Method == method && globMatches(pattern, request.Path) {
			matched = append(matched, request)
		}
	}

	return matched, nil
}

// globMatches matches path against a pattern where "*" is any run of characters.
func globMatches(pattern, path string) bool {
	return regexp.MustCompile("^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$").MatchString(path)
}

func (s *steps) noWriteRequest(role string) error {
	requests, err := s.w.requests(role)
	if err != nil {
		return err
	}

	var writes []string

	for _, request := range requests {
		if request.IsWrite() {
			writes = append(writes, request.String())
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
		return fmt.Errorf("the %s received no %s request on %s%s", role, method, pattern, s.requestsOf(role))
	}

	return nil
}

func (s *steps) receivedRequestWithQuery(role, method, pattern, query string) error {
	matched, err := s.matching(role, method, pattern)
	if err != nil {
		return err
	}

	for _, request := range matched {
		if strings.Contains(request.Query, query) {
			return nil
		}
	}

	return fmt.Errorf("the %s received no %s request on %s with the query %q%s", role, method, pattern, query, s.requestsOf(role))
}

func (s *steps) receivedNoRequest(role, method, pattern string) error {
	matched, err := s.matching(role, method, pattern)
	if err != nil {
		return err
	}

	if len(matched) > 0 {
		return fmt.Errorf("the %s received %d %s request(s) on %s, first one: %s", role, len(matched), method, pattern, matched[0].String())
	}

	return nil
}

func (s *steps) receivedRequestCount(role string, count int, method, pattern string) error {
	matched, err := s.matching(role, method, pattern)
	if err != nil {
		return err
	}

	if len(matched) != count {
		return fmt.Errorf("the %s received %d %s request(s) on %s, want %d%s", role, len(matched), method, pattern, count, s.requestsOf(role))
	}

	return nil
}

// userAgent checks the User-Agent of the requests of the last run whose path
// starts with pathPrefix (all of them when it is empty).
func (s *steps) userAgent(role, pattern, pathPrefix string) error {
	all, err := s.w.requests(role)
	if err != nil {
		return err
	}

	var requests []gitlabmock.Request

	for _, request := range all {
		if strings.HasPrefix(request.Path, pathPrefix) {
			requests = append(requests, request)
		}
	}

	if len(requests) == 0 {
		return fmt.Errorf("the %s received no request under %q", role, pathPrefix)
	}

	expression, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("bad pattern %q: %w", pattern, err)
	}

	for _, request := range requests {
		if agent := request.Header.Get("User-Agent"); !expression.MatchString(agent) {
			return fmt.Errorf("%s carried the User-Agent %q, which does not match %q", request.String(), agent, pattern)
		}
	}

	return nil
}

// requestsOf lists what a role received during the last run, to explain a failure.
func (s *steps) requestsOf(role string) string {
	requests, err := s.w.requests(role)
	if err != nil {
		return ""
	}

	listing := []string{"\nrequests received by the " + role + ":"}

	for _, request := range requests {
		listing = append(listing, fmt.Sprintf("\n  %s -> %d", request.String(), request.Status))
	}

	return strings.Join(listing, "")
}

func (s *steps) matchingBody(role, method, pattern, body string) ([]gitlabmock.Request, error) {
	matched, err := s.matching(role, method, pattern)
	if err != nil {
		return nil, err
	}

	expression, err := regexp.Compile(body)
	if err != nil {
		return nil, fmt.Errorf("bad pattern %q: %w", body, err)
	}

	var withBody []gitlabmock.Request

	for _, request := range matched {
		if expression.MatchString(request.Body) {
			withBody = append(withBody, request)
		}
	}

	return withBody, nil
}

func (s *steps) receivedRequestWithBody(role, method, pattern, body string) error {
	matched, err := s.matchingBody(role, method, pattern, body)
	if err != nil {
		return err
	}

	if len(matched) == 0 {
		return fmt.Errorf("the %s received no %s request on %s with a body matching %q%s", role, method, pattern, body, s.requestsOf(role))
	}

	return nil
}

func (s *steps) receivedNoRequestWithBody(role, method, pattern, body string) error {
	matched, err := s.matchingBody(role, method, pattern, body)
	if err != nil {
		return err
	}

	if len(matched) > 0 {
		return fmt.Errorf("the %s received %s with a body matching %q: %s", role, matched[0].String(), body, matched[0].Body)
	}

	return nil
}

// pushOptionsOfLastRun returns the git push options the destination received
// during the last run.
func (s *steps) pushOptionsOfLastRun() ([]string, error) {
	last, err := s.w.lastRun()
	if err != nil {
		return nil, err
	}

	var options []string

	for _, option := range s.w.destination.PushOptions() {
		if !option.Time.Before(last.at) {
			options = append(options, option.Option)
		}
	}

	return options, nil
}

func (s *steps) pushOption(want string) error {
	options, err := s.pushOptionsOfLastRun()
	if err != nil {
		return err
	}

	if !slices.Contains(options, want) {
		return fmt.Errorf("the destination received the push options %v, want %q among them", options, want)
	}

	return nil
}

func (s *steps) noPushOption() error {
	options, err := s.pushOptionsOfLastRun()
	if err != nil {
		return err
	}

	if len(options) > 0 {
		return fmt.Errorf("the destination received the push options %v, want none", options)
	}

	return nil
}
