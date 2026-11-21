package mirroring

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLimitedTransportBoundsInFlightRequests(t *testing.T) {
	t.Parallel()

	const limit = 3

	var current, peak atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = request

		now := current.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}

		time.Sleep(20 * time.Millisecond)
		current.Add(-1)

		_, _ = writer.Write([]byte("ok")) //nolint:errcheck // test
	}))
	t.Cleanup(server.Close)

	client := &http.Client{Transport: newLimitedTransport(nil, make(chan struct{}, limit))}

	var waitGroup sync.WaitGroup

	for range 30 {
		waitGroup.Go(func() {
			resp, err := client.Get(server.URL) //nolint:noctx // test
			if err != nil {
				t.Error(err)

				return
			}

			_ = resp.Body.Close() //nolint:errcheck // test
		})
	}

	waitGroup.Wait()

	if got := peak.Load(); got > limit {
		t.Fatalf("peak in-flight requests = %d, want <= %d", got, limit)
	}
}

func TestAcquireGitSlotBounds(t *testing.T) {
	t.Parallel()

	instance := &GitlabInstance{gitSlots: make(chan struct{}, 1)}
	release := instance.acquireGitSlot()

	acquired := make(chan struct{})

	go func() {
		instance.acquireGitSlot()()
		close(acquired)
	}()

	select {
	case <-acquired:
		t.Fatal("second slot acquired while the first one is held")
	case <-time.After(50 * time.Millisecond):
	}

	release()

	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("second slot not acquired after release")
	}

	(&GitlabInstance{}).acquireGitSlot()()
}

func TestSameGitlabServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		first, second string
		want          bool
	}{
		{"https://gitlab.example.com", "https://gitlab.example.com", true},
		{"https://gitlab.example.com/", "https://GitLab.Example.com", true},
		{"https://gitlab.example.com:443", "https://gitlab.example.com", true},
		{"http://gitlab.example.com:80/", "http://gitlab.example.com", true},
		{"https://example.com/gitlab/", "https://example.com/gitlab", true},
		{"https://gitlab.example.com", "https://gitlab.other.com", false},
		{"https://gitlab.example.com", "http://gitlab.example.com", false},
		{"https://gitlab.example.com:8443", "https://gitlab.example.com", false},
		{"https://example.com/a", "https://example.com/b", false},
	}

	for _, tt := range tests {
		t.Run(tt.first+" vs "+tt.second, func(t *testing.T) {
			t.Parallel()

			if got := sameGitlabServer(tt.first, tt.second); got != tt.want {
				t.Errorf("sameGitlabServer(%q, %q) = %t; want %t", tt.first, tt.second, got, tt.want)
			}
		})
	}
}

func TestSharedAPISlots(t *testing.T) {
	t.Parallel()

	if slots := sharedAPISlots("https://a.example.com", "https://b.example.com", 4); slots != nil {
		t.Error("different servers share their API slots")
	}

	if slots := sharedAPISlots("https://a.example.com", "https://a.example.com/", 0); slots != nil {
		t.Error("unbounded mode creates shared API slots")
	}

	slots := sharedAPISlots("https://a.example.com", "https://a.example.com/", 4)
	if slots == nil || cap(slots) != 4 {
		t.Fatalf("same server: got slots %v (cap %d), want a shared channel of capacity 4", slots, cap(slots))
	}
}

// TestLimitConcurrencySharesAPISlots checks that two instances built with the
// same apiSlots draw from one bound instead of one each.
func TestLimitConcurrencySharesAPISlots(t *testing.T) {
	t.Parallel()

	shared := make(chan struct{}, 2)

	transports := make([]*limitedTransport, 0, 2)

	for range 2 {
		instance, err := NewGitlabInstance(&GitlabInstanceOpts{GitlabURL: "https://gitlab.example.com", APIConcurrency: 2, apiSlots: shared})
		if err != nil {
			t.Fatalf("NewGitlabInstance failed: %v", err)
		}

		transport, ok := instance.Gitlab.HTTPClient().Transport.(*limitedTransport)
		if !ok {
			t.Fatalf("transport is %T, want *limitedTransport", instance.Gitlab.HTTPClient().Transport)
		}

		transports = append(transports, transport)
	}

	if transports[0].slots != shared || transports[1].slots != shared {
		t.Error("the instances do not share the API slots they were given")
	}
}
