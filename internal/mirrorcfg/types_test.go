package mirrorcfg

import (
	"fmt"
	"reflect"
	"testing"
)

const (
	fakeValidProject = "namespace/project"
	fakeValidGroup   = "group1"
)

var (
	fakeValidMapping = &MirrorMapping{
		Projects: map[string]*MirroringOptions{
			fakeValidProject: {
				DestinationPath:     fakeValidProject,
				CICDCatalog:         new(true),
				MirrorIssues:        new(true),
				MirrorTriggerBuilds: new(false),
				Visibility:          new("private"),
			},
		},
		Groups: map[string]*MirroringOptions{
			fakeValidGroup: {
				DestinationPath:     fakeValidGroup,
				CICDCatalog:         new(true),
				MirrorIssues:        new(true),
				MirrorTriggerBuilds: new(false),
				Visibility:          new("private"),
			},
		},
	}

	fakeValidMappingRaw = fmt.Sprintf(`{
		"projects": {
			"%s": {
				"destination_path": "%s",
				"ci_cd_catalog": true,
				"mirror_issues": true,
				"mirror_trigger_builds": false,
				"visibility": "private"
			}
		},
		"groups": {
			"%s": {
				"destination_path": "%s",
				"ci_cd_catalog": true,
				"mirror_issues": true,
				"mirror_trigger_builds": false,
				"visibility": "private"
			}
		}
	}`, fakeValidProject, fakeValidProject, fakeValidGroup, fakeValidGroup)
)

// testMirroringOptions is a helper function to create a MirroringOptions instance for testing.
func testMirroringOptions() *MirroringOptions {
	return &MirroringOptions{
		DestinationPath: "project",
		CICDCatalog:     new(true),
		MirrorIssues:    new(true),
	}
}

// TestAddProject tests adding a project to the MirrorMapping.
func TestAddProject(t *testing.T) {
	t.Parallel()

	m := &MirrorMapping{
		Projects: make(map[string]*MirroringOptions),
	}

	project := "test-project"
	options := testMirroringOptions()

	m.AddProject(project, options)

	if got, exists := m.Projects[project]; !exists {
		t.Fatalf("expected project %s to be added", project)
	} else if !reflect.DeepEqual(got, options) {
		t.Errorf("expected project options %v, got %v", options, got)
	}
}

// TestAddGroup tests adding a group to the MirrorMapping.
func TestAddGroup(t *testing.T) {
	t.Parallel()

	m := &MirrorMapping{
		Groups: make(map[string]*MirroringOptions),
	}

	group := "test-group"
	options := testMirroringOptions()

	m.AddGroup(group, options)

	if got, exists := m.Groups[group]; !exists {
		t.Fatalf("expected group %s to be added", group)
	} else if !reflect.DeepEqual(got, options) {
		t.Errorf("expected group options %v, got %v", options, got)
	}
}

func TestMirrorMappingGetProject(t *testing.T) {
	t.Parallel()

	// Prepare a mirror mapping with some project entries
	opts1 := &MirroringOptions{
		DestinationPath:     "dest1",
		CICDCatalog:         new(true),
		MirrorIssues:        new(false),
		MirrorTriggerBuilds: new(true),
		Visibility:          new("public"),
		MirrorReleases:      new(false),
	}
	opts2 := &MirroringOptions{
		DestinationPath:     "dest2",
		CICDCatalog:         new(false),
		MirrorIssues:        new(true),
		MirrorTriggerBuilds: new(false),
		Visibility:          new("private"),
		MirrorReleases:      new(true),
	}
	mm := &MirrorMapping{
		Projects: map[string]*MirroringOptions{
			"project-one": opts1,
			"project-two": opts2,
		},
		Groups: map[string]*MirroringOptions{},
	}

	tests := []struct {
		want   *MirroringOptions
		name   string
		key    string
		wantOk bool
	}{
		{
			name:   "existing project-one",
			key:    "project-one",
			want:   opts1,
			wantOk: true,
		},
		{
			name:   "existing project-two",
			key:    "project-two",
			want:   opts2,
			wantOk: true,
		},
		{
			name:   "nonexistent project",
			key:    "no-such-project",
			want:   nil,
			wantOk: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := mm.GetProject(tc.key)
			if ok != tc.wantOk {
				t.Errorf("GetProject(%q) returned ok=%v, want %v", tc.key, ok, tc.wantOk)
			}
			if got != tc.want {
				t.Errorf("GetProject(%q) returned %+v, want %+v", tc.key, got, tc.want)
			}
		})
	}
}

func TestMirrorMappingGetGroup(t *testing.T) {
	t.Parallel()

	// Prepare a mirror mapping with some group entries
	optsA := &MirroringOptions{
		DestinationPath:     "groupDestA",
		CICDCatalog:         new(true),
		MirrorIssues:        new(true),
		MirrorTriggerBuilds: new(false),
		Visibility:          new("internal"),
		MirrorReleases:      new(true),
	}
	optsB := &MirroringOptions{
		DestinationPath:     "groupDestB",
		CICDCatalog:         new(false),
		MirrorIssues:        new(false),
		MirrorTriggerBuilds: new(true),
		Visibility:          new("private"),
		MirrorReleases:      new(false),
	}
	mm := &MirrorMapping{
		Projects: map[string]*MirroringOptions{},
		Groups: map[string]*MirroringOptions{
			"group-A": optsA,
			"group-B": optsB,
		},
	}

	tests := []struct {
		want   *MirroringOptions
		name   string
		key    string
		wantOk bool
	}{
		{
			name:   "existing group-A",
			key:    "group-A",
			want:   optsA,
			wantOk: true,
		},
		{
			name:   "existing group-B",
			key:    "group-B",
			want:   optsB,
			wantOk: true,
		},
		{
			name:   "nonexistent group",
			key:    "no-such-group",
			want:   nil,
			wantOk: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := mm.GetGroup(tc.key)
			if ok != tc.wantOk {
				t.Errorf("GetGroup(%q) returned ok=%v, want %v", tc.key, ok, tc.wantOk)
			}
			if got != tc.want {
				t.Errorf("GetGroup(%q) returned %+v, want %+v", tc.key, got, tc.want)
			}
		})
	}
}

func TestMirroringOptionsTriggerBuilds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		options *MirroringOptions
		name    string
		want    bool
	}{
		{nil, "no options", false},
		{&MirroringOptions{}, "mirror_trigger_builds unset", false},
		{&MirroringOptions{MirrorTriggerBuilds: new(false)}, "mirror_trigger_builds disabled", false},
		{&MirroringOptions{MirrorTriggerBuilds: new(true)}, "mirror_trigger_builds enabled", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.options.TriggerBuilds(); got != tt.want {
				t.Errorf("TriggerBuilds() = %v; want %v", got, tt.want)
			}
		})
	}
}
