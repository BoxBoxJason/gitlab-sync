package mirroring

import (
	"fmt"
	"net/http"
	"net/url"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

// setupTestGroups sets up the test HTTP server with handlers for group-related.
func setupTestGroups(mux *http.ServeMux) {
	// Setup the get groups endpoint to return a list of groups.
	mux.HandleFunc("/api/v4/groups", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set(headerContentType, headerAccept)
			// Set response status to 200 OK
			w.WriteHeader(http.StatusOK)
			// Return a mock response for the list of groups
			writeBody(w, testGroupsString)
		case http.MethodPost:
			// Set response status to 201 Created
			w.WriteHeader(http.StatusCreated)
			w.Header().Set(headerContentType, headerAccept)
			// Return a mock response for the created group
			writeBody(w, testGroupString)
		default:
			// Set response status to 405 Method Not Allowed
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	// Setup the get group for each group endpoint to return the group details.
	setupTestGroup(mux, testGroup, testGroupString)
	setupTestGroup(mux, testGroup2, testGroup2String)
}

// setupTestGroupGet sets up the test HTTP server with handlers for group-related.
func setupTestGroup(mux *http.ServeMux, group *gitlab.Group, stringResponse string) {
	// Setup the get group response from the group path
	mux.HandleFunc("/api/v4/groups/"+url.QueryEscape(group.FullPath), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(headerContentType, headerAccept)
		w.WriteHeader(http.StatusOK)
		writeBody(w, stringResponse)
	})
	// Setup the get group response from the group ID
	mux.HandleFunc(fmt.Sprintf("/api/v4/groups/%d", group.ID), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodPut:
			writeJSONResponse(w, http.StatusOK, stringResponse)
		default:
			writeMethodNotAllowed(w)
			return
		}
	})
	// Setup the get group avatar response from the group ID
	mux.HandleFunc(fmt.Sprintf("/api/v4/groups/%d/avatar", group.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// Write a fake image response
		w.Header().Set(headerContentType, "image/png")
		w.WriteHeader(http.StatusOK)
		writeBytes(w, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) // PNG header
	})
	// Setup the add group member endpoint
	mux.HandleFunc(fmt.Sprintf("/api/v4/groups/%d/members", group.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(headerContentType, headerAccept)
		w.WriteHeader(http.StatusCreated)
		// Return a mock member response
		writeBody(w, `{"id": 1, "username": "testuser", "name": "Test User", "state": "active", "access_level": 50}`)
	})
}

// setupTestProjects sets up the test HTTP server with handlers for project-related.
func setupTestProjects(mux *http.ServeMux) {
	// Setup the get projects endpoint to return a list of projects.
	mux.HandleFunc("/api/v4/projects", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set(headerContentType, headerAccept)
			// Set response status to 200 OK
			w.WriteHeader(http.StatusOK)
			writeBody(w, testProjectsString)
		case http.MethodPost:
			// Set response status to 201 Created
			w.WriteHeader(http.StatusCreated)
			w.Header().Set(headerContentType, headerAccept)
			// Return a mock response for the created project
			writeBody(w, testProjectString)
		default:
			// Set response status to 405 Method Not Allowed
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	// Setup the get project for each project endpoint to return the project details.
	setupTestProject(mux, testProject, testProjectString)
	setupTestProject(mux, testProject2, testProject2String)

	// ========== Setup the list subgroups endpoint for both groups ==========

	// Subgroups of the testGroup
	mux.HandleFunc(fmt.Sprintf("/api/v4/groups/%d/subgroups", testGroup.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(headerContentType, headerAccept)
		writeBody(w, fmt.Sprintf(`[%s]`, testGroup2String))
	})

	// Subgroups of the testGroup2
	mux.HandleFunc(fmt.Sprintf("/api/v4/groups/%d/subgroups", testGroup2.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(headerContentType, headerAccept)
		writeBody(w, "[]")
	})

	// ========== Setup the list projects endpoint for each group ==========
	// Projects of the testGroup
	mux.HandleFunc(fmt.Sprintf("/api/v4/groups/%d/projects", testGroup.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(headerContentType, headerAccept)
		writeBody(w, fmt.Sprintf(`[%s]`, testProjectString))
	})

	// Projects of the testGroup2
	mux.HandleFunc(fmt.Sprintf("/api/v4/groups/%d/projects", testGroup2.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(headerContentType, headerAccept)
		writeBody(w, fmt.Sprintf("[%s]", testProject2String))
	})
}

// setupTestProject sets up the test HTTP server with handlers for project-related actions.
func setupTestProject(mux *http.ServeMux, project *gitlab.Project, stringResponse string) {
	// Setup the get project response from the project path
	mux.HandleFunc("/api/v4/projects/"+url.QueryEscape(project.PathWithNamespace), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(headerContentType, headerAccept)
		w.WriteHeader(http.StatusOK)
		writeBody(w, stringResponse)
	})
	// Setup the get and put project response from the project ID
	mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d", project.ID), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set(headerContentType, headerAccept)
			writeBody(w, stringResponse)
		case http.MethodPut:
			w.Header().Set(headerContentType, headerAccept)
			w.WriteHeader(http.StatusOK)
			writeBody(w, stringResponse)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
	})
	// Setup the get project avatar response from the project ID
	mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d/avatar", project.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// Write a fake image response
		w.Header().Set(headerContentType, "image/png")
		w.WriteHeader(http.StatusOK)
		writeBytes(w, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) // PNG header
	})
	// Setup the get project releases response from the project ID
	mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d/releases", project.ID), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set(headerContentType, headerAccept)
			w.WriteHeader(http.StatusOK)
			writeBody(w, fmt.Sprintf("[%s]", testReleasesString[project.ID%2]))
		case http.MethodPost:
			w.Header().Set(headerContentType, headerAccept)
			w.WriteHeader(http.StatusCreated)
			writeBody(w, testReleaseString)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
	})
	// setup the put project mirror pull response from the project ID
	mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d/mirror/pull", project.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(headerContentType, headerAccept)
		w.WriteHeader(http.StatusOK)
		writeBody(w, "{}")
	})
	// Setup the get project issues response from the project ID
	mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d/issues", project.ID), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set(headerContentType, headerAccept)
			w.WriteHeader(http.StatusOK)
			writeBody(w, fmt.Sprintf("[%s]", testIssueString))
		case http.MethodPost:
			w.Header().Set(headerContentType, headerAccept)
			w.WriteHeader(http.StatusCreated)
			writeBody(w, testIssueString)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
	})
	// Setup the get project issue response from the project ID and issue IID
	mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d/issues/%d", project.ID, testIssue.IID), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodPut:
			writeJSONResponse(w, http.StatusOK, testIssueString)
		default:
			writeMethodNotAllowed(w)
			return
		}
	})

	mux.HandleFunc(fmt.Sprintf("/api/v4/projects/%d/members", project.ID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(headerContentType, headerAccept)
		w.WriteHeader(http.StatusCreated)
		// Return a mock member response
		writeBody(w, `{"id": 1, "username": "testuser", "name": "Test User", "state": "active", "access_level": 50}`)
	})
	// Setup the get project issue notes response from the project ID and issue IID
}
