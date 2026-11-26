//go:build e2e

package e2e

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

// userAgent checks the User-Agent of the requests of the last run whose path
// starts with pathPrefix (all of them when it is empty).
func (s *steps) userAgent(role, pattern, pathPrefix string) error {
	received, err := s.requests(role)
	if err != nil {
		return err
	}

	expression, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("bad pattern %q: %w", pattern, err)
	}

	checked := 0

	for _, candidate := range received {
		if !strings.HasPrefix(candidate.path, pathPrefix) {
			continue
		}

		checked++

		if agent := candidate.header.Get("User-Agent"); !expression.MatchString(agent) {
			return fmt.Errorf("%s carried the User-Agent %q, which does not match %q", candidate, agent, pattern)
		}
	}

	if checked == 0 {
		return fmt.Errorf("the %s received no request under %q", role, pathPrefix)
	}

	return nil
}

// authenticatedWith checks that the API and git requests a role received carried
// the token of a role. Git requests may be tried without credentials first.
func (s *steps) authenticatedWith(received, owner string) error {
	requests, err := s.requests(received)
	if err != nil {
		return err
	}

	token := s.w.token(owner)
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("git:"+token))
	checked := 0

	for _, candidate := range requests {
		private, authorization := candidate.header.Get(gitlab.AccessTokenHeaderName), candidate.header.Get("Authorization")
		if private == "" && authorization == "" && candidate.status == http.StatusUnauthorized {
			continue
		}

		checked++

		if private != token && authorization != basic {
			return fmt.Errorf("%s was not authenticated with the %s token", candidate, owner)
		}
	}

	if checked == 0 {
		return fmt.Errorf("the %s received no request", received)
	}

	return nil
}

// downloaded is how many bytes of git objects the source sent during a run.
func (s *steps) downloaded(number int) (int64, error) {
	received, err := s.requestsOfRun(roleSource, number)
	if err != nil {
		return 0, err
	}

	var total int64

	for _, candidate := range received {
		if candidate.method == http.MethodPost && strings.HasSuffix(candidate.path, "/git-upload-pack") {
			total += candidate.received
		}
	}

	return total, nil
}

// comparison is what two runs downloaded from the source.
type comparison struct {
	later, earlier int64
}

// downloads returns what two runs downloaded from the source.
func (s *steps) downloads(later, earlier int) (comparison, error) {
	laterBytes, err := s.downloaded(later)
	if err != nil {
		return comparison{}, err
	}

	earlierBytes, err := s.downloaded(earlier)
	if err != nil {
		return comparison{}, err
	}

	if earlierBytes == 0 {
		return comparison{}, fmt.Errorf("run %d downloaded nothing from the source, the comparison would prove nothing", earlier)
	}

	return comparison{later: laterBytes, earlier: earlierBytes}, nil
}

func (s *steps) downloadedLess(later, earlier int) error {
	sizes, err := s.downloads(later, earlier)
	if err != nil {
		return err
	}

	if sizes.later >= sizes.earlier {
		return fmt.Errorf("run %d downloaded %d bytes from the source, run %d downloaded %d: want less", later, sizes.later, earlier, sizes.earlier)
	}

	return nil
}

// downloadedSame tolerates a tenth of difference: the packs GitLab builds for the
// same objects are not guaranteed to be identical.
func (s *steps) downloadedSame(later, earlier int) error {
	sizes, err := s.downloads(later, earlier)
	if err != nil {
		return err
	}

	const tolerance = 10

	if difference := sizes.later - sizes.earlier; max(difference, -difference) > sizes.earlier/tolerance {
		return fmt.Errorf("run %d downloaded %d bytes from the source, run %d downloaded %d: want about the same", later, sizes.later, earlier, sizes.earlier)
	}

	return nil
}

// pushOptions returns the git push options the destination received during the
// last run, which must have pushed.
func (s *steps) pushOptions() ([]string, error) {
	received, err := s.requests(roleDestination)
	if err != nil {
		return nil, err
	}

	var (
		options []string
		pushed  bool
	)

	for _, candidate := range received {
		if candidate.method != http.MethodPost || !strings.HasSuffix(candidate.path, "/git-receive-pack") || len(candidate.body) <= pktHeader {
			continue
		}

		found, parseErr := pushOptionsOf(candidate.body)
		if parseErr != nil {
			return nil, fmt.Errorf("read the push options of %s: %w", candidate, parseErr)
		}

		pushed = true

		options = append(options, found...)
	}

	if !pushed {
		return nil, fmt.Errorf("the destination received no push%s", s.listing(roleDestination))
	}

	return options, nil
}

func (s *steps) pushOption(want string) error {
	options, err := s.pushOptions()
	if err != nil {
		return err
	}

	// go-git sends every option as "key=value".
	if !slices.Contains(options, want+"=") {
		return fmt.Errorf("the destination received the push options %v, want %q among them", options, want)
	}

	return nil
}

func (s *steps) noPushOption() error {
	options, err := s.pushOptions()
	if err != nil {
		return err
	}

	if len(options) > 0 {
		return fmt.Errorf("the destination received the push options %v, want none", options)
	}

	return nil
}
