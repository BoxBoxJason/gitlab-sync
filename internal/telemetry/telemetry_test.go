package telemetry

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
)

// otelEnvVars lists every variable Enabled consults, so each test starts clean.
var otelEnvVars = []string{
	"OTEL_SDK_DISABLED",
	"OTEL_METRICS_EXPORTER",
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
	"OTEL_EXPORTER_OTLP_PROTOCOL",
	"OTEL_EXPORTER_OTLP_METRICS_PROTOCOL",
}

func clearOtelEnv(t *testing.T) {
	t.Helper()

	for _, name := range otelEnvVars {
		t.Setenv(name, "")
	}
}

// resetGlobalProvider restores the no-op MeterProvider after a test installs a real one.
func resetGlobalProvider(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { otel.SetMeterProvider(noop.NewMeterProvider()) })
}

func TestEnabled(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{name: "nothing set", env: nil, want: false},
		{name: "generic endpoint", env: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318"}, want: true},
		{name: "metrics endpoint", env: map[string]string{"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "http://localhost:4318/v1/metrics"}, want: true},
		{name: "blank endpoint", env: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "  "}, want: false},
		{name: "exporter otlp without endpoint", env: map[string]string{"OTEL_METRICS_EXPORTER": "otlp"}, want: true},
		{name: "exporter none wins over endpoint", env: map[string]string{"OTEL_METRICS_EXPORTER": "none", "OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318"}, want: false},
		{name: "unsupported exporter", env: map[string]string{"OTEL_METRICS_EXPORTER": "prometheus", "OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318"}, want: false},
		{name: "sdk disabled wins", env: map[string]string{"OTEL_SDK_DISABLED": "TRUE", "OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearOtelEnv(t)

			for name, value := range tt.env {
				t.Setenv(name, value)
			}

			if got := Enabled(); got != tt.want {
				t.Fatalf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProtocol(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "default", env: nil, want: ProtocolHTTPProtobuf},
		{name: "grpc", env: map[string]string{"OTEL_EXPORTER_OTLP_PROTOCOL": "grpc"}, want: ProtocolGRPC},
		{name: "case and spaces", env: map[string]string{"OTEL_EXPORTER_OTLP_PROTOCOL": " GRPC "}, want: ProtocolGRPC},
		{name: "metrics variable wins", env: map[string]string{"OTEL_EXPORTER_OTLP_PROTOCOL": "grpc", "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL": "http/protobuf"}, want: ProtocolHTTPProtobuf},
		{name: "unsupported falls back", env: map[string]string{"OTEL_EXPORTER_OTLP_PROTOCOL": "http/json"}, want: ProtocolHTTPProtobuf},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearOtelEnv(t)

			for name, value := range tt.env {
				t.Setenv(name, value)
			}

			if got := Protocol(); got != tt.want {
				t.Fatalf("Protocol() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRedactURL(t *testing.T) {
	tests := map[string]string{
		"":                                    "",
		"not a url":                           "",
		"https://gitlab.example.com":          "https://gitlab.example.com",
		"https://user:secret@gitlab.com/x":    "https://gitlab.com/x",
		"https://gitlab.com/?private_token=t": "https://gitlab.com/",
		"https://gitlab.com/#frag":            "https://gitlab.com/",
	}

	for raw, want := range tests {
		if got := redactURL(raw); got != want {
			t.Errorf("redactURL(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestSetupDisabledKeepsNoopProvider(t *testing.T) {
	clearOtelEnv(t)
	resetGlobalProvider(t)

	before := otel.GetMeterProvider()

	shutdown, err := Setup(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	if otel.GetMeterProvider() != before {
		t.Fatal("Setup() replaced the global MeterProvider while metrics are disabled")
	}

	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown() error = %v", err)
	}
}

func TestSetupExportsRunMetricsOnShutdown(t *testing.T) {
	var requests atomic.Int32

	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/metrics" {
			requests.Add(1)
		}

		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()

	clearOtelEnv(t)
	resetGlobalProvider(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", collector.URL)
	// Long enough that only the shutdown flush can export during the test.
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "3600000")

	shutdown, err := Setup(context.Background(), Options{Version: "v0.0.0-test", SourceURL: "https://gitlab.com"})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	RecordRun(context.Background(), 1500*time.Millisecond, 2)

	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown() error = %v", err)
	}

	if got := requests.Load(); got == 0 {
		t.Fatal("no OTLP metrics request reached the collector on shutdown")
	}
}

func TestShutdownToleratesUnreachableCollector(t *testing.T) {
	// Grab a free port and close it so nothing listens there.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	endpoint := "http://" + listener.Addr().String()
	_ = listener.Close()

	clearOtelEnv(t)
	resetGlobalProvider(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "3600000")
	t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "500")

	shutdown, err := Setup(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	RecordRun(context.Background(), time.Second, 0)

	done := make(chan struct{})

	go func() {
		defer close(done)

		Shutdown(shutdown) // must only log, never panic or exit
	}()

	select {
	case <-done:
	case <-time.After(ShutdownTimeout + 5*time.Second):
		t.Fatal("Shutdown() did not honour its timeout")
	}
}
