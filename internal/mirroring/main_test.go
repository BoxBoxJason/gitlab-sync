package mirroring

import (
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
