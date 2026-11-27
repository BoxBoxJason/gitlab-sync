//go:build e2e

package e2e

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

const (
	// bodyLimit is how much of a request body the proxy keeps: enough for the
	// API payloads and for the commands and push options of a git push.
	bodyLimit = 64 << 10

	// pktHeader is the size of the length prefix of a git pkt-line.
	pktHeader = 4
)

// request is one request the proxy received.
type request struct {
	header http.Header
	method string
	path   string
	query  string
	body   []byte
	status int
	// received is the size of the response sent back.
	received int64
}

func (r *request) String() string {
	line := r.method + " " + r.path
	if r.query != "" {
		line += "?" + r.query
	}

	return fmt.Sprintf("%s -> %d", line, r.status)
}

// isWrite tells whether the request changes something on the instance. A git
// fetch is a POST that only reads.
func (r *request) isWrite() bool {
	switch r.method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return !strings.HasSuffix(r.path, "/git-upload-pack")
	}
}

// fault makes the matching requests fail without reaching the instance.
type fault struct {
	method  string
	pattern string
	status  int
	// remaining is how many requests still fail, negative for all of them.
	remaining int
}

// proxy sits between gitlab-sync and the real instance, for the scenarios that
// assert on the requests made or need the instance to fail. Every request it does
// not fail is answered by the instance. The URLs the instance gives out are
// rewritten to the proxy so that git goes through it too.
type proxy struct {
	server   *httptest.Server
	requests []*request
	faults   []*fault
	inflight sync.WaitGroup
	mu       sync.Mutex
}

func newProxy(target string) (*proxy, error) {
	backend, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("parse the instance URL: %w", err)
	}

	created := &proxy{}
	reverse := &httputil.ReverseProxy{
		Rewrite: func(proxied *httputil.ProxyRequest) {
			proxied.SetURL(backend)
			// Let the transport decompress, so that the answers can be rewritten.
			proxied.Out.Header.Del("Accept-Encoding")
		},
		ModifyResponse: func(response *http.Response) error {
			return rewriteURLs(response, strings.TrimSuffix(target, "/"), created.server.URL)
		},
	}

	created.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		created.serve(reverse, writer, incoming)
	}))

	return created, nil
}

// rewriteURLs replaces the URL of the instance by the one of the proxy in the
// API answers.
func rewriteURLs(response *http.Response, instanceURL, proxyURL string) error {
	if !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		return nil
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read the answer of the instance: %w", err)
	}

	err = response.Body.Close()
	if err != nil {
		return fmt.Errorf("close the answer of the instance: %w", err)
	}

	body = bytes.ReplaceAll(body, []byte(instanceURL), []byte(proxyURL))
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	response.Header.Set("Content-Length", strconv.Itoa(len(body)))

	return nil
}

// countingWriter records the status and the size of an answer.
type countingWriter struct {
	http.ResponseWriter

	status int
	size   int64
}

func (w *countingWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *countingWriter) Write(data []byte) (int, error) {
	written, err := w.ResponseWriter.Write(data)
	w.size += int64(written)

	if err != nil {
		return written, fmt.Errorf("answer gitlab-sync: %w", err)
	}

	return written, nil
}

// Unwrap lets the reverse proxy flush the underlying writer.
func (w *countingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// keptBody keeps the beginning of a body while it is forwarded.
type keptBody struct {
	io.ReadCloser

	kept bytes.Buffer
}

func (b *keptBody) Read(data []byte) (int, error) {
	read, err := b.ReadCloser.Read(data)
	// A bytes.Buffer never fails to grow, it panics.
	_, _ = b.kept.Write(data[:min(read, max(0, bodyLimit-b.kept.Len()))])

	return read, err //nolint:wrapcheck // io.EOF must reach the caller untouched
}

func (p *proxy) serve(next http.Handler, writer http.ResponseWriter, incoming *http.Request) {
	p.inflight.Add(1)
	defer p.inflight.Done()

	record := &request{
		method: incoming.Method,
		path:   incoming.URL.Path,
		query:  incoming.URL.RawQuery,
		header: incoming.Header.Clone(),
	}

	body := &keptBody{ReadCloser: incoming.Body}
	incoming.Body = body
	counting := &countingWriter{ResponseWriter: writer, status: http.StatusOK}

	// Deferred: the reverse proxy panics when gitlab-sync stops reading an answer.
	defer func() {
		record.status, record.received, record.body = counting.status, counting.size, body.kept.Bytes()

		p.mu.Lock()
		defer p.mu.Unlock()

		p.requests = append(p.requests, record)
	}()

	if status, failed := p.failure(record); failed {
		http.Error(counting, fmt.Sprintf(`{"message":"%d injected by the test proxy"}`, status), status)
	} else {
		next.ServeHTTP(counting, incoming)
	}
}

// drain waits for the requests being served to be recorded.
func (p *proxy) drain() { p.inflight.Wait() }

// failure returns the status a request must fail with, if any.
func (p *proxy) failure(record *request) (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, candidate := range p.faults {
		if candidate.remaining != 0 && candidate.method == record.method && globMatches(candidate.pattern, record.path) {
			candidate.remaining--

			return candidate.status, true
		}
	}

	return 0, false
}

func (p *proxy) inject(injected *fault) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.faults = append(p.faults, injected)
}

// journal returns the requests received so far.
func (p *proxy) journal() []*request {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]*request(nil), p.requests...)
}

func (p *proxy) close() { p.server.Close() }

// globMatches matches path against a pattern where "*" is any run of characters.
func globMatches(pattern, path string) bool {
	return regexp.MustCompile("^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$").MatchString(path)
}

// pushOptionsOf reads the push options of the body of a git-receive-pack request:
// the ref update commands, a flush packet, the options, a flush packet.
func pushOptionsOf(body []byte) ([]string, error) {
	var (
		options    []string
		flushes    int
		advertised bool
	)

	for len(body) >= pktHeader {
		length, err := strconv.ParseUint(string(body[:pktHeader]), 16, 32)
		if err != nil {
			return nil, fmt.Errorf("read a pkt-line length: %w", err)
		}

		if length == 0 {
			flushes++
			body = body[pktHeader:]

			if flushes == 2 || !advertised {
				return options, nil
			}

			continue
		}

		if length < pktHeader || int(length) > len(body) {
			return nil, errors.New("truncated pkt-line")
		}

		line := string(body[pktHeader:length])
		body = body[length:]

		if flushes == 1 {
			options = append(options, strings.TrimSuffix(line, "\n"))

			continue
		}

		// The capabilities follow the first command, after a NUL byte.
		if _, capabilities, found := strings.Cut(line, "\x00"); found {
			advertised = strings.Contains(" "+capabilities+" ", " push-options ")
		}
	}

	return nil, errors.New("the push has no flush packet")
}
