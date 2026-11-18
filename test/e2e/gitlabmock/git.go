package gitlabmock

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

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
		return nil, nil, err
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

	var refs *packp.AdvRefs

	switch service {
	case uploadPack:
		var session transport.UploadPackSession

		session, err = backend.NewUploadPackSession(endpoint, nil)
		if err == nil {
			defer session.Close() //nolint:errcheck // nothing to do about it

			refs, err = session.AdvertisedReferencesContext(request.Context())
		}
	case receivePack:
		var session transport.ReceivePackSession

		session, err = backend.NewReceivePackSession(endpoint, nil)
		if err == nil {
			defer session.Close() //nolint:errcheck // nothing to do about it

			refs, err = session.AdvertisedReferencesContext(request.Context())
		}

		if err == nil {
			err = refs.Capabilities.Set(pushOptionsCapability)
		}
	default:
		s.notImplemented(writer, request)

		return
	}

	if err != nil && !errors.Is(err, transport.ErrEmptyRemoteRepository) {
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

	_ = refs.Encode(writer)
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

	defer session.Close() //nolint:errcheck // nothing to do about it

	// The session learns the capabilities it serves while advertising.
	if _, err = session.AdvertisedReferencesContext(request.Context()); err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())

		return
	}

	upload := packp.NewUploadPackRequest()
	if err = upload.Decode(request.Body); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())

		return
	}

	response, err := session.UploadPack(request.Context(), upload)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())

		return
	}

	writer.Header().Set("Content-Type", "application/x-"+uploadPack+"-result")
	_ = response.Encode(writer)
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

	defer session.Close() //nolint:errcheck // nothing to do about it

	if _, err = session.AdvertisedReferencesContext(request.Context()); err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())

		return
	}

	update := packp.NewReferenceUpdateRequest()
	if err = update.Decode(request.Body); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())

		return
	}

	if err = s.readPushOptions(request, update); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())

		return
	}

	// A push that only deletes refs carries no pack, which go-git's server would
	// refuse to unpack.
	if onlyDeletes(update) {
		update.Packfile = nil
	}

	status, err := session.ReceivePack(context.WithoutCancel(request.Context()), update)
	if err != nil && status == nil {
		writeError(writer, http.StatusInternalServerError, err.Error())

		return
	}

	s.markPushed(projectPath)

	writer.Header().Set("Content-Type", "application/x-"+receivePack+"-result")

	if status != nil && update.Capabilities.Supports(capability.ReportStatus) {
		_ = status.Encode(writer)
	}
}

// readPushOptions consumes the push options that precede the pack in the body
// of a push (go-git's decoder leaves them in the pack reader), records them (see
// PushOptions) and removes the capability go-git's server does not know.
func (s *Server) readPushOptions(request *http.Request, update *packp.ReferenceUpdateRequest) error {
	if !update.Capabilities.Supports(pushOptionsCapability) {
		return nil
	}

	update.Capabilities.Delete(pushOptionsCapability)

	if update.Packfile == nil {
		return nil
	}

	scanner := pktline.NewScanner(update.Packfile)

	var options []string

	for scanner.Scan() {
		payload := scanner.Bytes()
		if len(payload) == 0 {
			break
		}

		options = append(options, strings.TrimSuffix(string(payload), "\n"))
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, option := range options {
		s.pushOptions = append(s.pushOptions, PushOption{Time: time.Now(), Path: request.URL.Path, Option: strings.TrimSuffix(option, "=")})
	}

	return nil
}

// PushOption is a git push option received by the fake.
type PushOption struct {
	Time   time.Time
	Path   string
	Option string
}

// PushOptions returns the push options received so far.
func (s *Server) PushOptions() []PushOption {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]PushOption(nil), s.pushOptions...)
}

// markPushed records that the repository of a project now holds refs, and drops
// the releases of the tags the push deleted.
func (s *Server) markPushed(projectPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if found := s.projectByPath(strings.Trim(projectPath, "/")); found != nil {
		found.EmptyRepo = false

		found.dropReleasesOfDeletedTags()
	}
}

func onlyDeletes(update *packp.ReferenceUpdateRequest) bool {
	for _, command := range update.Commands {
		if !command.New.IsZero() {
			return false
		}
	}

	return len(update.Commands) > 0
}
