package mirroring

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// limitedTransport bounds the number of requests a client has in flight at once.
//
// It only wraps the leaf HTTP round trip, so a slot is never held while waiting
// for another one and it cannot deadlock. The slot is kept until the response body
// is closed, which also bounds the streamed downloads.
type limitedTransport struct {
	next  http.RoundTripper
	slots chan struct{}
}

// newLimitedTransport bounds next with slots, whose capacity is the number of
// requests in flight at once. Clients sharing slots share the bound.
func newLimitedTransport(next http.RoundTripper, slots chan struct{}) *limitedTransport {
	if next == nil {
		next = http.DefaultTransport
	}

	return &limitedTransport{next: next, slots: slots}
}

// RoundTrip implements http.RoundTripper.
func (t *limitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	select {
	case t.slots <- struct{}{}:
	case <-req.Context().Done():
		// A RoundTripper must close the request body, even on errors.
		if req.Body != nil {
			_ = req.Body.Close() //nolint:errcheck // the wait error is the one reported
		}

		return nil, fmt.Errorf("waiting for a request slot: %w", req.Context().Err())
	}

	resp, err := t.next.RoundTrip(req)
	if err != nil {
		<-t.slots

		return nil, err //nolint:wrapcheck // transparent wrapper, the error is the transport's own
	}

	resp.Body = &slotReleasingBody{ReadCloser: resp.Body, release: func() { <-t.slots }}

	return resp, nil
}

// slotReleasingBody frees the request slot once, when the body is closed.
type slotReleasingBody struct {
	io.ReadCloser

	release func()
	once    sync.Once
}

func (b *slotReleasingBody) Close() error {
	err := b.ReadCloser.Close()

	b.once.Do(b.release)

	return err //nolint:wrapcheck // transparent wrapper
}

// limitConcurrency applies the API and git concurrency bounds of opts. The API
// requests draw from opts.apiSlots when it is set, so instances talking to the
// same server share one bound.
func (g *GitlabInstance) limitConcurrency(opts *GitlabInstanceOpts) {
	if opts.APIConcurrency > 0 {
		slots := opts.apiSlots
		if slots == nil {
			slots = make(chan struct{}, opts.APIConcurrency)
		}

		httpClient := g.Gitlab.HTTPClient()
		httpClient.Transport = newLimitedTransport(httpClient.Transport, slots)
	}

	if opts.GitConcurrency > 0 {
		g.gitSlots = make(chan struct{}, opts.GitConcurrency)
	}
}

// sharedAPISlots returns the API request slots both instances of a run must
// share, or nil when each one gets its own: the source and destination are the
// same GitLab server, and one --api-concurrency bound should cover all the
// requests it receives rather than one bound per role.
func sharedAPISlots(sourceURL, destinationURL string, apiConcurrency int) chan struct{} {
	if apiConcurrency <= 0 || !sameGitlabServer(sourceURL, destinationURL) {
		return nil
	}

	return make(chan struct{}, apiConcurrency)
}

// sameGitlabServer reports whether two GitLab URLs point to the same server,
// ignoring the case of the scheme and host, an explicit default port and a
// trailing slash.
func sameGitlabServer(first, second string) bool {
	firstURL, firstErr := url.Parse(strings.TrimSpace(first))
	secondURL, secondErr := url.Parse(strings.TrimSpace(second))

	if firstErr != nil || secondErr != nil || firstURL.Host == "" || secondURL.Host == "" {
		return strings.TrimRight(strings.TrimSpace(first), "/") == strings.TrimRight(strings.TrimSpace(second), "/")
	}

	return strings.EqualFold(firstURL.Scheme, secondURL.Scheme) &&
		strings.EqualFold(firstURL.Hostname(), secondURL.Hostname()) &&
		effectivePort(firstURL) == effectivePort(secondURL) &&
		strings.TrimRight(firstURL.Path, "/") == strings.TrimRight(secondURL.Path, "/")
}

// effectivePort returns the port of u, defaulting to the one of its scheme.
func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}

	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

// acquireGitSlot waits for a free git clone/push slot and returns the function
// releasing it. Without a bound it returns immediately.
func (g *GitlabInstance) acquireGitSlot() func() {
	if g.gitSlots == nil {
		return func() {}
	}

	g.gitSlots <- struct{}{}

	return func() { <-g.gitSlots }
}
