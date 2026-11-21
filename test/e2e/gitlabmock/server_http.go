package gitlabmock

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

func (s *Server) addFault(method, pathPattern, query string, status, times int) {
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

	written, err := r.ResponseWriter.Write(data)
	if err != nil {
		return written, fmt.Errorf("write the response: %w", err)
	}

	return written, nil
}

// serve journals the request, then applies the token check, the injected faults
// and finally the routes.
func (s *Server) serve(writer http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "400 Bad Request: "+err.Error())

		return
	}

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
		sendBytes(writer, content)

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
