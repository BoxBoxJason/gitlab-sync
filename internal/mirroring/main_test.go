package mirroring

import (
	"net/http"
	"reflect"
	"testing"

	"gitlab-sync/internal/utils"
)

func TestProcessFilters(t *testing.T) {
	tests := []struct {
		name                              string
		mirrorMapping                     *utils.MirrorMapping
		expectedSourceProjectFilters      map[string]struct{}
		expectedSourceGroupFilters        map[string]struct{}
		expectedDestinationProjectFilters map[string]struct{}
		expectedDestinationGroupFilters   map[string]struct{}
	}{
		{
			name: "EmptyMirrorMapping",
			mirrorMapping: &utils.MirrorMapping{
				Projects: make(map[string]*utils.MirroringOptions),
				Groups:   make(map[string]*utils.MirroringOptions),
			},
			expectedSourceProjectFilters:      map[string]struct{}{},
			expectedSourceGroupFilters:        map[string]struct{}{},
			expectedDestinationProjectFilters: map[string]struct{}{},
			expectedDestinationGroupFilters:   map[string]struct{}{},
		},
		{
			name: "SingleProjectAndGroup",
			mirrorMapping: &utils.MirrorMapping{
				Projects: map[string]*utils.MirroringOptions{
					"sourceProject": {
						DestinationPath: "destinationGroupPath/destinationProjectPath",
						CI_CD_Catalog:   true,
						Issues:          true,
					},
				},
				Groups: map[string]*utils.MirroringOptions{
					"sourceGroup": {
						DestinationPath: "destinationGroupPath",
						CI_CD_Catalog:   true,
						Issues:          true,
					},
				},
			},
			expectedSourceProjectFilters: map[string]struct{}{
				"sourceProject": {},
			},
			expectedSourceGroupFilters: map[string]struct{}{
				"sourceGroup": {},
			},
			expectedDestinationProjectFilters: map[string]struct{}{
				"destinationGroupPath/destinationProjectPath": {},
			},
			expectedDestinationGroupFilters: map[string]struct{}{
				"destinationGroupPath": {},
			},
		},
		{
			name: "MultipleProjectsAndGroups",
			mirrorMapping: &utils.MirrorMapping{
				Projects: map[string]*utils.MirroringOptions{
					"sourceProject1": {
						DestinationPath: "destinationGroupPath1/destinationProjectPath1",
						CI_CD_Catalog:   true,
						Issues:          true,
					},
					"sourceProject2": {
						DestinationPath: "destinationGroupPath2/destinationProjectPath2",
						CI_CD_Catalog:   false,
						Issues:          false,
					},
				},
				Groups: map[string]*utils.MirroringOptions{
					"sourceGroup1": {
						DestinationPath: "destinationGroupPath3",
						CI_CD_Catalog:   true,
						Issues:          true,
					},
					"sourceGroup2": {
						DestinationPath: "destinationGroupPath4",
						CI_CD_Catalog:   false,
						Issues:          false,
					},
				},
			},
			expectedSourceProjectFilters: map[string]struct{}{
				"sourceProject1": {},
				"sourceProject2": {},
			},
			expectedSourceGroupFilters: map[string]struct{}{
				"sourceGroup1": {},
				"sourceGroup2": {},
			},
			expectedDestinationProjectFilters: map[string]struct{}{
				"destinationGroupPath1/destinationProjectPath1": {},
				"destinationGroupPath2/destinationProjectPath2": {},
			},
			expectedDestinationGroupFilters: map[string]struct{}{
				"destinationGroupPath1": {},
				"destinationGroupPath2": {},
				"destinationGroupPath3": {},
				"destinationGroupPath4": {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sourceProjectFilters, sourceGroupFilters, destinationProjectFilters, destinationGroupFilters := processFilters(tt.mirrorMapping)

			if !reflect.DeepEqual(sourceProjectFilters, tt.expectedSourceProjectFilters) {
				t.Errorf("expected sourceProjectFilters %v, got %v", tt.expectedSourceProjectFilters, sourceProjectFilters)
			}

			if !reflect.DeepEqual(sourceGroupFilters, tt.expectedSourceGroupFilters) {
				t.Errorf("expected sourceGroupFilters %v, got %v", tt.expectedSourceGroupFilters, sourceGroupFilters)
			}

			if !reflect.DeepEqual(destinationProjectFilters, tt.expectedDestinationProjectFilters) {
				t.Errorf("expected destinationProjectFilters %v, got %v", tt.expectedDestinationProjectFilters, destinationProjectFilters)
			}

			if !reflect.DeepEqual(destinationGroupFilters, tt.expectedDestinationGroupFilters) {
				t.Errorf("expected destinationGroupFilters %v, got %v", tt.expectedDestinationGroupFilters, destinationGroupFilters)
			}
		})
	}
}

func TestDryRun(t *testing.T) {
	tests := []struct {
		name       string
		sourceSize string
	}{
		{
			name:       "Dry Run Source Small",
			sourceSize: INSTANCE_SIZE_SMALL,
		},
		{
			name:       "Dry Run Source Big",
			sourceSize: INSTANCE_SIZE_BIG,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, sourceGitlabInstance := setupTestServer(t, ROLE_SOURCE, tt.sourceSize)
			_, destinationGitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
			gitlabMirrorArgs := &utils.MirrorMapping{
				Projects: map[string]*utils.MirroringOptions{
					TEST_PROJECT.PathWithNamespace: {
						DestinationPath: TEST_PROJECT.PathWithNamespace,
						MirrorReleases:  true,
					},
				},
				Groups: map[string]*utils.MirroringOptions{
					TEST_GROUP_2.FullPath: {
						DestinationPath: TEST_GROUP_2.FullPath,
						MirrorReleases:  true,
					},
				},
			}
			sourceGitlabInstance.addProject(TEST_PROJECT)
			sourceGitlabInstance.addGroup(TEST_GROUP_2)
			sourceGitlabInstance.addGroup(TEST_GROUP_2)

			destinationGitlabInstance.DryRun(sourceGitlabInstance, gitlabMirrorArgs)
		})
	}
}

func TestIsPullMirrorAvailable(t *testing.T) {
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
			licensePlan:    PREMIUM_PLAN,
			version:        supportedVersion,
			expectedResult: true,
		},
		{
			name:           "Ultimate license, good version",
			licensePlan:    ULTIMATE_PLAN,
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
			licensePlan:    PREMIUM_PLAN,
			version:        unsupportedVersion,
			expectedResult: false,
		},
		{
			name:           "Ultimate license, bad version",
			licensePlan:    ULTIMATE_PLAN,
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
			mux, gitlabInstance := setupEmptyTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
			if !tt.expectedError {
				mux.HandleFunc("/api/v4/license", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(200)
					w.Write([]byte(`{"plan": "` + tt.licensePlan + `", "expired": false}`))
				})
				mux.HandleFunc("/api/v4/metadata", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(200)
					w.Write([]byte(`{"version": "` + tt.version + `"}`))
				})
			}

			pullMirrorAvailable, err := gitlabInstance.IsPullMirrorAvailable(tt.forcePremium)
			if (err != nil) != tt.expectedError {
				t.Fatalf("CheckDestinationInstance() error = %v, expectedError %v", err, tt.expectedError)
			}
			if pullMirrorAvailable != tt.expectedResult {
				t.Errorf("CheckDestinationInstance() = %v, expectedResult %v", pullMirrorAvailable, tt.expectedResult)
			}
		})
	}
}
