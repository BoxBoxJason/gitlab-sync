package helpers

import (
	"sync/atomic"

	"go.uber.org/zap"
)

// Process exit codes derived from the errors reported during a run.
const (
	// ExitOK means no error was reported.
	ExitOK = 0
	// ExitBlocking means at least one blocking error was reported.
	ExitBlocking = 1
	// ExitNonBlocking means only non-blocking errors were reported.
	ExitNonBlocking = 2
)

// reportedSeverity holds the highest error severity reported during the run.
// ExitCode consults it once the mirroring process is done to pick the process
// exit status.
//
//nolint:gochecknoglobals // process-wide error tally, intentionally shared
var reportedSeverity atomic.Int32

// Report logs err straight away, at a level matching its severity (blocking
// errors at ERROR, everything else at WARN), and records its severity so
// ExitCode can pick the right process exit status at the end of the run.
// A nil err is ignored.
func Report(err error) {
	if err == nil {
		return
	}

	severity := int32(SeverityOf(err)) //nolint:gosec // Severity is a small, bounded enum

	for {
		current := reportedSeverity.Load()
		if severity <= current || reportedSeverity.CompareAndSwap(current, severity) {
			break
		}
	}

	if Severity(severity) == SeverityBlocking {
		zap.L().Error(err.Error())
	} else {
		zap.L().Warn(err.Error())
	}
}

// ReportBlocking reports err as a blocking error. A nil err is ignored.
func ReportBlocking(err error) {
	if err != nil {
		Report(NewBlocking(err))
	}
}

// ReportNonBlocking reports err as a non-blocking error. A nil err is ignored.
func ReportNonBlocking(err error) {
	if err != nil {
		Report(NewNonBlocking(err))
	}
}

// ExitCode returns the process exit code implied by the errors reported so far.
func ExitCode() int {
	switch Severity(reportedSeverity.Load()) {
	case SeverityBlocking:
		return ExitBlocking
	case SeverityNonBlocking:
		return ExitNonBlocking
	case SeverityNone:
		return ExitOK
	default:
		return ExitOK
	}
}

// ResetReported clears the reported-severity tally. It is intended for tests
// that need a clean slate between cases.
func ResetReported() {
	reportedSeverity.Store(int32(SeverityNone))
}
