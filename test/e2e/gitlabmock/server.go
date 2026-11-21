// Package gitlabmock is a stateful, in-memory fake GitLab used by the end-to-end
// tests. It serves the subset of the REST API gitlab-sync uses, plus the git
// smart-HTTP protocol over bare repositories kept in a temporary directory.
//
// The fake is strict: an endpoint it does not implement answers 501 and is
// remembered (see Server.Unimplemented), so a scenario fails loudly instead of
// passing against a silently wrong answer.
package gitlabmock

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// firstID is the first ID the instance hands out.
	firstID = 100

	// DefaultVersion is the GitLab version a new fake reports.
	DefaultVersion = "17.6.0"
	// DefaultUser is the user the authenticated token belongs to.
	DefaultUser = "mirror-bot"
	userID      = 1

	// maxPerPage is GitLab's cap on the page size.
	maxPerPage     = 100
	defaultPerPage = 20
)

// Request is one request the fake received.
type Request struct {
	Time    time.Time
	Header  http.Header
	Method  string
	Path    string // decoded path
	Query   string // raw query
	Body    string
	Status  int
	Bytes   int64 // size of the response body
	Faulted bool  // answered by an injected fault
}

// String renders the request as "METHOD /path?query".
func (r *Request) String() string {
	if r.Query != "" {
		return r.Method + " " + r.Path + "?" + r.Query
	}

	return r.Method + " " + r.Path
}

// IsWrite tells whether the request can change the state of a GitLab instance.
// A git-upload-pack POST only reads (it is how a clone fetches objects).
func (r *Request) IsWrite() bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}

	return !strings.HasSuffix(r.Path, "/git-upload-pack")
}

// fault is an injected failure.
type fault struct {
	method    string // empty matches any method
	pattern   *regexp.Regexp
	query     string // substring the raw query must contain, empty matches any
	status    int
	remaining int // -1 means unlimited
}

// Server is a fake GitLab instance.
type Server struct {
	http         *httptest.Server
	mux          *http.ServeMux
	files        map[string][]byte
	version      string
	publicBase   string
	plan         string
	dir          string
	validToken   string
	groups       []*group
	projects     []*project
	faults       []*fault
	journal      []Request
	unimplements []string
	pushOptions  []PushOption
	nextID       int64
	mu           sync.Mutex
	noLicense    bool
}

// New starts a fake GitLab. Close it when done.
func New() (*Server, error) {
	dir, err := os.MkdirTemp("", "gitlabmock-*")
	if err != nil {
		return nil, fmt.Errorf("create the repositories directory: %w", err)
	}

	server := &Server{
		dir:     dir,
		version: DefaultVersion,
		plan:    "",
		files:   make(map[string][]byte),
		nextID:  firstID,
	}

	server.mux = http.NewServeMux()
	server.registerRoutes()
	server.http = httptest.NewServer(http.HandlerFunc(server.serve))
	server.publicBase = server.http.URL

	return server, nil
}

// URL is the base URL of the instance, without /api/v4.
func (s *Server) URL() string { return s.http.URL }

// Close stops the server and removes its repositories.
func (s *Server) Close() error {
	s.http.Close()

	err := os.RemoveAll(s.dir)
	if err != nil {
		return fmt.Errorf("remove the repositories: %w", err)
	}

	return nil
}

// SetVersion sets the version the metadata endpoint reports. An unparsable
// version (for example "not-a-version") is allowed on purpose.
func (s *Server) SetVersion(version string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.version = version
}

// SetLicensePlan sets the plan the license endpoint reports ("premium",
// "ultimate"...). An empty plan makes the endpoint answer null, like an instance
// without a license does.
func (s *Server) SetLicensePlan(plan string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.plan = plan
}

// SetLicenseEndpointMissing makes the license endpoint answer 404, like a
// Community Edition does.
func (s *Server) SetLicenseEndpointMissing(missing bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.noLicense = missing
}

// RequireToken makes the instance reject (401) every request that does not carry
// token, as private token, bearer token or git basic auth password.
func (s *Server) RequireToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.validToken = token
}

// InjectFault makes the requests matching method (empty: any) and pathPattern
// answer status. pathPattern is matched against the decoded request path; "*"
// matches any run of characters, including slashes. times limits how many
// matching requests fail (0 or less means every one of them).
func (s *Server) InjectFault(method, pathPattern string, status, times int) {
	s.addFault(method, pathPattern, "", status, times)
}

// ForbidGit makes the git operation ("clone" or "push") on the project at
// projectPath answer 403. An empty projectPath means every project.
func (s *Server) ForbidGit(operation, projectPath string) error {
	pattern := "*"
	if projectPath != "" {
		pattern = "/" + strings.Trim(projectPath, "/") + ".git"
	}

	switch operation {
	case "clone":
		s.addFault(http.MethodGet, pattern+"/info/refs", "service=git-upload-pack", http.StatusForbidden, 0)
	case "push":
		s.addFault(http.MethodGet, pattern+"/info/refs", "service=git-receive-pack", http.StatusForbidden, 0)
	default:
		return fmt.Errorf("unknown git operation %q (want clone or push)", operation)
	}

	return nil
}

func globToRegexp(glob string) *regexp.Regexp {
	return regexp.MustCompile("^" + strings.ReplaceAll(regexp.QuoteMeta(glob), `\*`, ".*") + "$")
}

// Requests returns a copy of the request journal, oldest first.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]Request(nil), s.journal...)
}

// Matching returns the journal entries for method (empty: any) whose decoded
// path matches pathPattern ("*" matches anything, slashes included).
func (s *Server) Matching(method, pathPattern string) []Request {
	pattern := globToRegexp(pathPattern)

	var matched []Request

	for _, request := range s.Requests() {
		if (method == "" || strings.EqualFold(method, request.Method)) && pattern.MatchString(request.Path) {
			matched = append(matched, request)
		}
	}

	return matched
}

// WriteRequests returns the journal entries that can change the instance.
func (s *Server) WriteRequests() []Request {
	var writes []Request

	for _, request := range s.Requests() {
		if request.IsWrite() {
			writes = append(writes, request)
		}
	}

	return writes
}

// Unimplemented lists the "METHOD /path" of the requests the fake does not serve.
func (s *Server) Unimplemented() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.unimplements...)
}

// ResetJournal forgets the requests received so far, for scenarios that run the
// application several times and look at the last run only.
func (s *Server) ResetJournal() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.journal = nil
}

// AddFile serves content at the instance path (for example a release asset that
// is not a package file).
func (s *Server) AddFile(path string, content []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.files["/"+strings.TrimPrefix(path, "/")] = content
}
