package mirroring

import (
	"reflect"
	"testing"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
)

func TestProcessFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mirrorMapping                     *mirrorcfg.MirrorMapping
		expectedSourceProjectFilters      map[string]struct{}
		expectedSourceGroupFilters        map[string]struct{}
		expectedDestinationProjectFilters map[string]struct{}
		expectedDestinationGroupFilters   map[string]struct{}
		name                              string
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
	t.Parallel()

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
			sourceGitlabInstance := setupTestServer(t, RoleSource, tt.sourceSize)
			destinationGitlabInstance := setupTestServer(t, RoleDestination, InstanceSizeSmall)
			gitlabMirrorArgs := &mirrorcfg.MirrorMapping{
				Projects: map[string]*mirrorcfg.MirroringOptions{
					testProject.PathWithNamespace: {
						DestinationPath: testProject.PathWithNamespace,
						MirrorReleases:  new(true),
					},
				},
				Groups: map[string]*mirrorcfg.MirroringOptions{
					testGroup2.FullPath: {
						DestinationPath: testGroup2.FullPath,
						MirrorReleases:  new(true),
					},
				},
			}
			sourceGitlabInstance.AddProject(testProject)
			sourceGitlabInstance.AddGroup(testGroup2)
			sourceGitlabInstance.AddGroup(testGroup2)

			destinationGitlabInstance.DryRun(sourceGitlabInstance, gitlabMirrorArgs)
		})
	}
}
