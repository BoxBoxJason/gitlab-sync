package mirroring

import (
	"fmt"
	"io"
	"net/http"
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

func newLimitedTransport(next http.RoundTripper, limit int) *limitedTransport {
	if next == nil {
		next = http.DefaultTransport
	}

	return &limitedTransport{next: next, slots: make(chan struct{}, limit)}
}

// RoundTrip implements http.RoundTripper.
func (t *limitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	select {
	case t.slots <- struct{}{}:
	case <-req.Context().Done():
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
	closed  bool
}

func (b *slotReleasingBody) Close() error {
	err := b.ReadCloser.Close()

	if !b.closed {
		b.closed = true

		b.release()
	}

	return err //nolint:wrapcheck // transparent wrapper
}

// limitConcurrency applies the API and git concurrency bounds of opts.
func (g *GitlabInstance) limitConcurrency(opts *GitlabInstanceOpts) {
	if opts.APIConcurrency > 0 {
		httpClient := g.Gitlab.HTTPClient()
		httpClient.Transport = newLimitedTransport(httpClient.Transport, opts.APIConcurrency)
	}

	if opts.GitConcurrency > 0 {
		g.gitSlots = make(chan struct{}, opts.GitConcurrency)
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
