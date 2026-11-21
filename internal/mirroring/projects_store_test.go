package mirroring

import (
	"testing"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

// TestFetchAndProcessProjectsSmallInstance verifies that projects listed directly
// in the mirror-mapping config are mirrored correctly in small-instance mode,
// i.e. without requiring a matching group entry (the same root-cause as the big
// instance bug but triggered via MatchPathAgainstFilters returning "" as the
// parent group path for an exact allowList hit).
func TestFetchAndProcessProjectsSmallInstance(t *testing.T) { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
	const customDestination = "custom/destination/project"

	tests := []struct {
		name                       string
		role                       string
		projectFilters             map[string]struct{}
		groupFilters               map[string]struct{}
		mirrorMapping              *mirrorcfg.MirrorMapping
		expectedProjects           map[string]struct{}
		expectedMappingDestination string
	}{
		{
			name: "source role, project-only config (no groups), preserves destination path",
			role: RoleSource,
			projectFilters: map[string]struct{}{
				testProject.PathWithNamespace: {},
			},
			groupFilters: map[string]struct{}{},
			mirrorMapping: &mirrorcfg.MirrorMapping{
				Projects: map[string]*mirrorcfg.MirroringOptions{
					testProject.PathWithNamespace: {DestinationPath: customDestination},
				},
				Groups: map[string]*mirrorcfg.MirroringOptions{},
			},
			expectedProjects: map[string]struct{}{
				testProject.PathWithNamespace: {},
			},
			expectedMappingDestination: customDestination,
		},
		{
			name:           "source role, group-only config, derives destination path from group",
			role:           RoleSource,
			projectFilters: map[string]struct{}{},
			groupFilters: map[string]struct{}{
				testGroup.FullPath: {},
			},
			mirrorMapping: &mirrorcfg.MirrorMapping{
				Projects: map[string]*mirrorcfg.MirroringOptions{},
				Groups: map[string]*mirrorcfg.MirroringOptions{
					testGroup.FullPath: {DestinationPath: "mirror-group"},
				},
			},
			// testProject.PathWithNamespace is "test/group/project"; relative to
			// testGroup.FullPath ("test/group") that is "project", so the expected
			// destination is "mirror-group/project".
			expectedProjects: map[string]struct{}{
				testProject.PathWithNamespace: {},
			},
			expectedMappingDestination: "mirror-group/project",
		},
		{
			name: "destination role, project-only config, project in cache, mapping unchanged",
			role: RoleDestination,
			projectFilters: map[string]struct{}{
				testProject.PathWithNamespace: {},
			},
			groupFilters: map[string]struct{}{},
			mirrorMapping: &mirrorcfg.MirrorMapping{
				Projects: map[string]*mirrorcfg.MirroringOptions{},
				Groups:   map[string]*mirrorcfg.MirroringOptions{},
			},
			expectedProjects: map[string]struct{}{
				testProject.PathWithNamespace: {},
			},
		},
	}

	for _, tt := range tests { //nolint:paralleltest // asserts on the process-wide error tally (helpers.ExitCode)
		t.Run(tt.name, func(t *testing.T) {
			// Not parallel: the assertion below reads the process-wide error tally.
			helpers.ResetReported()
			t.Cleanup(helpers.ResetReported)

			gitlabInstance := setupTestServer(t, tt.role, InstanceSizeSmall)

			gitlabInstance.FetchAndProcessProjectsSmallInstance(tt.projectFilters, tt.groupFilters, tt.mirrorMapping)
			if got := helpers.ExitCode(); got != 0 {
				t.Fatalf("unexpected errors: exit code %d", got)
			}

			for project := range tt.expectedProjects {
				if _, ok := gitlabInstance.Projects[project]; !ok {
					t.Errorf("expected project %q in instance cache", project)
				}
			}

			if tt.expectedMappingDestination != "" {
				opts, ok := tt.mirrorMapping.GetProject(testProject.PathWithNamespace)
				if !ok {
					t.Fatalf("expected %q in mirror mapping after FetchAndProcessProjectsSmallInstance", testProject.PathWithNamespace)
				}
				if opts.DestinationPath != tt.expectedMappingDestination {
					t.Errorf("mirror mapping destination path: got %q, want %q", opts.DestinationPath, tt.expectedMappingDestination)
				}
			}
		})
	}
}

func TestStoreProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                    string
		role                    string
		parentGroupPath         string
		mirrorMapping           *mirrorcfg.MirrorMapping
		expectedDestinationPath string // empty means no entry expected in mirrorMapping.Projects
	}{
		{
			// Core bug fix: a project listed directly in the config must not require
			// a group entry and must preserve its configured destination path.
			name:            "source role, project directly in mirror mapping, preserves destination without group",
			role:            RoleSource,
			parentGroupPath: "",
			mirrorMapping: &mirrorcfg.MirrorMapping{
				Projects: map[string]*mirrorcfg.MirroringOptions{
					testProject.PathWithNamespace: {DestinationPath: "custom/dest/project"},
				},
				Groups: map[string]*mirrorcfg.MirroringOptions{},
			},
			expectedDestinationPath: "custom/dest/project",
		},
		{
			// Existing group-derived behaviour must still work.
			name:            "source role, project matched via parent group, derives destination path",
			role:            RoleSource,
			parentGroupPath: testGroup.FullPath,
			mirrorMapping: &mirrorcfg.MirrorMapping{
				Projects: map[string]*mirrorcfg.MirroringOptions{},
				Groups: map[string]*mirrorcfg.MirroringOptions{
					testGroup.FullPath: {DestinationPath: "mirror-group"},
				},
			},
			// filepath.Join("mirror-group", filepath.Rel("test/group", "test/group/project")) == "mirror-group/project"
			expectedDestinationPath: "mirror-group/project",
		},
		{
			// Project not in mapping and no matching group: project enters the instance
			// cache but is not added to the mirror mapping.
			name:            "source role, no project or group entry, not added to mirror mapping",
			role:            RoleSource,
			parentGroupPath: "nonexistent/group",
			mirrorMapping: &mirrorcfg.MirrorMapping{
				Projects: map[string]*mirrorcfg.MirroringOptions{},
				Groups:   map[string]*mirrorcfg.MirroringOptions{},
			},
			expectedDestinationPath: "",
		},
		{
			// Destination role never touches the mirror mapping.
			name:            "destination role, project stored in cache, mirror mapping unchanged",
			role:            RoleDestination,
			parentGroupPath: "",
			mirrorMapping: &mirrorcfg.MirrorMapping{
				Projects: map[string]*mirrorcfg.MirroringOptions{},
				Groups:   map[string]*mirrorcfg.MirroringOptions{},
			},
			expectedDestinationPath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gitlabInstance := setupTestServer(t, tt.role, InstanceSizeBig)

			gitlabInstance.storeProject(testProject, tt.parentGroupPath, tt.mirrorMapping)

			if _, ok := gitlabInstance.Projects[testProject.PathWithNamespace]; !ok {
				t.Errorf("expected project %q in instance cache after storeProject", testProject.PathWithNamespace)
			}

			opts, inMapping := tt.mirrorMapping.GetProject(testProject.PathWithNamespace)
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
