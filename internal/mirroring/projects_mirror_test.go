package mirroring

import (
	"encoding/json"
	"net/http"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

func TestSyncMirrorProjectAttributes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		destinationProject       *gitlab.Project
		copyOptions              *mirrorcfg.MirroringOptions
		expectedMirror           *bool
		name                     string
		pullMirrorAvailable      bool
		expectedMismatch         bool
		expectMirrorTriggerBuild bool
	}{
		{
			// Pull mirroring is a Premium feature and GitLab refuses mirror=true
			// without an import URL and a mirror user, so a freemium destination must
			// be left untouched: gitlab-sync pushes the repository itself instead.
			name:                "freemium destination leaves the mirror attributes alone",
			pullMirrorAvailable: false,
			destinationProject:  &gitlab.Project{ID: 1, Mirror: false, MirrorTriggerBuilds: false},
			copyOptions:         &mirrorcfg.MirroringOptions{MirrorTriggerBuilds: new(true)},
			expectedMismatch:    false,
			expectedMirror:      nil,
		},
		{
			name:                "freemium destination does not re-enable an existing pull mirror",
			pullMirrorAvailable: false,
			destinationProject:  &gitlab.Project{ID: 1, Mirror: true, MirrorOverwritesDivergedBranches: true},
			copyOptions:         &mirrorcfg.MirroringOptions{},
			expectedMismatch:    false,
			expectedMirror:      nil,
		},
		{
			name:                     "premium destination enables the pull mirror",
			pullMirrorAvailable:      true,
			destinationProject:       &gitlab.Project{ID: 1, Mirror: false},
			copyOptions:              &mirrorcfg.MirroringOptions{MirrorTriggerBuilds: new(true)},
			expectedMismatch:         true,
			expectedMirror:           new(true),
			expectMirrorTriggerBuild: true,
		},
		{
			name:                "premium destination already in sync",
			pullMirrorAvailable: true,
			destinationProject:  &gitlab.Project{ID: 1, Mirror: true, MirrorOverwritesDivergedBranches: true},
			copyOptions:         &mirrorcfg.MirroringOptions{},
			expectedMismatch:    false,
			expectedMirror:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			editOptions := &gitlab.EditProjectOptions{}

			// Callers only sync the mirror attributes when pull mirroring is available.
			mismatch := tt.pullMirrorAvailable && syncMirrorProjectAttributes(tt.destinationProject, tt.copyOptions, editOptions)
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
			default:
			}

			if !tt.expectMirrorTriggerBuild && editOptions.MirrorTriggerBuilds != nil {
				t.Errorf("expected MirrorTriggerBuilds to be left unset, got %v", *editOptions.MirrorTriggerBuilds)
			}
		})
	}
}

func TestMirrorPushOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mirrorOptions     *mirrorcfg.MirroringOptions
		name              string
		wantSkipPipelines bool
	}{
		{nil, "no mirroring options", true},
		{&mirrorcfg.MirroringOptions{}, "mirror_trigger_builds unset", true},
		{&mirrorcfg.MirroringOptions{MirrorTriggerBuilds: new(false)}, "mirror_trigger_builds disabled", true},
		{&mirrorcfg.MirroringOptions{MirrorTriggerBuilds: new(true)}, "mirror_trigger_builds enabled", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			want := helpers.MirrorPushOptions{SkipPipelines: tt.wantSkipPipelines}
			if got := mirrorPushOptions(tt.mirrorOptions); got != want {
				t.Errorf("mirrorPushOptions() = %+v; want %+v", got, want)
			}
		})
	}
}

func TestDisableProjectMirrorPull(t *testing.T) {
	t.Parallel()

	t.Run("no call when the project is not a pull mirror", func(t *testing.T) {
		t.Parallel()

		_, gitlabInstance := setupEmptyTestServer(t, RoleDestination)

		project := &gitlab.Project{ID: 1, PathWithNamespace: testProject.PathWithNamespace, Mirror: false}
		if err := gitlabInstance.DisableProjectMirrorPull(project); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("turns an existing pull mirror off before pushing", func(t *testing.T) {
		t.Parallel()

		mux, gitlabInstance := setupEmptyTestServer(t, RoleDestination)

		var editBody map[string]any

		mux.HandleFunc("/api/v4/projects/1", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPut {
				w.WriteHeader(http.StatusMethodNotAllowed)

				return
			}

			if err := json.NewDecoder(r.Body).Decode(&editBody); err != nil {
				t.Errorf("failed to decode project edit body: %v", err)
			}

			w.Header().Set(headerContentType, headerAccept)
			w.WriteHeader(http.StatusOK)
			writeBody(w, testProjectString)
		})

		project := &gitlab.Project{ID: 1, PathWithNamespace: testProject.PathWithNamespace, Mirror: true}
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
