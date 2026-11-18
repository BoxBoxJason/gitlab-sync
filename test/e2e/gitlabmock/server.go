// Package gitlabmock is a stateful, in-memory fake GitLab used by the end-to-end
// tests. It serves the subset of the REST API gitlab-sync uses, plus the git
// smart-HTTP protocol over bare repositories kept in a temporary directory.
//
// The fake is strict: an endpoint it does not implement answers 501 and is
// remembered (see Server.Unimplemented), so a scenario fails loudly instead of
// passing against a silently wrong answer.
package gitlabmock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

const (
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
func (r Request) String() string {
	if r.Query != "" {
		return r.Method + " " + r.Path + "?" + r.Query
	}

	return r.Method + " " + r.Path
}

// IsWrite tells whether the request can change the state of a GitLab instance.
// A git-upload-pack POST only reads (it is how a clone fetches objects).
func (r Request) IsWrite() bool {
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
	http *httptest.Server
	mux  *http.ServeMux
	dir  string

	mu sync.Mutex

	version      string
	plan         string // license plan, "" answers null like an unlicensed instance
	noLicense    bool   // the license endpoint answers 404 like a Community Edition
	validToken   string // when set, any other token is rejected with 401
	publicBase   string
	nextID       int64
	groups       []*group
	projects     []*project
	files        map[string][]byte
	faults       []*fault
	journal      []Request
	unimplements []string
	pushOptions  []PushOption
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
		nextID:  100,
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
func (s *Server) Close() {
	s.http.Close()

	_ = os.RemoveAll(s.dir)
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
	s.injectFault(method, pathPattern, "", status, times)
}

func (s *Server) injectFault(method, pathPattern, query string, status, times int) {
	if times <= 0 {
		times = -1
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.faults = append(s.faults, &fault{
		method:    strings.ToUpper(method),
		pattern:   globToRegexp(pathPattern),
		query:     query,
		status:    status,
		remaining: times,
	})
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
		s.injectFault(http.MethodGet, pattern+"/info/refs", "service=git-upload-pack", http.StatusForbidden, 0)
	case "push":
		s.injectFault(http.MethodGet, pattern+"/info/refs", "service=git-receive-pack", http.StatusForbidden, 0)
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

// statusRecorder keeps the status code a handler wrote.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}

	r.bytes += int64(len(data))

	return r.ResponseWriter.Write(data)
}

// serve journals the request, then applies the token check, the injected faults
// and finally the routes.
func (s *Server) serve(writer http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	request.Body = io.NopCloser(bytes.NewReader(body))

	entry := Request{
		Time:   time.Now(),
		Header: request.Header.Clone(),
		Method: request.Method,
		Path:   request.URL.Path,
		Query:  request.URL.RawQuery,
		Body:   journalBody(request, body),
	}

	recorder := &statusRecorder{ResponseWriter: writer}

	s.dispatch(recorder, request, &entry)

	entry.Status = recorder.status
	entry.Bytes = recorder.bytes

	s.mu.Lock()
	s.journal = append(s.journal, entry)
	s.mu.Unlock()
}

// journalBody keeps the body of a request for the journal. Pack data of a git
// push is binary and may be big, so only its size is recorded.
func journalBody(request *http.Request, body []byte) string {
	if strings.Contains(request.Header.Get("Content-Type"), "git-") {
		return fmt.Sprintf("<%d bytes of git protocol>", len(body))
	}

	return string(body)
}

func (s *Server) dispatch(writer http.ResponseWriter, request *http.Request, entry *Request) {
	if status, ok := s.matchFault(request); ok {
		entry.Faulted = true

		if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
			writer.Header().Set("Retry-After", "0")
		}

		writeError(writer, status, "injected fault")

		return
	}

	if !s.tokenAccepted(request) {
		if strings.HasSuffix(request.URL.Path, "/info/refs") || strings.Contains(request.URL.Path, "/git-") {
			writer.Header().Set("WWW-Authenticate", `Basic realm="GitLab"`)
		}

		writeError(writer, http.StatusUnauthorized, "401 Unauthorized")

		return
	}

	if content, ok := s.file(request.URL.Path); ok && request.Method == http.MethodGet {
		_, _ = writer.Write(content)

		return
	}

	if _, pattern := s.mux.Handler(request); pattern == "" {
		s.notImplemented(writer, request)

		return
	}

	// ServeHTTP, not the handler returned above: only it fills in the path values.
	s.mux.ServeHTTP(writer, request)
}

func (s *Server) file(path string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	content, ok := s.files[path]

	return content, ok
}

func (s *Server) notImplemented(writer http.ResponseWriter, request *http.Request) {
	description := request.Method + " " + request.URL.Path

	s.mu.Lock()
	s.unimplements = append(s.unimplements, description)
	s.mu.Unlock()

	writeError(writer, http.StatusNotImplemented, "gitlabmock does not implement "+description)
}

func (s *Server) matchFault(request *http.Request) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, candidate := range s.faults {
		if candidate.remaining == 0 {
			continue
		}

		if candidate.method != "" && candidate.method != request.Method {
			continue
		}

		if !candidate.pattern.MatchString(request.URL.Path) || !strings.Contains(request.URL.RawQuery, candidate.query) {
			continue
		}

		if candidate.remaining > 0 {
			candidate.remaining--
		}

		return candidate.status, true
	}

	return 0, false
}

// tokenAccepted applies RequireToken.
func (s *Server) tokenAccepted(request *http.Request) bool {
	s.mu.Lock()
	required := s.validToken
	s.mu.Unlock()

	if required == "" {
		return true
	}

	return requestToken(request) == required
}

func requestToken(request *http.Request) string {
	if token := request.Header.Get(gitlab.AccessTokenHeaderName); token != "" {
		return token
	}

	if token, found := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer "); found {
		return token
	}

	if _, password, ok := request.BasicAuth(); ok {
		return password
	}

	return ""
}

// id hands out a new identifier. The caller holds the lock.
func (s *Server) id() int64 {
	s.nextID++

	return s.nextID
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)

		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)

	_, _ = writer.Write(append(body, '\n'))
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"message": message})
}

// paginate slices a collection of total items the way GitLab does and sets the
// pagination headers. It returns the half-open range of items to send.
func paginate(writer http.ResponseWriter, request *http.Request, total int) (int, int) {
	perPage := atoiDefault(request.URL.Query().Get("per_page"), defaultPerPage)
	if perPage < 1 || perPage > maxPerPage {
		perPage = maxPerPage
	}

	page := max(atoiDefault(request.URL.Query().Get("page"), 1), 1)
	totalPages := max((total+perPage-1)/perPage, 1)

	header := writer.Header()
	header.Set("X-Total", strconv.Itoa(total))
	header.Set("X-Total-Pages", strconv.Itoa(totalPages))
	header.Set("X-Per-Page", strconv.Itoa(perPage))
	header.Set("X-Page", strconv.Itoa(page))

	if page < totalPages {
		header.Set("X-Next-Page", strconv.Itoa(page+1))
	} else {
		header.Set("X-Next-Page", "")
	}

	start := min((page-1)*perPage, total)
	end := min(start+perPage, total)

	return start, end
}

func atoiDefault(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}

	return value
}

// decodeBody decodes a JSON request body into target. An empty body is fine.
func decodeBody(request *http.Request, target any) error {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return err
	}

	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}

	return json.Unmarshal(body, target)
}
