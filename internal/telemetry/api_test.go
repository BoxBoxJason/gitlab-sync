package telemetry

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// newTestClient returns a retryable client instrumented like the GitLab ones,
// and the reader holding what it recorded.
func newTestClient(t *testing.T, role string) (*retryablehttp.Client, *sdkmetric.ManualReader) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	api := NewAPIInstrumentation(provider, role)

	client := retryablehttp.NewClient()
	client.Logger = nil
	client.RetryMax = 2
	client.RetryWaitMin = time.Millisecond
	client.RetryWaitMax = time.Millisecond
	client.HTTPClient.Transport = api.Interceptor(http.DefaultTransport)
	client.RequestLogHook = api.RetryHook

	return client, reader
}

func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Metrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	byName := make(map[string]metricdata.Metrics)

	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			byName[m.Name] = m
		}
	}

	return byName
}

// counterValue sums the data points of a counter matching every attribute in want.
func counterValue(t *testing.T, metrics map[string]metricdata.Metrics, name string, want ...attribute.KeyValue) int64 {
	t.Helper()

	m, ok := metrics[name]
	if !ok {
		return 0
	}

	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("%s is a %T, want an int64 sum", name, m.Data)
	}

	var total int64

	for _, point := range sum.DataPoints {
		if hasAttributes(point.Attributes, want) {
			total += point.Value
		}
	}

	return total
}

func hasAttributes(set attribute.Set, want []attribute.KeyValue) bool {
	for _, kv := range want {
		got, ok := set.Value(kv.Key)
		if !ok || got != kv.Value {
			return false
		}
	}

	return true
}

func TestAPIInstrumentationCountsAttemptsAndRetries(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, reader := newTestClient(t, "destination")

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	_ = resp.Body.Close()

	metrics := collect(t, reader)
	role := attribute.String(AttrRole, "destination")
	get := attribute.String(AttrMethod, http.MethodGet)

	if got := counterValue(t, metrics, "gitlab_sync.api.requests", role, get, attribute.String(AttrStatusClass, "5xx")); got != 1 {
		t.Errorf("5xx requests = %d, want 1", got)
	}

	if got := counterValue(t, metrics, "gitlab_sync.api.requests", role, get, attribute.String(AttrStatusClass, "2xx")); got != 1 {
		t.Errorf("2xx requests = %d, want 1", got)
	}

	if got := counterValue(t, metrics, "gitlab_sync.api.retries", role); got != 1 {
		t.Errorf("retries = %d, want 1", got)
	}

	hist, ok := metrics["gitlab_sync.api.duration"].Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("gitlab_sync.api.duration missing or not a float64 histogram")
	}

	var observations uint64
	for _, point := range hist.DataPoints {
		observations += point.Count
	}

	if observations != 2 {
		t.Errorf("duration observations = %d, want 2", observations)
	}
}

func TestAPIInstrumentationRecordsTransportErrors(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	url := "http://" + listener.Addr().String()
	_ = listener.Close()

	client, reader := newTestClient(t, "source")
	client.RetryMax = 0

	resp, err := client.Get(url)
	if err == nil {
		_ = resp.Body.Close()

		t.Fatal("Get() succeeded against a closed port")
	}

	metrics := collect(t, reader)

	if got := counterValue(t, metrics, "gitlab_sync.api.requests", attribute.String(AttrRole, "source"), attribute.String(AttrStatusClass, statusClassError)); got != 1 {
		t.Errorf("error requests = %d, want 1", got)
	}

	if got := counterValue(t, metrics, "gitlab_sync.api.retries"); got != 0 {
		t.Errorf("retries = %d, want 0", got)
	}
}

func TestStatusClass(t *testing.T) {
	tests := map[int]string{
		0:   "unknown",
		99:  "unknown",
		100: "1xx",
		200: "2xx",
		204: "2xx",
		304: "3xx",
		404: "4xx",
		429: "4xx",
		503: "5xx",
		600: "unknown",
	}

	for code, want := range tests {
		if got := StatusClass(code); got != want {
			t.Errorf("StatusClass(%d) = %q, want %q", code, got, want)
		}
	}
}
