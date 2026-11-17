package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Attribute keys of the mirroring metrics.
const (
	// AttrEntity is the kind of GitLab object a measure is about.
	AttrEntity = "entity"
	// AttrOperation is what the mirroring did with that object.
	AttrOperation = "operation"
	// AttrOutcome tells whether the operation succeeded.
	AttrOutcome = "outcome"
	// AttrSeverity is the severity of a reported error.
	AttrSeverity = "severity"
	// AttrPhase is a step of the mirroring run.
	AttrPhase = "phase"
	// AttrResult is the result of a git cache lookup.
	AttrResult = "result"
)

// Values of the entity attribute.
const (
	EntityGroup   = "group"
	EntityProject = "project"
	EntityRelease = "release"
	EntityAsset   = "asset"
	EntityIssue   = "issue"
)

// Values of the operation attribute.
const (
	// OperationCreate means the object did not exist on the destination and was created.
	OperationCreate = "create"
	// OperationSync means the object already existed and was synchronized again.
	OperationSync = "sync"
	// OperationSkip means the object already existed and was left as it is.
	OperationSkip = "skip"
	// OperationDelete means the destination object no longer exists on the source and was deleted.
	OperationDelete = "delete"
)

// Values of the outcome attribute.
const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
)

// Values of the severity attribute.
const (
	SeverityBlocking    = "blocking"
	SeverityNonBlocking = "non_blocking"
)

// Values of the phase attribute.
const (
	// PhaseFetch covers the connection to both instances and the fetch of their groups and projects.
	PhaseFetch = "fetch"
	// PhaseGroups covers the creation of the destination groups.
	PhaseGroups = "groups"
	// PhaseProjects covers the mirroring of the projects (git, attributes, releases, issues).
	PhaseProjects = "projects"
	// PhaseDryRun covers the dry run output.
	PhaseDryRun = "dry_run"
)

// Values of the result attribute.
const (
	// CacheHit means a cached repository was refreshed with a fetch.
	CacheHit = "hit"
	// CacheMiss means the repository was not cached yet and was cloned.
	CacheMiss = "miss"
	// CacheInvalid means the cached repository was unusable and was cloned again.
	CacheInvalid = "invalid"
)

// gitDurationBuckets are the bucket boundaries, in seconds, of the git
// durations: from a no-op push to a multi-gigabyte repository.
//
//nolint:gochecknoglobals // read-only bucket boundaries
var gitDurationBuckets = []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600, 1800}

// Outcome returns OutcomeSuccess when ok, OutcomeFailure otherwise.
func Outcome(ok bool) string {
	if ok {
		return OutcomeSuccess
	}

	return OutcomeFailure
}

// RecordEntity counts one GitLab object the mirroring went through.
func RecordEntity(entity, operation string, succeeded bool) {
	counter, err := Meter().Int64Counter("gitlab_sync.entities",
		metric.WithDescription("GitLab objects (groups, projects, releases, release assets, issues) processed by the mirroring."),
		metric.WithUnit("{entity}"),
	)
	if err != nil {
		logInstrumentError("gitlab_sync.entities", err)

		return
	}

	counter.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String(AttrEntity, entity),
		attribute.String(AttrOperation, operation),
		attribute.String(AttrOutcome, Outcome(succeeded)),
	))
}

// RecordError counts one reported error. blocking is its severity.
func RecordError(blocking bool) {
	counter, err := Meter().Int64Counter("gitlab_sync.errors",
		metric.WithDescription("Errors reported during the mirroring, by severity."),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		logInstrumentError("gitlab_sync.errors", err)

		return
	}

	severity := SeverityNonBlocking
	if blocking {
		severity = SeverityBlocking
	}

	counter.Add(context.Background(), 1, metric.WithAttributes(attribute.String(AttrSeverity, severity)))
}

// RecordPhase records how long a phase of the run took.
func RecordPhase(phase string, duration time.Duration) {
	gauge, err := Meter().Float64Gauge("gitlab_sync.phase.duration",
		metric.WithDescription("Duration of each phase of the last mirroring run."),
		metric.WithUnit("s"),
	)
	if err != nil {
		logInstrumentError("gitlab_sync.phase.duration", err)

		return
	}

	gauge.Record(context.Background(), duration.Seconds(), metric.WithAttributes(attribute.String(AttrPhase, phase)))
}

// TimePhase starts timing phase and returns the function that records it.
func TimePhase(phase string) func() {
	start := time.Now()

	return func() { RecordPhase(phase, time.Since(start)) }
}

// RunPhase runs fn and records its duration as phase.
func RunPhase(phase string, fn func()) {
	defer TimePhase(phase)()

	fn()
}

// RecordGitPush records the duration of one git mirror push.
func RecordGitPush(duration time.Duration, succeeded bool) {
	histogram, err := Meter().Float64Histogram("gitlab_sync.git.push.duration",
		metric.WithDescription("Duration of the git pushes to the destination repositories (freemium mirroring)."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(gitDurationBuckets...),
	)
	if err != nil {
		logInstrumentError("gitlab_sync.git.push.duration", err)

		return
	}

	histogram.Record(context.Background(), duration.Seconds(), metric.WithAttributes(attribute.String(AttrOutcome, Outcome(succeeded))))
}

// RecordGitCache counts one lookup of the git repository cache.
func RecordGitCache(result string) {
	counter, err := Meter().Int64Counter("gitlab_sync.git.cache",
		metric.WithDescription("Lookups of the git repository cache (--cache-dir), by result."),
		metric.WithUnit("{lookup}"),
	)
	if err != nil {
		logInstrumentError("gitlab_sync.git.cache", err)

		return
	}

	counter.Add(context.Background(), 1, metric.WithAttributes(attribute.String(AttrResult, result)))
}
