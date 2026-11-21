package mirroring

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

func TestProcessFilters(t *testing.T) {
	tests := []struct {
		name                              string
		mirrorMapping                     *mirrorcfg.MirrorMapping
		expectedSourceProjectFilters      map[string]struct{}
		expectedSourceGroupFilters        map[string]struct{}
		expectedDestinationProjectFilters map[string]struct{}
		expectedDestinationGroupFilters   map[string]struct{}
	}{
		{
			name: "EmptyMirrorMapping",
			mirrorMapping: &mirrorcfg.MirrorMapping{
				Projects: make(map[string]*mirrorcfg.MirroringOptions),
				Groups:   make(map[string]*mirrorcfg.MirroringOptions),
			},
			expectedSourceProjectFilters:      map[string]struct{}{},
			expectedSourceGroupFilters:        map[string]struct{}{},
			expectedDestinationProjectFilters: map[string]struct{}{},
			expectedDestinationGroupFilters:   map[string]struct{}{},
		},
		{
			name: "SingleProjectAndGroup",
			mirrorMapping: &mirrorcfg.MirrorMapping{
				Projects: map[string]*mirrorcfg.MirroringOptions{
					"sourceProject": {
						DestinationPath: "destinationGroupPath/destinationProjectPath",
						CICDCatalog:     new(true),
						MirrorIssues:    new(true),
					},
				},
				Groups: map[string]*mirrorcfg.MirroringOptions{
					"sourceGroup": {
						DestinationPath: "destinationGroupPath",
						CICDCatalog:     new(true),
						MirrorIssues:    new(true),
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
			mirrorMapping: &mirrorcfg.MirrorMapping{
				Projects: map[string]*mirrorcfg.MirroringOptions{
					"sourceProject1": {
						DestinationPath: "destinationGroupPath1/destinationProjectPath1",
						CICDCatalog:     new(true),
						MirrorIssues:    new(true),
					},
					"sourceProject2": {
						DestinationPath: "destinationGroupPath2/destinationProjectPath2",
						CICDCatalog:     new(false),
						MirrorIssues:    new(false),
					},
				},
				Groups: map[string]*mirrorcfg.MirroringOptions{
					"sourceGroup1": {
						DestinationPath: "destinationGroupPath3",
						CICDCatalog:     new(true),
						MirrorIssues:    new(true),
					},
					"sourceGroup2": {
						DestinationPath: "destinationGroupPath4",
						CICDCatalog:     new(false),
						MirrorIssues:    new(false),
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
		{
			name: "ProjectDestinationWithoutNamespaceDoesNotCreateDestinationGroupFilter",
			mirrorMapping: &mirrorcfg.MirrorMapping{
				Projects: map[string]*mirrorcfg.MirroringOptions{
					"sourceProject": {
						DestinationPath: "destinationProjectPath",
					},
				},
				Groups: map[string]*mirrorcfg.MirroringOptions{},
			},
			expectedSourceProjectFilters: map[string]struct{}{
				"sourceProject": {},
			},
			expectedSourceGroupFilters: map[string]struct{}{},
			expectedDestinationProjectFilters: map[string]struct{}{
				"destinationProjectPath": {},
			},
			expectedDestinationGroupFilters: map[string]struct{}{},
		},
		{
			name: "DuplicateDestinationGroupFromProjectAndGroupIsDeduplicated",
			mirrorMapping: &mirrorcfg.MirrorMapping{
				Projects: map[string]*mirrorcfg.MirroringOptions{
					"sourceProject": {
						DestinationPath: "destinationGroupPath/destinationProjectPath",
					},
				},
				Groups: map[string]*mirrorcfg.MirroringOptions{
					"sourceGroup": {
						DestinationPath: "destinationGroupPath",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			filters := processFilters(tt.mirrorMapping)
			sourceProjectFilters, sourceGroupFilters := filters.sourceProjects, filters.sourceGroups
			destinationProjectFilters, destinationGroupFilters := filters.destinationProjects, filters.destinationGroups

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
			sourceSize: InstanceSizeSmall,
		},
		{
			name:       "Dry Run Source Big",
			sourceSize: InstanceSizeBig,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, sourceGitlabInstance := setupTestServer(t, RoleSource, tt.sourceSize)
			_, destinationGitlabInstance := setupTestServer(t, RoleDestination, InstanceSizeSmall)
			gitlabMirrorArgs := &mirrorcfg.MirrorMapping{
				Projects: map[string]*mirrorcfg.MirroringOptions{
					TEST_PROJECT.PathWithNamespace: {
						DestinationPath: TEST_PROJECT.PathWithNamespace,
						MirrorReleases:  new(true),
					},
				},
				Groups: map[string]*mirrorcfg.MirroringOptions{
					TEST_GROUP_2.FullPath: {
						DestinationPath: TEST_GROUP_2.FullPath,
						MirrorReleases:  new(true),
					},
				},
			}
			sourceGitlabInstance.AddProject(TEST_PROJECT)
			sourceGitlabInstance.AddGroup(TEST_GROUP_2)
			sourceGitlabInstance.AddGroup(TEST_GROUP_2)

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
			mux, gitlabInstance := setupEmptyTestServer(t, RoleDestination, InstanceSizeSmall)
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

func TestMirrorGitlabsReportsBlockingError(t *testing.T) {
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
