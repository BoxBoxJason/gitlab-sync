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

	client := &http.Client{Transport: newLimitedTransport(nil, limit)}

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
