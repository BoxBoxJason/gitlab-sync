package mirrorcfg

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

// TestOpenMirrorMapping tests opening and parsing a JSON file into a MirrorMapping.
func TestOpenMirrorMapping(t *testing.T) {
	t.Parallel()

	file := createTempTestFile(t, fakeValidMappingRaw)

	mapping, err := OpenMirrorMapping(file.Name())
	if err != nil {
		t.Fatalf("OpenMirrorMapping() error = %v", err)
	}

	// Check if projects and groups are equal
	if len(mapping.Projects) != len(fakeValidMapping.Projects) || len(mapping.Groups) != len(fakeValidMapping.Groups) {
		t.Fatalf("expected mapping to have %d projects and %d groups, got %d projects and %d groups", len(fakeValidMapping.Projects), len(fakeValidMapping.Groups), len(mapping.Projects), len(mapping.Groups))
	}
	for k, v := range mapping.Projects {
		if expected, ok := fakeValidMapping.Projects[k]; ok {
			if !reflect.DeepEqual(v, expected) {
				t.Errorf("expected project %s options %v, got %v", k, expected, v)
			}
		} else {
			t.Errorf("unexpected project %s in mapping", k)
		}
	}
	for k, v := range mapping.Groups {
		if expected, ok := fakeValidMapping.Groups[k]; ok {
			if !reflect.DeepEqual(v, expected) {
				t.Errorf("expected group %s options %v, got %v", k, expected, v)
			}
		} else {
			t.Errorf("unexpected group %s in mapping", k)
		}
	}
}

func createTempTestFile(t *testing.T, fileContent string) *os.File {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "testfile")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	if _, err = file.WriteString(fileContent); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}

	err = file.Close()
	if err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}
	return file
}

// TestCheck tests the check method of MirrorMapping.
func TestCheck(t *testing.T) { //nolint:paralleltest // replaces the process-wide zap logger
	tests := []struct {
		name     string
		mapping  *MirrorMapping
		wantMsgs []string
	}{
		{
			name: "ValidMapping",
			mapping: &MirrorMapping{
				Projects: map[string]*MirroringOptions{
					fakeValidProject: {
						DestinationPath: fakeValidProject,
						CICDCatalog:     new(true),
						MirrorIssues:    new(true),
					},
				},
				Groups: map[string]*MirroringOptions{
					fakeValidGroup: {
						DestinationPath: fakeValidGroup,
						CICDCatalog:     new(true),
						MirrorIssues:    new(true),
					},
				},
			},
			// no errors expected
			wantMsgs: nil,
		},
		{
			name: "ValidMappingWithOptionalFieldsOmitted",
			mapping: &MirrorMapping{
				Projects: map[string]*MirroringOptions{
					fakeValidProject: {
						DestinationPath: fakeValidProject,
					},
				},
				Groups: map[string]*MirroringOptions{
					fakeValidGroup: {
						DestinationPath: fakeValidGroup,
					},
				},
			},
			wantMsgs: nil,
		},
		{
			name: "InvalidMappingNoProjectsOrGroups",
			mapping: &MirrorMapping{
				Projects: map[string]*MirroringOptions{},
				Groups:   map[string]*MirroringOptions{},
			},
			wantMsgs: []string{
				"no projects or groups defined in the mapping",
			},
		},
		{
			name: "InvalidProjectMapping",
			mapping: &MirrorMapping{
				Projects: map[string]*MirroringOptions{
					"": {
						DestinationPath: "",
					},
				},
				Groups: map[string]*MirroringOptions{},
			},
			wantMsgs: []string{
				"invalid (empty) string in project mapping",
			},
		},
		{
			name: "InvalidGroupMapping",
			mapping: &MirrorMapping{
				Projects: map[string]*MirroringOptions{},
				Groups: map[string]*MirroringOptions{
					"": {
						DestinationPath: "",
					},
				},
			},
			wantMsgs: []string{
				"invalid (empty) string in group mapping",
			},
		},
		{
			name: "MultipleErrors",
			mapping: &MirrorMapping{
				Projects: map[string]*MirroringOptions{
					"": {
						DestinationPath: "",
					},
				},
				Groups: map[string]*MirroringOptions{
					"": {
						DestinationPath: "",
					},
				},
			},
			wantMsgs: []string{
				"invalid (empty) string in project mapping",
				"invalid (empty) string in group mapping",
			},
		},
	}

	for _, tt := range tests { //nolint:paralleltest // replaces the process-wide zap logger
		t.Run(tt.name, func(t *testing.T) {
			// check() logs every problem as it finds it; capture those logs.
			core, recorded := observer.New(zapcore.ErrorLevel)
			restore := zap.ReplaceGlobals(zap.New(core))
			defer restore()

			problems := tt.mapping.check()

			got := make([]string, 0, len(recorded.All()))
			for _, entry := range recorded.All() {
				got = append(got, strings.TrimPrefix(entry.Message, "invalid mirror mapping: "))
			}

			if problems != len(tt.wantMsgs) {
				t.Errorf("check() problems = %d, want %d", problems, len(tt.wantMsgs))
			}
			if !slices.Equal(got, tt.wantMsgs) {
				t.Errorf("check() logged = %v, want %v", got, tt.wantMsgs)
			}
		})
	}
}

func TestCheckSetsDefaultVisibilityWhenOptionalFieldsOmitted(t *testing.T) {
	t.Parallel()

	mapping := &MirrorMapping{
		Projects: map[string]*MirroringOptions{
			fakeValidProject: {
				DestinationPath: fakeValidProject,
			},
		},
		Groups: map[string]*MirroringOptions{
			fakeValidGroup: {
				DestinationPath: fakeValidGroup,
			},
		},
	}

	if problems := mapping.check(); problems != 0 {
		t.Fatalf("expected no problems, got %d", problems)
	}

	if got := helpers.Deref(mapping.Projects[fakeValidProject].Visibility, ""); got != string(gitlab.PublicVisibility) {
		t.Errorf("expected default project visibility %q, got %q", string(gitlab.PublicVisibility), got)
	}
	if got := helpers.Deref(mapping.Groups[fakeValidGroup].Visibility, ""); got != string(gitlab.PublicVisibility) {
		t.Errorf("expected default group visibility %q, got %q", string(gitlab.PublicVisibility), got)
	}
}
