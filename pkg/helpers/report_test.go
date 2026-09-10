package helpers

import (
	"errors"
	"testing"
)

func TestExitCodeReflectsHighestSeverity(t *testing.T) {
	tests := []struct {
		name     string
		report   func()
		wantCode int
	}{
		{
			name:     "nothing reported",
			report:   func() {},
			wantCode: ExitOK,
		},
		{
			name:     "nil is ignored",
			report:   func() { Report(nil) },
			wantCode: ExitOK,
		},
		{
			name:     "non-blocking only",
			report:   func() { ReportNonBlocking(errors.New("soft")) },
			wantCode: ExitNonBlocking,
		},
		{
			name: "blocking wins over non-blocking regardless of order",
			report: func() {
				ReportNonBlocking(errors.New("soft"))
				ReportBlocking(errors.New("hard"))
				ReportNonBlocking(errors.New("soft again"))
			},
			wantCode: ExitBlocking,
		},
		{
			name:     "plain error counts as non-blocking",
			report:   func() { Report(errors.New("plain")) },
			wantCode: ExitNonBlocking,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetReported()
			t.Cleanup(ResetReported)

			tt.report()

			if got := ExitCode(); got != tt.wantCode {
				t.Errorf("ExitCode() = %d, want %d", got, tt.wantCode)
			}
		})
	}
}

func TestResetReported(t *testing.T) {
	ReportBlocking(errors.New("boom"))
	if ExitCode() != ExitBlocking {
		t.Fatalf("precondition failed: expected exit code %d", ExitBlocking)
	}

	ResetReported()

	if got := ExitCode(); got != ExitOK {
		t.Errorf("after ResetReported ExitCode() = %d, want %d", got, ExitOK)
	}
}
