//go:build e2e

package e2e

import (
	"fmt"
	"regexp"
	"sync"
)

const (
	// sharedProject is the read-only source project the scenarios that never
	// change their source share: creating one per scenario is a large part of the
	// work GitLab has to do.
	sharedProject = "team/app"
	// sharedSuffix names the source group holding it, in place of a scenario one.
	sharedSuffix = "shared"
)

// sourceChange matches the steps giving something to the source of a scenario,
// which the shared one must not receive.
var sourceChange = regexp.MustCompile(`^the source (has|group|project) `)

// sourceCheck matches the steps of that form that only read the source.
var sourceCheck = regexp.MustCompile(`same refs|is still`)

// sharedFixture creates the shared source project once for the whole run.
type sharedFixture struct {
	err  error
	once sync.Once
}

func (f *sharedFixture) create(env *environment) error {
	f.once.Do(func() {
		seeder := &steps{w: &world{
			env:      env,
			suffix:   sharedSuffix,
			branches: make(map[string][]string),
			groups:   map[string]int64{env.root: env.rootID},
		}}

		f.err = seeder.projectWithCommit(roleSource, sharedProject, defaultBranch)
	})

	return f.err
}

// removeSharedSource deletes the group of the shared source, if any scenario
// had it created.
func (env *environment) removeSharedSource() error {
	owner := &world{env: env, sharedSource: true}

	return owner.removeGroup(owner.groupPath(roleSource))
}

// useSharedSource makes the shared project the source of the scenario.
func (s *steps) useSharedSource() error {
	err := s.w.env.shared.create(s.w.env)
	if err != nil {
		return fmt.Errorf("create the shared source: %w", err)
	}

	s.w.sharedSource = true

	return nil
}

// checkSharedSource refuses the steps that would change the shared source, which
// the other scenarios are reading.
func (s *steps) checkSharedSource(step string) error {
	if s.w.sharedSource && sourceChange.MatchString(step) && !sourceCheck.MatchString(step) {
		return fmt.Errorf("the step %q would change the shared source project, give the scenario its own", step)
	}

	return nil
}
