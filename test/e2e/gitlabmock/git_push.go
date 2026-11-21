package gitlabmock

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
)

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

	err := scanner.Err()
	if err != nil {
		return fmt.Errorf("scan the push options: %w", err)
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
