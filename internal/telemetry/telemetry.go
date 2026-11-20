// Package telemetry wires the OpenTelemetry metrics SDK for gitlab-sync.
//
// Metrics are opt-in and configured only through the standard OTEL_*
// environment variables: they are exported over OTLP (http/protobuf by
// default, or grpc through OTEL_EXPORTER_OTLP_PROTOCOL) when an OTLP
// endpoint is set (OTEL_EXPORTER_OTLP_ENDPOINT or
// OTEL_EXPORTER_OTLP_METRICS_ENDPOINT) or when OTEL_METRICS_EXPORTER=otlp.
// Otherwise the global no-op MeterProvider stays in place and recording a
// metric costs nothing.
//
// Telemetry never changes the outcome of a run: export failures are logged as
// a warning and are not reported through helpers.Report.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.uber.org/zap"
)

const (
	// ScopeName is the instrumentation scope of every gitlab-sync instrument.
	ScopeName = "github.com/boxboxjason/gitlab-sync"
	// ServiceName is the default service.name resource attribute
	// (OTEL_SERVICE_NAME / OTEL_RESOURCE_ATTRIBUTES take precedence).
	ServiceName = "gitlab-sync"

	// ProtocolHTTPProtobuf is the default OTLP protocol.
	ProtocolHTTPProtobuf = "http/protobuf"
	// ProtocolGRPC selects the OTLP/gRPC exporter.
	ProtocolGRPC = "grpc"

	// ShutdownTimeout bounds the final flush done before the process exits.
	ShutdownTimeout = 10 * time.Second

	// AttrSourceURL is the resource attribute holding the source instance URL.
	AttrSourceURL = "gitlab_sync.source.url"
	// AttrDestinationURL is the resource attribute holding the destination instance URL.
	AttrDestinationURL = "gitlab_sync.destination.url"
)

// Options describes the run being instrumented.
type Options struct {
	Version        string
	SourceURL      string
	DestinationURL string
}

// ShutdownFunc flushes the pending metrics and releases the exporter.
type ShutdownFunc func(ctx context.Context) error

func noopShutdown(context.Context) error { return nil }

// Enabled reports whether the OTEL_* environment asks for OTLP metrics.
func Enabled() bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_SDK_DISABLED")), "true") {
		return false
	}

	switch exporter := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_METRICS_EXPORTER"))); exporter {
	case "otlp":
		return true
	case "":
		return strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT")) != "" ||
			strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")) != ""
	default:
		// "none", or an exporter we do not ship (console, prometheus, ...).
		return false
	}
}

// Setup installs the global MeterProvider when metrics are enabled and
// returns the function that flushes and stops it. When metrics are disabled
// it does nothing and returns a no-op ShutdownFunc.
func Setup(ctx context.Context, opts Options) (ShutdownFunc, error) {
	if !Enabled() {
		zap.L().Debug("OpenTelemetry metrics disabled (no OTLP endpoint configured)")

		return noopShutdown, nil
	}

	protocol := Protocol()

	exporter, err := newExporter(ctx, protocol)
	if err != nil {
		return noopShutdown, err
	}

	res, err := newResource(ctx, opts)
	if err != nil {
		// A partial resource is still usable: keep going with what was detected.
		zap.L().Warn("Incomplete OpenTelemetry resource", zap.Error(err))
	}

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		// The interval and timeout honour OTEL_METRIC_EXPORT_INTERVAL / _TIMEOUT.
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
	)

	otel.SetErrorHandler(newErrorHandler())
	otel.SetMeterProvider(provider)

	zap.L().Info("OpenTelemetry metrics enabled", zap.String("protocol", protocol))

	return provider.Shutdown, nil
}

// Meter returns the gitlab-sync meter from the global MeterProvider. Instruments
// created before Setup are delegated to the real provider once it is installed.
func Meter() metric.Meter {
	return otel.Meter(ScopeName)
}

// Shutdown runs shutdown with ShutdownTimeout and logs a failure as a warning:
// a collector that cannot be reached never fails the run.
func Shutdown(shutdown ShutdownFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	err := shutdown(ctx)
	if err != nil {
		zap.L().Warn("Failed to flush the OpenTelemetry metrics", zap.Error(err))
	}
}

func newResource(ctx context.Context, opts Options) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{semconv.ServiceName(ServiceName)}
	if opts.Version != "" {
		attrs = append(attrs, semconv.ServiceVersion(opts.Version))
	}

	if u := redactURL(opts.SourceURL); u != "" {
		attrs = append(attrs, attribute.String(AttrSourceURL, u))
	}

	if u := redactURL(opts.DestinationURL); u != "" {
		attrs = append(attrs, attribute.String(AttrDestinationURL, u))
	}

	// Later options win: OTEL_SERVICE_NAME / OTEL_RESOURCE_ATTRIBUTES override our defaults.
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithAttributes(attrs...),
		resource.WithFromEnv(),
	)
	if err != nil {
		return res, fmt.Errorf("failed to detect the OpenTelemetry resource: %w", err)
	}

	return res, nil
}

// redactURL drops any credentials, query and fragment from raw so it is safe to
// export as a resource attribute.
func redactURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}

	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return parsed.String()
}

// Protocol returns the OTLP protocol requested by the environment
// (OTEL_EXPORTER_OTLP_METRICS_PROTOCOL, then OTEL_EXPORTER_OTLP_PROTOCOL).
// It defaults to http/protobuf and falls back to it, with a warning, for a
// protocol the Go SDK does not ship (such as http/json).
func Protocol() string {
	for _, name := range []string{"OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "OTEL_EXPORTER_OTLP_PROTOCOL"} {
		protocol := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
		switch protocol {
		case "":
			continue
		case ProtocolHTTPProtobuf, ProtocolGRPC:
			return protocol
		default:
			zap.L().Warn("Unsupported OTLP protocol, falling back to "+ProtocolHTTPProtobuf,
				zap.String("variable", name), zap.String("protocol", protocol))

			return ProtocolHTTPProtobuf
		}
	}

	return ProtocolHTTPProtobuf
}

// newExporter builds the OTLP exporter for protocol. Endpoint, headers,
// timeout, compression and TLS come from the OTEL_EXPORTER_OTLP_* variables.
func newExporter(ctx context.Context, protocol string) (sdkmetric.Exporter, error) {
	var (
		exporter sdkmetric.Exporter
		err      error
	)

	if protocol == ProtocolGRPC {
		exporter, err = otlpmetricgrpc.New(ctx)
	} else {
		exporter, err = otlpmetrichttp.New(ctx)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create the OTLP %s metrics exporter: %w", protocol, err)
	}

	return exporter, nil
}

// errorHandler logs the first OpenTelemetry error as a warning and the
// following ones at debug level, so an unreachable collector does not flood
// the logs.
type errorHandler struct {
	warned atomic.Bool
}

func newErrorHandler() *errorHandler { return &errorHandler{} }

// Handle implements otel.ErrorHandler.
func (h *errorHandler) Handle(err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}

	if h.warned.CompareAndSwap(false, true) {
		zap.L().Warn("OpenTelemetry export error (further errors are logged at debug level)", zap.Error(err))

		return
	}

	zap.L().Debug("OpenTelemetry export error", zap.Error(err))
}
