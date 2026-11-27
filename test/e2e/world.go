//go:build e2e

// Package e2e runs the gitlab-sync binary against a real GitLab instance
// (see test/e2e/gitlab), seeding and checking it through its API. The scenarios
// are skipped unless GITLAB_E2E_URL and GITLAB_E2E_TOKEN are set. Run them with
// `make e2e`.
package e2e

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

const (
	// runTimeout fails a run that hangs instead of blocking the CI.
	runTimeout = 5 * time.Minute

	// GitLab updates some of what it answers in background jobs: the assertions
	// about its state are retried for that long.
	settleTimeout  = 2 * time.Minute
	settleInterval = time.Second

	roleSource      = "source"
	roleDestination = "destination"

	mappingFilePerm = 0o600
)

// binary is the gitlab-sync binary every scenario runs, built with coverage
// instrumentation.
type binary struct {
	path string
	// coverDir is where the instrumented binary writes its coverage data.
	coverDir string
}

// environment is what the scenarios share: the GitLab instance and the binary.
type environment struct {
	client *gitlab.Client
	bin    binary
	url    string
	token  string
	// root is the group holding the groups of every scenario of the run.
	root string
	// shared is the source project of the scenarios that only read it.
	shared sharedFixture
	rootID int64
}

// outcome is the result of one execution of the binary.
type outcome struct {
	// started is how many requests each proxy had received when the run began.
	started map[string]int
	stdout  string
	stderr  string
	args    []string
	exit    int
}

// world is the state of one scenario. The source and the destination are two
// groups of the same instance, named after the scenario so that
// scenarios never see each other's projects.
type world struct {
	env *environment
	// branches are the branches of each source project, in creation order.
	branches map[string][]string
	// groups are the IDs of the groups of the scenario, by full path.
	groups map[string]int64
	// proxies holds, per role, the proxy of an observed scenario.
	proxies map[string]*proxy
	// tokens holds, per role, the token replacing the administrator one.
	tokens map[string]string
	// unreachable holds, per role, a URL nothing listens on.
	unreachable map[string]string
	// uploads holds the URL of the files uploaded to the source, by file name.
	uploads     map[string]string
	work        string
	mappingPath string
	suffix      string
	runs        []*outcome
	// destinationUser is the user of the destination token, 0 for the administrator.
	destinationUser int64
	files           int
	users           int
	// sharedSource tells that the source is the one of the shared fixture.
	sharedSource bool
}

func newWorld(env *environment) (*world, error) {
	work, err := os.MkdirTemp("", "gitlab-sync-e2e-*")
	if err != nil {
		return nil, fmt.Errorf("create the work directory: %w", err)
	}

	created := &world{
		env:         env,
		work:        work,
		suffix:      strconv.FormatInt(time.Now().UnixNano(), 36),
		branches:    make(map[string][]string),
		groups:      make(map[string]int64),
		proxies:     make(map[string]*proxy),
		tokens:      make(map[string]string),
		unreachable: make(map[string]string),
		uploads:     make(map[string]string),
	}
	created.groups[env.root] = env.rootID

	return created, nil
}

// observe puts a proxy between gitlab-sync and the instance, for each role.
func (w *world) observe() error {
	for _, role := range []string{roleSource, roleDestination} {
		running, err := newProxy(w.env.url)
		if err != nil {
			return fmt.Errorf("start the %s proxy: %w", role, err)
		}

		w.proxies[role] = running
	}

	return nil
}

// close stops the proxies, removes the scenario files and its groups.
func (w *world) close() error {
	for _, running := range w.proxies {
		running.close()
	}

	err := os.RemoveAll(w.work)
	if err != nil {
		err = fmt.Errorf("remove the scenario files: %w", err)
	}

	// The shared source outlives the scenario: it goes with the root group.
	if !w.sharedSource {
		err = errors.Join(err, w.removeGroup(w.groupPath(roleSource)))
	}

	return errors.Join(err, w.removeGroup(w.groupPath(roleDestination)))
}

// removeGroup deletes a group for good, if it exists. gitlab-sync lists every
// project of the instance: keeping them would slow each run down more than the
// previous one.
func (w *world) removeGroup(fullPath string) error {
	// Without the projects of the group, which GitLab is slow to serialize.
	withoutProjects := &gitlab.GetGroupOptions{WithProjects: new(false)} //nolint:staticcheck // deprecated for client v5, still the way to ask for it

	group, response, err := w.env.client.Groups.GetGroup(fullPath, withoutProjects)

	exists, err := found(response, err)
	if err != nil || !exists {
		return err
	}

	// A group is first marked for deletion, which renames it.
	_, err = w.env.client.Groups.DeleteGroup(group.ID, nil)
	if err != nil {
		return fmt.Errorf("delete the group %s: %w", fullPath, err)
	}

	marked, _, err := w.env.client.Groups.GetGroup(group.ID, withoutProjects)
	if err != nil {
		return fmt.Errorf("get the deleted group %s: %w", fullPath, err)
	}

	_, err = w.env.client.Groups.DeleteGroup(group.ID, &gitlab.DeleteGroupOptions{PermanentlyRemove: new(true), FullPath: &marked.FullPath})
	if err != nil {
		return fmt.Errorf("remove the group %s: %w", fullPath, err)
	}

	return nil
}

// groupPath is the group of a role.
func (w *world) groupPath(role string) string {
	if role != roleSource {
		return w.env.root + "/dst-" + w.suffix
	}

	suffix := w.suffix
	if w.sharedSource {
		suffix = sharedSuffix
	}

	return w.env.root + "/src-" + suffix
}

// fullPath is the full path of a group or a project of a role.
func (w *world) fullPath(role, path string) string {
	return w.groupPath(role) + "/" + path
}

// url is the URL the binary is given for a role.
func (w *world) url(role string) string {
	if url, ok := w.unreachable[role]; ok {
		return url
	}

	if running, ok := w.proxies[role]; ok {
		return running.server.URL
	}

	return w.env.url
}

// token is the token the binary is given for a role.
func (w *world) token(role string) string {
	if token, ok := w.tokens[role]; ok {
		return token
	}

	return w.env.token
}

// destinationUserID is the user of the destination token.
func (w *world) destinationUserID() (int64, error) {
	if w.destinationUser != 0 {
		return w.destinationUser, nil
	}

	user, _, err := w.env.client.Users.CurrentUser()
	if err != nil {
		return 0, fmt.Errorf("get the current user: %w", err)
	}

	w.destinationUser = user.ID

	return user.ID, nil
}

// makeUnreachable gives a role a URL nothing listens on.
func (w *world) makeUnreachable(role string) error {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("reserve a port: %w", err)
	}

	w.unreachable[role] = "http://" + listener.Addr().String()

	err = listener.Close()
	if err != nil {
		return fmt.Errorf("release the port: %w", err)
	}

	return nil
}

// expand replaces the placeholders a scenario can use in its mapping, arguments,
// patterns and URLs.
func (w *world) expand(text string) string {
	return strings.NewReplacer(
		"{source}", w.groupPath(roleSource),
		"{destination}", w.groupPath(roleDestination),
		"{source_url}", w.url(roleSource),
		"{destination_url}", w.url(roleDestination),
		"{work}", w.work,
		"{cache}", filepath.Join(w.work, "cache"),
	).Replace(text)
}

// writeMapping stores the mirror mapping the next runs are given.
func (w *world) writeMapping(content string) error {
	w.mappingPath = filepath.Join(w.work, "mirror-mapping.json")

	err := os.WriteFile(w.mappingPath, []byte(w.expand(content)), mappingFilePerm)
	if err != nil {
		return fmt.Errorf("write the mirror mapping: %w", err)
	}

	return nil
}
