package mirroring

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
)

var groupAA = &gitlab.Group{ID: 10, Name: "AA", Path: "AA", FullPath: "AA", Visibility: gitlab.InternalVisibility}

func TestEnsureParentNamespaceIDCreatesMissingParent(t *testing.T) {
	t.Parallel()

	mux, instance := setupEmptyTestServer(t, RoleDestination)
	api := newFakeGroupsAPI(t, mux, groupAA)
	instance.AddGroup(groupAA)

	parentID, err := instance.EnsureParentNamespaceID("AA/BB/CC", gitlab.PublicVisibility)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	created := instance.GetGroup("AA/BB")
	if created == nil {
		t.Fatal("expected AA/BB to be created and cached")
	}

	if parentID != created.ID {
		t.Errorf("expected parent ID %d, got %d", created.ID, parentID)
	}

	if len(api.created) != 1 {
		t.Fatalf("expected exactly one group creation, got %v", api.createdPaths())
	}

	opts := api.created[0]
	if opts.ParentID == nil || *opts.ParentID != groupAA.ID {
		t.Errorf("expected AA/BB to be created under AA (ID %d), got %v", groupAA.ID, opts.ParentID)
	}

	// A new group inherits its parent's visibility so it is never more visible than its parent.
	if opts.Visibility == nil || *opts.Visibility != gitlab.InternalVisibility {
		t.Errorf("expected inherited visibility %q, got %v", gitlab.InternalVisibility, opts.Visibility)
	}
}

func TestEnsureParentNamespaceIDCreatesWholeChain(t *testing.T) {
	t.Parallel()

	mux, instance := setupEmptyTestServer(t, RoleDestination)
	// AA exists on the instance but is not in the cache (it was not part of the fetch filters).
	api := newFakeGroupsAPI(t, mux, groupAA)

	_, err := instance.EnsureParentNamespaceID("AA/BB/CC/DD", gitlab.PublicVisibility)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := strings.Join(api.createdPaths(), ","); got != "BB,CC" {
		t.Errorf("expected BB then CC to be created, got %q", got)
	}

	if instance.GetGroup("AA") == nil || instance.GetGroup("AA/BB/CC") == nil {
		t.Error("expected looked-up and created groups to be cached")
	}
}

func TestEnsureParentNamespaceIDCreatesMissingTopLevelGroup(t *testing.T) {
	t.Parallel()

	mux, instance := setupEmptyTestServer(t, RoleDestination)
	api := newFakeGroupsAPI(t, mux)

	_, err := instance.EnsureParentNamespaceID("AA/CC", gitlab.PrivateVisibility)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(api.created) != 1 || api.created[0].ParentID != nil {
		t.Fatalf("expected one top-level group creation, got %+v", api.created)
	}

	if *api.created[0].Visibility != gitlab.PrivateVisibility {
		t.Errorf("expected top-level group to use the requested visibility, got %q", *api.created[0].Visibility)
	}
}

func TestEnsureParentNamespaceIDUsesExistingUncachedParent(t *testing.T) {
	t.Parallel()

	mux, instance := setupEmptyTestServer(t, RoleDestination)
	groupBB := &gitlab.Group{ID: 11, Name: "BB", Path: "BB", FullPath: "AA/BB"}
	api := newFakeGroupsAPI(t, mux, groupAA, groupBB)

	parentID, err := instance.EnsureParentNamespaceID("AA/BB/CC", gitlab.PublicVisibility)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parentID != groupBB.ID {
		t.Errorf("expected parent ID %d, got %d", groupBB.ID, parentID)
	}

	if len(api.created) != 0 {
		t.Errorf("expected no group creation, got %v", api.createdPaths())
	}
}

func TestEnsureParentNamespaceIDTopLevelPath(t *testing.T) {
	t.Parallel()

	_, instance := setupEmptyTestServer(t, RoleDestination)

	parentID, err := instance.EnsureParentNamespaceID("AA", gitlab.PublicVisibility)
	if err != nil || parentID != -1 {
		t.Errorf("expected (-1, nil) for a top-level path, got (%d, %v)", parentID, err)
	}
}

func TestEnsureParentNamespaceIDLookupFailure(t *testing.T) {
	t.Parallel()

	mux, instance := setupEmptyTestServer(t, RoleDestination)
	api := newFakeGroupsAPI(t, mux)
	api.lookupStatus = http.StatusInternalServerError

	_, err := instance.EnsureParentNamespaceID("AA/BB/CC", gitlab.PublicVisibility)
	if err == nil {
		t.Fatal("expected an error when the group lookup fails")
	}

	if len(api.created) != 0 {
		t.Errorf("must not create groups when the lookup fails, got %v", api.createdPaths())
	}
}

func TestEnsureParentNamespaceIDConcurrentCallers(t *testing.T) {
	t.Parallel()

	mux, instance := setupEmptyTestServer(t, RoleDestination)
	api := newFakeGroupsAPI(t, mux, groupAA)
	instance.AddGroup(groupAA)

	const callers = 8

	var waitGroup sync.WaitGroup

	errs := make(chan error, callers)

	for i := range callers {
		waitGroup.Go(func() {
			_, err := instance.EnsureParentNamespaceID(fmt.Sprintf("AA/BB/project-%d", i), gitlab.PublicVisibility)
			errs <- err
		})
	}

	waitGroup.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}

	if len(api.created) != 1 {
		t.Errorf("expected AA/BB to be created once, got %v", api.createdPaths())
	}
}

func TestCreateProjectFromSourceCreatesMissingParentGroup(t *testing.T) {
	t.Parallel()

	mux, instance := setupEmptyTestServer(t, RoleDestination)
	api := newFakeGroupsAPI(t, mux, groupAA)
	instance.AddGroup(groupAA)

	var namespaceID int64

	mux.HandleFunc("/api/v4/projects", func(w http.ResponseWriter, r *http.Request) {
		var opts gitlab.CreateProjectOptions
		if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		if opts.NamespaceID != nil {
			namespaceID = *opts.NamespaceID
		}

		writeJSONResponse(w, http.StatusCreated, `{"id": 99, "path": "CC", "path_with_namespace": "AA/BB/CC"}`)
	})

	project, err := instance.CreateProjectFromSource(
		&gitlab.Project{Name: "CC", Path: "CC"},
		&mirrorcfg.MirroringOptions{DestinationPath: "AA/BB/CC", Visibility: new(string(gitlab.InternalVisibility))},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if project == nil || project.PathWithNamespace != "AA/BB/CC" {
		t.Fatalf("unexpected project: %+v", project)
	}

	createdGroup := instance.GetGroup("AA/BB")
	if createdGroup == nil || namespaceID != createdGroup.ID {
		t.Errorf("expected project namespace to be the created AA/BB group, got namespace %d (group %+v)", namespaceID, createdGroup)
	}

	if len(api.created) != 1 {
		t.Errorf("expected AA/BB to be created once, got %v", api.createdPaths())
	}
}

func TestCreateGroupFromSourceCreatesMissingParentGroup(t *testing.T) {
	t.Parallel()

	mux, instance := setupEmptyTestServer(t, RoleDestination)
	api := newFakeGroupsAPI(t, mux, groupAA)
	instance.AddGroup(groupAA)

	group, err := instance.CreateGroupFromSource(
		&gitlab.Group{Name: "CC", Path: "CC", Visibility: gitlab.InternalVisibility},
		&mirrorcfg.MirroringOptions{DestinationPath: "AA/BB/CC"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if group == nil || group.FullPath != "AA/BB/CC" {
		t.Fatalf("unexpected group: %+v", group)
	}

	if got := strings.Join(api.createdPaths(), ","); got != "BB,CC" {
		t.Errorf("expected BB then CC to be created, got %q", got)
	}
}
