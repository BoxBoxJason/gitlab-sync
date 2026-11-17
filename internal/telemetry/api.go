package telemetry

import (
	"net/http"
	"strconv"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

const (
	// AttrRole tells which GitLab instance a measure is about (source or destination).
	AttrRole = "role"
	// AttrMethod is the HTTP request method.
	AttrMethod = "method"
	// AttrStatusClass is the HTTP response status class (2xx, 4xx, ...), or
	// "error" when no response was received.
	AttrStatusClass = "status_class"

	statusClassError = "error"
	statusClassSize  = 100
)

// apiDurationBuckets are the bucket boundaries, in seconds, of the API call
// duration histogram (the OpenTelemetry HTTP semantic conventions ones,
// extended for the slow calls a big GitLab instance can have).
//
//nolint:gochecknoglobals // read-only bucket boundaries
var apiDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10, 30, 60}

// APIInstrumentation measures the HTTP calls one GitLab client makes.
type APIInstrumentation struct {
	role     attribute.KeyValue
	requests metric.Int64Counter
	duration metric.Float64Histogram
	retries  metric.Int64Counter
}

// NewAPIInstrumentation creates the API instruments for the GitLab instance
// playing role, from provider (otel.GetMeterProvider() in production).
// Instruments that cannot be created fall back to no-ops: telemetry never
// breaks the mirroring.
func NewAPIInstrumentation(provider metric.MeterProvider, role string) *APIInstrumentation {
	meter := provider.Meter(ScopeName)

	requests, err := meter.Int64Counter("gitlab_sync.api.requests",
		metric.WithDescription("HTTP requests sent to the GitLab API, retries included."),
		metric.WithUnit("{request}"),
	)
	logInstrumentError("gitlab_sync.api.requests", err)

	duration, err := meter.Float64Histogram("gitlab_sync.api.duration",
		metric.WithDescription("Duration of the HTTP requests sent to the GitLab API."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(apiDurationBuckets...),
	)
	logInstrumentError("gitlab_sync.api.duration", err)

	retries, err := meter.Int64Counter("gitlab_sync.api.retries",
		metric.WithDescription("HTTP requests to the GitLab API that were retries of a failed attempt."),
		metric.WithUnit("{retry}"),
	)
	logInstrumentError("gitlab_sync.api.retries", err)

	return &APIInstrumentation{
		role:     attribute.String(AttrRole, role),
		requests: requests,
		duration: duration,
		retries:  retries,
	}
}

// Interceptor wraps next so every HTTP attempt is counted and timed. Its
// signature matches gitlab.Interceptor.
func (a *APIInstrumentation) Interceptor(next http.RoundTripper) http.RoundTripper {
	return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		start := time.Now()
		resp, err := next.RoundTrip(req)

		statusClass := statusClassError
		if err == nil && resp != nil {
			statusClass = StatusClass(resp.StatusCode)
		}

		attrs := metric.WithAttributes(a.role, attribute.String(AttrMethod, req.Method), attribute.String(AttrStatusClass, statusClass))
		a.requests.Add(req.Context(), 1, attrs)
		a.duration.Record(req.Context(), time.Since(start).Seconds(), attrs)

		return resp, err //nolint:wrapcheck // a transparent RoundTripper must not alter errors
	})
}

// RetryHook counts the retries. retryablehttp calls it before every attempt,
// starting at attempt 0, so only the following attempts are retries. Its
// signature matches retryablehttp.RequestLogHook.
func (a *APIInstrumentation) RetryHook(_ retryablehttp.Logger, req *http.Request, attempt int) {
	if attempt > 0 {
		a.retries.Add(req.Context(), 1, metric.WithAttributes(a.role))
	}
}

// StatusClass returns the class of an HTTP status code, such as "2xx" or "5xx".
func StatusClass(code int) string {
	if code < statusClassSize || code >= 6*statusClassSize {
		return "unknown"
	}

	return strconv.Itoa(code/statusClassSize) + "xx"
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func logInstrumentError(name string, err error) {
	if err != nil {
		zap.L().Debug("Failed to create a metric instrument", zap.String("instrument", name), zap.Error(err))
	}
}
