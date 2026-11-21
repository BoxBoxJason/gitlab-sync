package mirroring

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

// fakeGroupsAPI is a minimal in-memory GitLab groups API: it answers
// GET /groups/:path and POST /groups, and records every created group.
type fakeGroupsAPI struct {
	byPath       map[string]*gitlab.Group
	created      []gitlab.CreateGroupOptions
	nextID       int64
	lookupStatus int
	mu           sync.Mutex
}

func newFakeGroupsAPI(t *testing.T, mux *http.ServeMux, existing ...*gitlab.Group) *fakeGroupsAPI {
	t.Helper()

	api := &fakeGroupsAPI{nextID: 1000, byPath: map[string]*gitlab.Group{}}
	for _, group := range existing {
		api.byPath[group.FullPath] = group
	}

	mux.HandleFunc("/api/v4/groups", api.handleCreate)
	mux.HandleFunc("/api/v4/groups/", api.handleGet)

	return api
}

func (api *fakeGroupsAPI) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)

		return
	}

	api.mu.Lock()
	defer api.mu.Unlock()

	if api.lookupStatus != 0 {
		writeJSONResponse(w, api.lookupStatus, `{"message":"boom"}`)

		return
	}

	group, ok := api.byPath[strings.TrimPrefix(r.URL.Path, "/api/v4/groups/")]
	if !ok {
		writeJSONResponse(w, http.StatusNotFound, `{"message":"404 Group Not Found"}`)

		return
	}

	body, err := json.Marshal(group)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	writeJSONResponse(w, http.StatusOK, string(body))
}

func (api *fakeGroupsAPI) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)

		return
	}

	var opts gitlab.CreateGroupOptions
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		writeJSONResponse(w, http.StatusBadRequest, `{"message":"bad body"}`)

		return
	}

	api.mu.Lock()
	defer api.mu.Unlock()

	fullPath := *opts.Path

	if opts.ParentID != nil {
		parent := api.groupByID(*opts.ParentID)
		if parent == nil {
			writeJSONResponse(w, http.StatusNotFound, `{"message":"parent not found"}`)

			return
		}

		fullPath = parent.FullPath + "/" + *opts.Path
	}

	if _, exists := api.byPath[fullPath]; exists {
		writeJSONResponse(w, http.StatusBadRequest, `{"message":"has already been taken"}`)

		return
	}

	api.nextID++
	group := &gitlab.Group{ID: api.nextID, Name: *opts.Name, Path: *opts.Path, FullPath: fullPath}

	if opts.Visibility != nil {
		group.Visibility = *opts.Visibility
	}

	api.byPath[fullPath] = group
	api.created = append(api.created, opts)

	body, err := json.Marshal(group)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	writeJSONResponse(w, http.StatusCreated, string(body))
}

func (api *fakeGroupsAPI) groupByID(id int64) *gitlab.Group {
	for _, group := range api.byPath {
		if group.ID == id {
			return group
		}
	}

	return nil
}

func (api *fakeGroupsAPI) createdPaths() []string {
	api.mu.Lock()
	defer api.mu.Unlock()

	paths := make([]string, 0, len(api.created))
	for i := range api.created {
		paths = append(paths, *api.created[i].Path)
	}

	return paths
}
