package mirroring

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/boxboxjason/gitlab-sync/internal/utils"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
)

func TestFetchAll(t *testing.T) {
	tests := []struct {
		name          string
		instanceSize  string
		role          string
		expectedError bool
	}{
		{
			name:          "Fetch all Destination with small instance size",
			instanceSize:  INSTANCE_SIZE_SMALL,
			role:          ROLE_DESTINATION,
			expectedError: false,
		},
		{
			name:          "Fetch all Destination with big instance size",
			instanceSize:  INSTANCE_SIZE_BIG,
			role:          ROLE_DESTINATION,
			expectedError: false,
		},
		{
			name:          "Fetch all Source with small instance size",
			instanceSize:  INSTANCE_SIZE_SMALL,
			role:          ROLE_SOURCE,
			expectedError: false,
		},
		{
			name:          "Fetch all Source with big instance size",
			instanceSize:  INSTANCE_SIZE_BIG,
			role:          ROLE_SOURCE,
			expectedError: false,
		},
	}

	projectFilters := map[string]struct{}{
		TEST_PROJECT.PathWithNamespace: {},
	}
	groupFilters := map[string]struct{}{
		TEST_GROUP_2.FullPath: {},
	}
	gitlabMirrorArgs := &utils.MirrorMapping{
		Projects: map[string]*utils.MirroringOptions{},
		Groups:   map[string]*utils.MirroringOptions{},
	}
	gitlabMirrorArgs.Projects[TEST_PROJECT.PathWithNamespace] = &utils.MirroringOptions{
		DestinationPath: TEST_PROJECT.PathWithNamespace,
	}
	gitlabMirrorArgs.Groups[TEST_GROUP_2.FullPath] = &utils.MirroringOptions{
		DestinationPath: TEST_GROUP_2.FullPath,
	}

	// Iterate over the test cases
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, gitlabInstance := setupTestServer(t, test.role, test.instanceSize)

			// Call the function with the test case parameters
			err := gitlabInstance.FetchAll(projectFilters, groupFilters, gitlabMirrorArgs)

			// Check if an error was expected
			if (err != nil) != test.expectedError {
				t.Errorf(EXPECTED_ERROR_MESSAGE, test.expectedError, err)
			}

			// Check if the instance cache contains the expected projects and groups
			if _, ok := gitlabInstance.Projects[TEST_PROJECT.PathWithNamespace]; !ok {
				t.Errorf("expected project %s not found in %s %s instance cache", TEST_PROJECT.PathWithNamespace, gitlabInstance.Role, gitlabInstance.InstanceSize)
			}
			if _, ok := gitlabInstance.Groups[TEST_GROUP_2.FullPath]; !ok {
				t.Errorf("expected group %s not found in %s %s instance cache", TEST_GROUP_2.FullPath, gitlabInstance.Role, gitlabInstance.InstanceSize)
			}
		})
	}
}

func TestFetchAndProcessProjectsBigInstance(t *testing.T) {
	const customDestination = "custom/destination/project"

	tests := []struct {
		name             string
		role             string
		projectFilters   map[string]struct{}
		expectedProjects map[string]struct{}
		expectError      bool
		// When non-empty, assert that TEST_PROJECT's destination path in the
		// mirror mapping equals this value after the call.
		expectedMappingDestination string
	}{
		{
			name: "Test with source role, 1 project only, no error",
			role: ROLE_SOURCE,
			projectFilters: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace: {},
			},
			expectedProjects: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace: {},
			},
			expectedMappingDestination: customDestination,
		},
		{
			name: "Test with destination role, 1 project only, no error",
			role: ROLE_DESTINATION,
			projectFilters: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace: {},
			},
			expectedProjects: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace: {},
			},
		},
		{
			name: "Test with source role, 2 projects, no error",
			role: ROLE_SOURCE,
			projectFilters: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace:   {},
				TEST_PROJECT_2.PathWithNamespace: {},
			},
			expectedProjects: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace:   {},
				TEST_PROJECT_2.PathWithNamespace: {},
			},
			expectedMappingDestination: customDestination,
		},
		{
			name: "Test with destination role, 2 projects, no error",
			role: ROLE_DESTINATION,
			projectFilters: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace:   {},
				TEST_PROJECT_2.PathWithNamespace: {},
			},
			expectedProjects: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace:   {},
				TEST_PROJECT_2.PathWithNamespace: {},
			},
		},
		{
			name: "Test with source role, 1 project, 1 error",
			role: ROLE_SOURCE,
			projectFilters: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace:    {},
				INVALID_PROJECT.PathWithNamespace: {},
			},
			expectedProjects: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace: {},
			},
			expectError:                true,
			expectedMappingDestination: customDestination,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			gitlabMirrorArgs := &utils.MirrorMapping{
				Projects: map[string]*utils.MirroringOptions{
					TEST_PROJECT.PathWithNamespace: {
						DestinationPath: customDestination,
					},
				},
			}

			_, gitlabInstance := setupTestServer(t, test.role, INSTANCE_SIZE_BIG)

			err := gitlabInstance.FetchAndProcessProjectsBigInstance(&test.projectFilters, gitlabMirrorArgs)
			if (err != nil) != test.expectError {
				t.Fatalf("Expected error: %v, got: %v", test.expectError, err)
			}
			if len(gitlabInstance.Projects) != len(test.expectedProjects) {
				t.Fatalf("Expected %d projects, got %d", len(test.expectedProjects), len(gitlabInstance.Projects))
			}
			for project := range test.expectedProjects {
				if _, ok := gitlabInstance.Projects[project]; !ok {
					t.Errorf("Expected project %s to be in the cache", project)
				}
			}

			// For source-role cases the configured destination path must not be overwritten.
			if test.expectedMappingDestination != "" {
				opts, ok := gitlabMirrorArgs.GetProject(TEST_PROJECT.PathWithNamespace)
				if !ok {
					t.Fatalf("expected %q to remain in mirror mapping after FetchAndProcessProjectsBigInstance", TEST_PROJECT.PathWithNamespace)
				}
				if opts.DestinationPath != test.expectedMappingDestination {
					t.Errorf("mirror mapping destination path: got %q, want %q", opts.DestinationPath, test.expectedMappingDestination)
				}
			}
		})
	}
}

// TestFetchAndProcessProjectsSmallInstance verifies that projects listed directly
// in the mirror-mapping config are mirrored correctly in small-instance mode,
// i.e. without requiring a matching group entry (the same root-cause as the big
// instance bug but triggered via MatchPathAgainstFilters returning "" as the
// parent group path for an exact allowList hit).
func TestFetchAndProcessProjectsSmallInstance(t *testing.T) {
	const customDestination = "custom/destination/project"

	tests := []struct {
		name                       string
		role                       string
		projectFilters             map[string]struct{}
		groupFilters               map[string]struct{}
		mirrorMapping              *utils.MirrorMapping
		expectedProjects           map[string]struct{}
		expectedMappingDestination string
	}{
		{
			name: "source role, project-only config (no groups), preserves destination path",
			role: ROLE_SOURCE,
			projectFilters: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace: {},
			},
			groupFilters: map[string]struct{}{},
			mirrorMapping: &utils.MirrorMapping{
				Projects: map[string]*utils.MirroringOptions{
					TEST_PROJECT.PathWithNamespace: {DestinationPath: customDestination},
				},
				Groups: map[string]*utils.MirroringOptions{},
			},
			expectedProjects: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace: {},
			},
			expectedMappingDestination: customDestination,
		},
		{
			name:           "source role, group-only config, derives destination path from group",
			role:           ROLE_SOURCE,
			projectFilters: map[string]struct{}{},
			groupFilters: map[string]struct{}{
				TEST_GROUP.FullPath: {},
			},
			mirrorMapping: &utils.MirrorMapping{
				Projects: map[string]*utils.MirroringOptions{},
				Groups: map[string]*utils.MirroringOptions{
					TEST_GROUP.FullPath: {DestinationPath: "mirror-group"},
				},
			},
			// TEST_PROJECT.PathWithNamespace is "test/group/project"; relative to
			// TEST_GROUP.FullPath ("test/group") that is "project", so the expected
			// destination is "mirror-group/project".
			expectedProjects: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace: {},
			},
			expectedMappingDestination: "mirror-group/project",
		},
		{
			name: "destination role, project-only config, project in cache, mapping unchanged",
			role: ROLE_DESTINATION,
			projectFilters: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace: {},
			},
			groupFilters: map[string]struct{}{},
			mirrorMapping: &utils.MirrorMapping{
				Projects: map[string]*utils.MirroringOptions{},
				Groups:   map[string]*utils.MirroringOptions{},
			},
			expectedProjects: map[string]struct{}{
				TEST_PROJECT.PathWithNamespace: {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, gitlabInstance := setupTestServer(t, tt.role, INSTANCE_SIZE_SMALL)

			errs := gitlabInstance.FetchAndProcessProjectsSmallInstance(&tt.projectFilters, &tt.groupFilters, tt.mirrorMapping)
			if len(errs) > 0 {
				t.Fatalf("unexpected errors: %v", errs)
			}

			for project := range tt.expectedProjects {
				if _, ok := gitlabInstance.Projects[project]; !ok {
					t.Errorf("expected project %q in instance cache", project)
				}
			}

			if tt.expectedMappingDestination != "" {
				opts, ok := tt.mirrorMapping.GetProject(TEST_PROJECT.PathWithNamespace)
				if !ok {
					t.Fatalf("expected %q in mirror mapping after FetchAndProcessProjectsSmallInstance", TEST_PROJECT.PathWithNamespace)
				}
				if opts.DestinationPath != tt.expectedMappingDestination {
					t.Errorf("mirror mapping destination path: got %q, want %q", opts.DestinationPath, tt.expectedMappingDestination)
				}
			}
		})
	}
}

func TestStoreProject(t *testing.T) {
	tests := []struct {
		name                    string
		role                    string
		parentGroupPath         string
		mirrorMapping           *utils.MirrorMapping
		expectedDestinationPath string // empty means no entry expected in mirrorMapping.Projects
	}{
		{
			// Core bug fix: a project listed directly in the config must not require
			// a group entry and must preserve its configured destination path.
			name:            "source role, project directly in mirror mapping, preserves destination without group",
			role:            ROLE_SOURCE,
			parentGroupPath: "",
			mirrorMapping: &utils.MirrorMapping{
				Projects: map[string]*utils.MirroringOptions{
					TEST_PROJECT.PathWithNamespace: {DestinationPath: "custom/dest/project"},
				},
				Groups: map[string]*utils.MirroringOptions{},
			},
			expectedDestinationPath: "custom/dest/project",
		},
		{
			// Existing group-derived behaviour must still work.
			name:            "source role, project matched via parent group, derives destination path",
			role:            ROLE_SOURCE,
			parentGroupPath: TEST_GROUP.FullPath,
			mirrorMapping: &utils.MirrorMapping{
				Projects: map[string]*utils.MirroringOptions{},
				Groups: map[string]*utils.MirroringOptions{
					TEST_GROUP.FullPath: {DestinationPath: "mirror-group"},
				},
			},
			// filepath.Join("mirror-group", filepath.Rel("test/group", "test/group/project")) == "mirror-group/project"
			expectedDestinationPath: "mirror-group/project",
		},
		{
			// Project not in mapping and no matching group: project enters the instance
			// cache but is not added to the mirror mapping.
			name:            "source role, no project or group entry, not added to mirror mapping",
			role:            ROLE_SOURCE,
			parentGroupPath: "nonexistent/group",
			mirrorMapping: &utils.MirrorMapping{
				Projects: map[string]*utils.MirroringOptions{},
				Groups:   map[string]*utils.MirroringOptions{},
			},
			expectedDestinationPath: "",
		},
		{
			// Destination role never touches the mirror mapping.
			name:            "destination role, project stored in cache, mirror mapping unchanged",
			role:            ROLE_DESTINATION,
			parentGroupPath: "",
			mirrorMapping: &utils.MirrorMapping{
				Projects: map[string]*utils.MirroringOptions{},
				Groups:   map[string]*utils.MirroringOptions{},
			},
			expectedDestinationPath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, gitlabInstance := setupTestServer(t, tt.role, INSTANCE_SIZE_BIG)

			gitlabInstance.storeProject(TEST_PROJECT, tt.parentGroupPath, tt.mirrorMapping)

			if _, ok := gitlabInstance.Projects[TEST_PROJECT.PathWithNamespace]; !ok {
				t.Errorf("expected project %q in instance cache after storeProject", TEST_PROJECT.PathWithNamespace)
			}

			opts, inMapping := tt.mirrorMapping.GetProject(TEST_PROJECT.PathWithNamespace)
			if tt.expectedDestinationPath == "" {
				if inMapping && opts != nil && opts.DestinationPath != "" {
					t.Errorf("expected no mirror-mapping entry for project, but got destination path %q", opts.DestinationPath)
				}
			} else {
				if !inMapping {
					t.Fatalf("expected project to be in mirror mapping with destination %q, but it was absent", tt.expectedDestinationPath)
				}
				if opts.DestinationPath != tt.expectedDestinationPath {
					t.Errorf("expected destination path %q, got %q", tt.expectedDestinationPath, opts.DestinationPath)
				}
			}
		})
	}
}

func TestCreateProjectFromSource(t *testing.T) {
	tests := []struct {
		name         string
		instanceSize string
		role         string
	}{
		{
			name:         "Small Destination Instance",
			instanceSize: "small",
			role:         ROLE_DESTINATION,
		},
		{
			name:         "Small Source Instance",
			instanceSize: "small",
			role:         ROLE_SOURCE,
		},
		{
			name:         "Big Destination Instance",
			instanceSize: "big",
			role:         ROLE_DESTINATION,
		},
		{
			name:         "Big Source Instance",
			instanceSize: "big",
			role:         ROLE_SOURCE,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup the test server
			_, gitlabInstance := setupTestServer(t, tt.role, tt.instanceSize)
			gitlabInstance.AddGroup(TEST_GROUP)
			createdProject, err := gitlabInstance.CreateProjectFromSource(TEST_PROJECT, &utils.MirroringOptions{
				DestinationPath:     TEST_PROJECT.PathWithNamespace,
				MirrorIssues:        new(true),
				MirrorReleases:      new(true),
				MirrorTriggerBuilds: new(true),
				Visibility:          new("public"),
				CI_CD_Catalog:       new(true),
			})
			if err != nil {
				t.Errorf("Unexpected error when creating project: %v", err)
			}
			if createdProject == nil {
				t.Fatal("Expected created project to be non-nil")
			}
			if createdProject.PathWithNamespace != TEST_PROJECT.PathWithNamespace {
				t.Errorf("Expected created project path to be %s, got %s", TEST_PROJECT.PathWithNamespace, createdProject.PathWithNamespace)
			}
		})
	}
}

func TestCreateProjectFromSourceWithMinimalOptions(t *testing.T) {
	_, gitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
	gitlabInstance.AddGroup(TEST_GROUP)

	createdProject, err := gitlabInstance.CreateProjectFromSource(TEST_PROJECT, &utils.MirroringOptions{
		DestinationPath: TEST_PROJECT.PathWithNamespace,
	})
	if err != nil {
		t.Fatalf("unexpected error when creating project with minimal options: %v", err)
	}
	if createdProject == nil {
		t.Fatal("expected created project to be non-nil")
	}
	if createdProject.PathWithNamespace != TEST_PROJECT.PathWithNamespace {
		t.Errorf("expected created project path to be %s, got %s", TEST_PROJECT.PathWithNamespace, createdProject.PathWithNamespace)
	}
}

func TestCopyProjectAvatar(t *testing.T) {
	_, sourceGitlabInstance := setupTestServer(t, ROLE_SOURCE, INSTANCE_SIZE_SMALL)
	_, destinationGitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)

	t.Run("Copy Project Avatar", func(t *testing.T) {
		sourceProject := *TEST_PROJECT_2
		sourceProject.AvatarURL = "http://gitlab.example.com/uploads/avatar.png"

		err := sourceGitlabInstance.CopyProjectAvatar(destinationGitlabInstance, TEST_PROJECT, &sourceProject)
		if err != nil {
			t.Errorf("Unexpected error when copying project avatar: %v", err)
		}
	})

	t.Run("Source project without an avatar is skipped", func(t *testing.T) {
		// The avatar endpoint answers 404 for avatar-less projects, so the download
		// must not even be attempted.
		_, isolatedSource := setupEmptyTestServer(t, ROLE_SOURCE, INSTANCE_SIZE_SMALL)

		err := isolatedSource.CopyProjectAvatar(destinationGitlabInstance, TEST_PROJECT, TEST_PROJECT_2)
		if err != nil {
			t.Errorf("Unexpected error when skipping avatar copy: %v", err)
		}
	})
}

func TestCreateProjects(t *testing.T) {
	t.Run("Test Create Projects", func(t *testing.T) {
		_, sourceGitlabInstance := setupTestServer(t, ROLE_SOURCE, INSTANCE_SIZE_SMALL)
		sourceGitlabInstance.AddGroup(TEST_GROUP)
		sourceGitlabInstance.AddProject(TEST_PROJECT)
		_, destinationGitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
		destinationGitlabInstance.AddGroup(TEST_GROUP)
		destinationGitlabInstance.PullMirrorAvailable = true
		mirrorMapping := &utils.MirrorMapping{
			Projects: map[string]*utils.MirroringOptions{
				TEST_PROJECT.PathWithNamespace: {
					DestinationPath:     TEST_PROJECT.PathWithNamespace,
					CI_CD_Catalog:       new(false),
					MirrorIssues:        new(true),
					MirrorTriggerBuilds: new(false),
					Visibility:          new("public"),
					MirrorReleases:      new(true),
				},
			},
		}
		err := destinationGitlabInstance.CreateProjects(sourceGitlabInstance, mirrorMapping)
		if len(err) > 0 {
			t.Errorf("Unexpected error when creating projects: %v", err)
		}
		if len(destinationGitlabInstance.Projects) == 0 {
			t.Errorf("Expected projects to be created, but none were found")
		}
	})
}

func TestAddProjectToCICDCatalog(t *testing.T) {
	_, gitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
	t.Run("Add Project to CI/CD Catalog", func(t *testing.T) {
		err := gitlabInstance.AddProjectToCICDCatalog(TEST_PROJECT)
		if err != nil {
			t.Errorf("Unexpected error when adding project to CI/CD catalog: %v", err)
		}
	})

	t.Run("Skip Add Project to CI/CD Catalog when already enabled", func(t *testing.T) {
		// This project ID has no registered endpoint on the test server, so an
		// unexpected error here would reveal that the skip check failed to
		// prevent the API call.
		alreadyEnabledProject := &gitlab.Project{
			ID:                 999999,
			PathWithNamespace:  "test/group/unregistered-project",
			CICDCatalogEnabled: true,
		}

		err := gitlabInstance.AddProjectToCICDCatalog(alreadyEnabledProject)
		if err != nil {
			t.Errorf("Unexpected error when project is already in the CI/CD catalog: %v", err)
		}
	})
}

func TestClaimOwnershipToProject(t *testing.T) {
	_, gitlabInstance := setupTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)

	err := gitlabInstance.ClaimOwnershipToProject(TEST_PROJECT)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestCreateProjectClaimOwnershipOption(t *testing.T) {
	tests := []struct {
		name                 string
		claimOwnership       *bool
		preExisting          bool
		expectedMemberClaims int
	}{
		{
			name:                 "claim ownership omitted defaults to no claim",
			claimOwnership:       nil,
			expectedMemberClaims: 0,
		},
		{
			name:                 "claim ownership false does not claim",
			claimOwnership:       new(false),
			expectedMemberClaims: 0,
		},
		{
			name:                 "claim ownership true claims ownership when the project is created",
			claimOwnership:       new(true),
			expectedMemberClaims: 1,
		},
		{
			name:                 "claim ownership true reclaims ownership on an already existing project",
			claimOwnership:       new(true),
			preExisting:          true,
			expectedMemberClaims: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, sourceGitlabInstance := setupTestServer(t, ROLE_SOURCE, INSTANCE_SIZE_SMALL)
			sourceGitlabInstance.AddProject(TEST_PROJECT_2)

			mux, destinationGitlabInstance := setupEmptyTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
			destinationGitlabInstance.PullMirrorAvailable = true
			destinationGitlabInstance.AddGroup(TEST_GROUP_2)

			memberClaims := 0

			mux.HandleFunc("/api/v4/projects", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				writeJSONResponse(w, http.StatusCreated, TEST_PROJECT_2_STRING)
			})
			mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d", TEST_PROJECT_2.ID), func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet, http.MethodPut:
					writeJSONResponse(w, http.StatusOK, TEST_PROJECT_2_STRING)
				default:
					writeMethodNotAllowed(w)
				}
			})
			mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d/mirror/pull", TEST_PROJECT_2.ID), func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					writeMethodNotAllowed(w)
					return
				}
				writeJSONResponse(w, http.StatusOK, "{}")
			})
			// The edit-member endpoint is intentionally left unregistered so ClaimOwnershipToProject's
			// EditProjectMember attempt 404s and falls back to AddProjectMember below.
			mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d/members", TEST_PROJECT_2.ID), func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					writeMethodNotAllowed(w)
					return
				}
				memberClaims++
				writeJSONResponse(w, http.StatusCreated, `{"id": 1, "access_level": 50}`)
			})

			if tc.preExisting {
				destinationGitlabInstance.AddProject(TEST_PROJECT_2)
			}

			createdProject, errs := destinationGitlabInstance.CreateProject(TEST_PROJECT_2.PathWithNamespace, &utils.MirroringOptions{
				DestinationPath: TEST_PROJECT_2.PathWithNamespace,
				ClaimOwnership:  tc.claimOwnership,
			}, sourceGitlabInstance)
			if len(errs) > 0 {
				t.Fatalf("unexpected error: %v", errs)
			}
			if createdProject == nil {
				t.Fatal("expected created project to be non-nil")
			}
			if memberClaims != tc.expectedMemberClaims {
				t.Fatalf("expected %d ownership claims, got %d", tc.expectedMemberClaims, memberClaims)
			}
		})
	}
}

func TestSyncMirrorProjectAttributes(t *testing.T) {
	tests := []struct {
		name                     string
		pullMirrorAvailable      bool
		destinationProject       *gitlab.Project
		copyOptions              *utils.MirroringOptions
		expectedMismatch         bool
		expectedMirror           *bool
		expectMirrorTriggerBuild bool
	}{
		{
			// Pull mirroring is a Premium feature and GitLab refuses mirror=true
			// without an import URL and a mirror user, so a freemium destination must
			// be left untouched: gitlab-sync pushes the repository itself instead.
			name:                "freemium destination leaves the mirror attributes alone",
			pullMirrorAvailable: false,
			destinationProject:  &gitlab.Project{ID: 1, Mirror: false, MirrorTriggerBuilds: false},
			copyOptions:         &utils.MirroringOptions{MirrorTriggerBuilds: new(true)},
			expectedMismatch:    false,
			expectedMirror:      nil,
		},
		{
			name:                "freemium destination does not re-enable an existing pull mirror",
			pullMirrorAvailable: false,
			destinationProject:  &gitlab.Project{ID: 1, Mirror: true, MirrorOverwritesDivergedBranches: true},
			copyOptions:         &utils.MirroringOptions{},
			expectedMismatch:    false,
			expectedMirror:      nil,
		},
		{
			name:                     "premium destination enables the pull mirror",
			pullMirrorAvailable:      true,
			destinationProject:       &gitlab.Project{ID: 1, Mirror: false},
			copyOptions:              &utils.MirroringOptions{MirrorTriggerBuilds: new(true)},
			expectedMismatch:         true,
			expectedMirror:           new(true),
			expectMirrorTriggerBuild: true,
		},
		{
			name:                "premium destination already in sync",
			pullMirrorAvailable: true,
			destinationProject:  &gitlab.Project{ID: 1, Mirror: true, MirrorOverwritesDivergedBranches: true},
			copyOptions:         &utils.MirroringOptions{},
			expectedMismatch:    false,
			expectedMirror:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			editOptions := &gitlab.EditProjectOptions{}

			mismatch := syncMirrorProjectAttributes(tt.pullMirrorAvailable, tt.destinationProject, tt.copyOptions, editOptions)
			if mismatch != tt.expectedMismatch {
				t.Errorf("expected mismatch %v, got %v", tt.expectedMismatch, mismatch)
			}

			switch {
			case tt.expectedMirror == nil && editOptions.Mirror != nil:
				t.Errorf("expected Mirror to be left unset, got %v", *editOptions.Mirror)
			case tt.expectedMirror != nil && editOptions.Mirror == nil:
				t.Errorf("expected Mirror to be set to %v, got nil", *tt.expectedMirror)
			case tt.expectedMirror != nil && *editOptions.Mirror != *tt.expectedMirror:
				t.Errorf("expected Mirror %v, got %v", *tt.expectedMirror, *editOptions.Mirror)
			}

			if !tt.expectMirrorTriggerBuild && editOptions.MirrorTriggerBuilds != nil {
				t.Errorf("expected MirrorTriggerBuilds to be left unset, got %v", *editOptions.MirrorTriggerBuilds)
			}
		})
	}
}

func TestCreateProjectFromSourceDoesNotRequestMirror(t *testing.T) {
	// GitLab validates mirror=true against the presence of an import URL and a
	// mirror user, neither of which exists when the project is being created.
	for _, pullMirrorAvailable := range []bool{false, true} {
		t.Run(fmt.Sprintf("pullMirrorAvailable=%v", pullMirrorAvailable), func(t *testing.T) {
			mux, gitlabInstance := setupEmptyTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)
			gitlabInstance.PullMirrorAvailable = pullMirrorAvailable
			gitlabInstance.AddGroup(TEST_GROUP)

			var createBody map[string]any

			mux.HandleFunc("/api/v4/projects", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					w.WriteHeader(http.StatusMethodNotAllowed)

					return
				}

				if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
					t.Errorf("failed to decode project creation body: %v", err)
				}

				w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
				w.WriteHeader(http.StatusCreated)
				fmt.Fprint(w, TEST_PROJECT_STRING)
			})

			_, err := gitlabInstance.CreateProjectFromSource(TEST_PROJECT, &utils.MirroringOptions{
				DestinationPath:     TEST_PROJECT.PathWithNamespace,
				MirrorTriggerBuilds: new(true),
			})
			if err != nil {
				t.Fatalf("unexpected error when creating project: %v", err)
			}

			if _, ok := createBody["mirror"]; ok {
				t.Errorf("project creation must not send the mirror attribute, got %v", createBody["mirror"])
			}

			_, sentTriggerBuilds := createBody["mirror_trigger_builds"]
			if sentTriggerBuilds != pullMirrorAvailable {
				t.Errorf("expected mirror_trigger_builds to be sent = %v, got %v", pullMirrorAvailable, sentTriggerBuilds)
			}
		})
	}
}

func TestDisableProjectMirrorPull(t *testing.T) {
	t.Run("no call when the project is not a pull mirror", func(t *testing.T) {
		_, gitlabInstance := setupEmptyTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)

		project := &gitlab.Project{ID: 1, PathWithNamespace: TEST_PROJECT.PathWithNamespace, Mirror: false}
		if err := gitlabInstance.DisableProjectMirrorPull(project); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("turns an existing pull mirror off before pushing", func(t *testing.T) {
		mux, gitlabInstance := setupEmptyTestServer(t, ROLE_DESTINATION, INSTANCE_SIZE_SMALL)

		var editBody map[string]any

		mux.HandleFunc("/api/v4/projects/1", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPut {
				w.WriteHeader(http.StatusMethodNotAllowed)

				return
			}

			if err := json.NewDecoder(r.Body).Decode(&editBody); err != nil {
				t.Errorf("failed to decode project edit body: %v", err)
			}

			w.Header().Set(HEADER_CONTENT_TYPE, HEADER_ACCEPT)
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, TEST_PROJECT_STRING)
		})

		project := &gitlab.Project{ID: 1, PathWithNamespace: TEST_PROJECT.PathWithNamespace, Mirror: true}
		if err := gitlabInstance.DisableProjectMirrorPull(project); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if mirror, ok := editBody["mirror"].(bool); !ok || mirror {
			t.Errorf("expected the edit request to set mirror=false, got %v", editBody["mirror"])
		}

		if project.Mirror {
			t.Error("expected the cached project to no longer be flagged as a mirror")
		}
	})
}
