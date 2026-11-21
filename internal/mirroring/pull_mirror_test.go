package mirroring

import (
	"net/http"
	"testing"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

func TestIsPullMirrorAvailable(t *testing.T) {
	t.Parallel()

	const supportedVersion = "18.0.0"
	const unsupportedVersion = "17.0.0"
	tests := []struct {
		name           string
		licensePlan    string
		version        string
		expectedError  bool
		expectedResult bool
		forcePremium   bool
	}{
		{
			name:           "Premium license, good version",
			licensePlan:    PremiumPlan,
			version:        supportedVersion,
			expectedResult: true,
		},
		{
			name:           "Ultimate license, good version",
			licensePlan:    UltimatePlan,
			version:        supportedVersion,
			expectedResult: true,
		},
		{
			name:           "Free license, good version",
			licensePlan:    "free",
			version:        supportedVersion,
			expectedResult: false,
		},
		{
			name:           "Free license, good version, force premium",
			licensePlan:    "free",
			version:        supportedVersion,
			expectedResult: true,
			forcePremium:   true,
		},
		{
			name:           "Premium license, bad version",
			licensePlan:    PremiumPlan,
			version:        unsupportedVersion,
			expectedResult: false,
		},
		{
			name:           "Ultimate license, bad version",
			licensePlan:    UltimatePlan,
			version:        unsupportedVersion,
			expectedResult: false,
		},
		{
			name:           "Bad license, good version",
			licensePlan:    "bad_license",
			version:        supportedVersion,
			expectedResult: false,
		},
		{
			name:           "Bad license, bad version",
			licensePlan:    "bad_license",
			version:        unsupportedVersion,
			expectedResult: false,
		},
		{
			name:           "Error API response",
			licensePlan:    "",
			version:        "",
			expectedError:  true,
			expectedResult: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mux, gitlabInstance := setupEmptyTestServer(t, RoleDestination)
			if !tt.expectedError {
				mux.HandleFunc("/api/v4/license", func(w http.ResponseWriter, r *http.Request) {
					if !requireGet(w, r) {
						return
					}

					w.WriteHeader(http.StatusOK)
					writeBody(w, `{"plan": "`+tt.licensePlan+`", "expired": false}`)
				})
				mux.HandleFunc("/api/v4/metadata", func(w http.ResponseWriter, r *http.Request) {
					if !requireGet(w, r) {
						return
					}

					w.WriteHeader(http.StatusOK)
					writeBody(w, `{"version": "`+tt.version+`"}`)
				})
			}

			pullMirrorAvailable, err := gitlabInstance.IsPullMirrorAvailable(&mirrorcfg.ParserArgs{ForcePremium: tt.forcePremium})
			if (err != nil) != tt.expectedError {
				t.Fatalf("CheckDestinationInstance() error = %v, expectedError %v", err, tt.expectedError)
			}
			if pullMirrorAvailable != tt.expectedResult {
				t.Errorf("CheckDestinationInstance() = %v, expectedResult %v", pullMirrorAvailable, tt.expectedResult)
			}
		})
	}
}

func TestMirrorGitlabsReportsBlockingError(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
	// A bad source URL must surface as a blocking error, i.e. exit code 1.
	helpers.ResetReported()
	t.Cleanup(helpers.ResetReported)

	args := &mirrorcfg.ParserArgs{
		SourceGitlabURL:        "invalid-url",
		SourceGitlabToken:      "token",
		DestinationGitlabURL:   "https://example.com",
		DestinationGitlabToken: "token",
		MirrorMapping:          &mirrorcfg.MirrorMapping{},
		Retry:                  1,
	}

	MirrorGitlabs(args)

	if got := helpers.ExitCode(); got != 1 {
		t.Errorf("Expected blocking exit code 1, got %d", got)
	}
}
