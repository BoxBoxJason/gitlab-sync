package mirroring

import (
	"net/http"
	"testing"
)

func TestIsVersionGreaterThanThreshold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		version          string
		expectedError    bool
		expectedResponse bool
		noAPIResponse    bool
	}{
		{
			name:             "Valid version under threshold",
			version:          "15.0.0",
			expectedError:    false,
			expectedResponse: false,
		},
		{
			name:             "Valid version above threshold",
			version:          "17.9.3-ce.0",
			expectedError:    false,
			expectedResponse: true,
		},
		{
			name:             "Invalid version format with 1 dot",
			version:          "invalid.version",
			expectedError:    true,
			expectedResponse: false,
		},
		{
			name:             "Invalid version format with 2 dots",
			version:          "invalid.version.1",
			expectedError:    true,
			expectedResponse: false,
		},
		{
			name:             "Invalid empty version",
			version:          "",
			expectedError:    true,
			expectedResponse: false,
		},
		{
			name:             "No API response",
			version:          "",
			expectedError:    true,
			expectedResponse: false,
			noAPIResponse:    true,
		},
	}

	// Iterate over the test cases
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			mux, gitlabInstance := setupEmptyTestServer(t, RoleDestination)
			if !test.noAPIResponse {
				mux.HandleFunc("/api/v4/metadata", func(w http.ResponseWriter, r *http.Request) {
					if !requireGet(w, r) {
						return
					}

					w.Header().Set(headerContentType, headerAccept)
					w.WriteHeader(http.StatusOK)
					_, err := w.Write([]byte(`{"version": "` + test.version + `"}`))
					if err != nil {
						t.Errorf("failed to write response: %v", err)
					}
				})
			}

			thresholdOk, err := gitlabInstance.IsVersionGreaterThanThreshold()
			if (err != nil) != test.expectedError {
				t.Fatalf(expectedErrorMessage, test.expectedError, err)
			}
			if thresholdOk != test.expectedResponse {
				t.Errorf("expected thresholdOk: %v, got: %v", test.expectedResponse, thresholdOk)
			}
		})
	}
}

func TestIsLicensePremium(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		license          string
		status           int
		noEndpoint       bool
		expectedError    bool
		expectedResponse bool
	}{
		{
			name:             "Ultimate tier license",
			license:          UltimatePlan,
			expectedError:    false,
			expectedResponse: true,
		},
		{
			name:             "Premium tier license",
			license:          PremiumPlan,
			expectedError:    false,
			expectedResponse: true,
		},
		{
			name:             "Free tier license",
			license:          "free",
			expectedError:    false,
			expectedResponse: false,
		},
		{
			name:             "Invalid license",
			license:          "invalid",
			expectedError:    false,
			expectedResponse: false,
		},
		{
			name:             "Error API response",
			status:           http.StatusInternalServerError,
			expectedError:    true,
			expectedResponse: false,
		},
		{
			// GitLab Community Edition answers 404 on the license endpoint.
			name:             "Community Edition without the license endpoint",
			noEndpoint:       true,
			expectedError:    false,
			expectedResponse: false,
		},
	}
	// Iterate over the test cases
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			mux, gitlabInstance := setupEmptyTestServer(t, RoleDestination)
			if !test.noEndpoint {
				mux.HandleFunc("/api/v4/license", func(w http.ResponseWriter, r *http.Request) {
					if !requireGet(w, r) {
						return
					}

					if test.status != 0 {
						w.WriteHeader(test.status)

						return
					}

					w.Header().Set(headerContentType, headerAccept)
					w.WriteHeader(http.StatusOK)
					_, err := w.Write([]byte(`{"plan": "` + test.license + `"}`))
					if err != nil {
						t.Errorf("failed to write response: %v", err)
					}
				})
			}

			isPremium, err := gitlabInstance.IsLicensePremium()
			if (err != nil) != test.expectedError {
				t.Fatalf(expectedErrorMessage, test.expectedError, err)
			}
			if isPremium != test.expectedResponse {
				t.Errorf("expected isPremium: %v, got: %v", test.expectedResponse, isPremium)
			}
		})
	}
}
