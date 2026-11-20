package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// RecordRun records the overall outcome of a mirroring run: how long it took
// and the process exit code it ends with.
//
// Both are gauges: each run is a separate process, so the useful question is
// "how did the last run go", not a distribution inside a single run.
func RecordRun(ctx context.Context, duration time.Duration, exitCode int) {
	meter := Meter()

	durationGauge, err := meter.Float64Gauge("gitlab_sync.run.duration",
		metric.WithDescription("Duration of the last mirroring run."),
		metric.WithUnit("s"),
	)
	if err != nil {
		zap.L().Debug("Failed to create the run duration instrument", zap.Error(err))
	} else {
		durationGauge.Record(ctx, duration.Seconds())
	}

	statusGauge, err := meter.Int64Gauge("gitlab_sync.run.status",
		metric.WithDescription("Exit code of the last mirroring run: 0 OK, 1 blocking errors, 2 non-blocking errors only."),
		// An annotation unit: "1" would make Prometheus append a misleading "_ratio" suffix.
		metric.WithUnit("{code}"),
	)
	if err != nil {
		zap.L().Debug("Failed to create the run status instrument", zap.Error(err))
	} else {
		statusGauge.Record(ctx, int64(exitCode))
	}
}
