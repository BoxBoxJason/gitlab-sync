package gitlabmock

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

func deref[T any](value *T) T {
	var zero T

	if value == nil {
		return zero
	}

	return *value
}

// avatarUpload is the avatar of a request, when it uploads one.
type avatarUpload struct {
	content  []byte
	uploaded bool
}

// readAvatar reads the avatar of a multipart upload (UploadAvatar of the client).
// uploaded is false for any other kind of request.
func readAvatar(writer http.ResponseWriter, request *http.Request) (avatarUpload, error) {
	if !strings.HasPrefix(request.Header.Get("Content-Type"), "multipart/") {
		return avatarUpload{}, nil
	}

	request.Body = http.MaxBytesReader(writer, request.Body, maxUpload)

	err := request.ParseMultipartForm(maxUpload) //nolint:gosec // the body is bounded just above (G120 does not see the MaxBytesReader)
	if err != nil {
		return avatarUpload{}, fmt.Errorf("read the upload: %w", err)
	}

	file, _, err := request.FormFile("avatar")
	if err != nil {
		return avatarUpload{}, fmt.Errorf("find the avatar in the upload: %w", err)
	}

	defer closeLogged(file)

	content, err := io.ReadAll(file)
	if err != nil {
		return avatarUpload{}, fmt.Errorf("read the avatar: %w", err)
	}

	return avatarUpload{content: content, uploaded: true}, nil
}

const maxUpload = 32 << 20

func (s *Server) serveGroupAvatar(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.lookupGroup(request.PathValue("id"))
	if found == nil || found.avatar == nil {
		writeError(writer, http.StatusNotFound, "404 Avatar Not Found")

		return
	}

	sendBytes(writer, found.avatar)
}

func (s *Server) serveProjectAvatar(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.lookupProject(request.PathValue("id"))
	if found == nil || found.avatar == nil {
		writeError(writer, http.StatusNotFound, "404 Avatar Not Found")

		return
	}

	sendBytes(writer, found.avatar)
}
