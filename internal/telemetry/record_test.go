package telemetry

import (
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// installGlobalReader makes the global MeterProvider record into a ManualReader.
func installGlobalReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	resetGlobalProvider(t)

	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	return reader
}

func TestRecordEntity(t *testing.T) {
	reader := installGlobalReader(t)

	RecordEntity(EntityProject, OperationCreate, true)
	RecordEntity(EntityProject, OperationCreate, true)
	RecordEntity(EntityProject, OperationSync, false)
	RecordEntity(EntityGroup, OperationSkip, true)

	metrics := collect(t, reader)
	project := attribute.String(AttrEntity, EntityProject)

	tests := []struct {
		name string
		want int64
		with []attribute.KeyValue
	}{
		{"created projects", 2, []attribute.KeyValue{project, attribute.String(AttrOperation, OperationCreate), attribute.String(AttrOutcome, OutcomeSuccess)}},
		{"failed project syncs", 1, []attribute.KeyValue{project, attribute.String(AttrOperation, OperationSync), attribute.String(AttrOutcome, OutcomeFailure)}},
		{"all projects", 3, []attribute.KeyValue{project}},
		{"skipped groups", 1, []attribute.KeyValue{attribute.String(AttrEntity, EntityGroup), attribute.String(AttrOperation, OperationSkip)}},
	}

	for _, tt := range tests {
		if got := counterValue(t, metrics, "gitlab_sync.entities", tt.with...); got != tt.want {
			t.Errorf("%s = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestRecordError(t *testing.T) {
	reader := installGlobalReader(t)

	RecordError(true)
	RecordError(false)
	RecordError(false)

	metrics := collect(t, reader)

	if got := counterValue(t, metrics, "gitlab_sync.errors", attribute.String(AttrSeverity, SeverityBlocking)); got != 1 {
		t.Errorf("blocking errors = %d, want 1", got)
	}

	if got := counterValue(t, metrics, "gitlab_sync.errors", attribute.String(AttrSeverity, SeverityNonBlocking)); got != 2 {
		t.Errorf("non-blocking errors = %d, want 2", got)
	}
}

func TestRecordGitCache(t *testing.T) {
	reader := installGlobalReader(t)

	RecordGitCache(CacheHit)
	RecordGitCache(CacheHit)
	RecordGitCache(CacheMiss)

	metrics := collect(t, reader)

	if got := counterValue(t, metrics, "gitlab_sync.git.cache", attribute.String(AttrResult, CacheHit)); got != 2 {
		t.Errorf("cache hits = %d, want 2", got)
	}

	if got := counterValue(t, metrics, "gitlab_sync.git.cache", attribute.String(AttrResult, CacheMiss)); got != 1 {
		t.Errorf("cache misses = %d, want 1", got)
	}
}

func TestRecordGitPush(t *testing.T) {
	reader := installGlobalReader(t)

	RecordGitPush(3*time.Second, true)
	RecordGitPush(time.Second, false)

	hist, ok := collect(t, reader)["gitlab_sync.git.push.duration"].Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatal("gitlab_sync.git.push.duration missing or not a float64 histogram")
	}

	byOutcome := map[string]float64{}

	for _, point := range hist.DataPoints {
		outcome, _ := point.Attributes.Value(AttrOutcome)
		byOutcome[outcome.AsString()] += point.Sum
	}

	if byOutcome[OutcomeSuccess] != 3 || byOutcome[OutcomeFailure] != 1 {
		t.Errorf("push durations by outcome = %v, want success 3s and failure 1s", byOutcome)
	}
}

func TestTimePhase(t *testing.T) {
	reader := installGlobalReader(t)

	record := TimePhase(PhaseGroups)
	time.Sleep(10 * time.Millisecond)
	record()

	gauge, ok := collect(t, reader)["gitlab_sync.phase.duration"].Data.(metricdata.Gauge[float64])
	if !ok || len(gauge.DataPoints) != 1 {
		t.Fatal("gitlab_sync.phase.duration missing or not a single float64 gauge point")
	}

	point := gauge.DataPoints[0]
	if phase, _ := point.Attributes.Value(AttrPhase); phase.AsString() != PhaseGroups {
		t.Errorf("phase = %q, want %q", phase.AsString(), PhaseGroups)
	}

	if point.Value < 0.01 {
		t.Errorf("phase duration = %v, want at least 0.01s", point.Value)
	}
}
