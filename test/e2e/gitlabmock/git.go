package gitlabmock

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
)

const (
	uploadPack  = "git-upload-pack"
	receivePack = "git-receive-pack"

	// pushOptionsCapability is what lets a client send push options. go-git's
	// server does not advertise it, so the fake adds it, records the options in
	// the journal and strips the capability before go-git sees the request.
	pushOptionsCapability capability.Capability = "push-options"
)

// repositoryLoader serves the bare repository of the project a git URL names.
type repositoryLoader struct{ server *Server }

func (l repositoryLoader) Load(endpoint *transport.Endpoint) (storer.Storer, error) {
	l.server.mu.Lock()
	defer l.server.mu.Unlock()

	found := l.server.projectByPath(strings.Trim(strings.TrimSuffix(endpoint.Path, ".git"), "/"))
	if found == nil {
		return nil, transport.ErrRepositoryNotFound
	}

	repository, err := found.repo()
	if err != nil {
		return nil, err
	}

	return repository.Storer, nil
}

// gitOrDownload serves GET /:project.git/info/refs (git smart HTTP) and the
// release direct asset URLs. Any other path is not implemented.
func (s *Server) gitOrDownload(writer http.ResponseWriter, request *http.Request) {
	if projectPath, found := strings.CutSuffix(request.URL.Path, ".git/info/refs"); found {
		s.advertiseRefs(writer, request, projectPath)

		return
	}

	if s.downloadRedirect(writer, request) {
		return
	}

	// A file the instance does not serve is simply not there.
	if strings.HasPrefix(request.URL.Path, "/uploads/") {
		writeError(writer, http.StatusNotFound, "404 File Not Found")

		return
	}

	s.notImplemented(writer, request)
}

// gitPost serves POST /:project.git/git-upload-pack and git-receive-pack.
func (s *Server) gitPost(writer http.ResponseWriter, request *http.Request) {
	if projectPath, found := strings.CutSuffix(request.URL.Path, ".git/"+uploadPack); found {
		s.uploadPack(writer, request, projectPath)

		return
	}

	if projectPath, found := strings.CutSuffix(request.URL.Path, ".git/"+receivePack); found {
		s.receivePack(writer, request, projectPath)

		return
	}

	s.notImplemented(writer, request)
}

func (s *Server) gitEndpoint(projectPath string) (*transport.Endpoint, transport.Transport, error) {
	endpoint, err := transport.NewEndpoint(s.publicBase + projectPath + ".git")
	if err != nil {
		return nil, nil, fmt.Errorf("build the git endpoint of %s: %w", projectPath, err)
	}

	return endpoint, server.NewServer(repositoryLoader{server: s}), nil
}

func (s *Server) advertiseRefs(writer http.ResponseWriter, request *http.Request, projectPath string) {
	service := request.URL.Query().Get("service")

	endpoint, backend, err := s.gitEndpoint(projectPath)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())

		return
	}

	refs, err := advertise(request.Context(), backend, endpoint, service)
	if errors.Is(err, errUnknownService) {
		s.notImplemented(writer, request)

		return
	}

	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, transport.ErrRepositoryNotFound) {
			status = http.StatusNotFound
		}

		writeError(writer, status, err.Error())

		return
	}

	writer.Header().Set("Content-Type", "application/x-"+service+"-advertisement")
	writer.Header().Set("Cache-Control", "no-cache")

	refs.Prefix = [][]byte{[]byte("# service=" + service), pktline.Flush}

	logFailure("write the references", refs.Encode(writer))
}

func (s *Server) uploadPack(writer http.ResponseWriter, request *http.Request, projectPath string) {
	endpoint, backend, err := s.gitEndpoint(projectPath)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())

		return
	}

	session, err := backend.NewUploadPackSession(endpoint, nil)
	if err != nil {
		writeError(writer, http.StatusNotFound, err.Error())

		return
	}

	defer closeLogged(session)

	// The session learns the capabilities it serves while advertising.
	_, err = session.AdvertisedReferencesContext(request.Context())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())

		return
	}

	upload := packp.NewUploadPackRequest()

	err = upload.Decode(request.Body)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())

		return
	}

	response, err := session.UploadPack(request.Context(), upload)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())

		return
	}

	writer.Header().Set("Content-Type", "application/x-"+uploadPack+"-result")
	logFailure("write the upload-pack result", response.Encode(writer))
}

// readUpdate decodes the body of a push.
func (s *Server) readUpdate(request *http.Request) (*packp.ReferenceUpdateRequest, error) {
	update := packp.NewReferenceUpdateRequest()

	err := update.Decode(request.Body)
	if err != nil {
		return nil, fmt.Errorf("decode the push: %w", err)
	}

	err = s.readPushOptions(request, update)
	if err != nil {
		return nil, fmt.Errorf("read the push options: %w", err)
	}

	// A push that only deletes refs carries no pack, which go-git's server would
	// refuse to unpack.
	if onlyDeletes(update) {
		update.Packfile = nil
	}

	return update, nil
}

func (s *Server) receivePack(writer http.ResponseWriter, request *http.Request, projectPath string) {
	endpoint, backend, err := s.gitEndpoint(projectPath)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())

		return
	}

	session, err := backend.NewReceivePackSession(endpoint, nil)
	if err != nil {
		writeError(writer, http.StatusNotFound, err.Error())

		return
	}

	defer closeLogged(session)

	_, err = session.AdvertisedReferencesContext(request.Context())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())

		return
	}

	update, err := s.readUpdate(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())

		return
	}

	status, err := session.ReceivePack(context.WithoutCancel(request.Context()), update)
	if err != nil && status == nil {
		writeError(writer, http.StatusInternalServerError, err.Error())

		return
	}

	s.markPushed(projectPath)

	writer.Header().Set("Content-Type", "application/x-"+receivePack+"-result")

	if status != nil && update.Capabilities.Supports(capability.ReportStatus) {
		logFailure("write the receive-pack status", status.Encode(writer))
	}
}
