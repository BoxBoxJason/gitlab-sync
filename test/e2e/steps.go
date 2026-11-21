//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"

	"github.com/cucumber/godog"
)

const (
	role   = `(source|destination)`
	method = `(GET|POST|PUT|DELETE|PATCH)`

	// tableColumns is the width of the attribute / value tables.
	tableColumns = 2

	// packageFileParts is the number of parts of a "name/version/file" spec.
	packageFileParts = 3

	// GitLab access levels.
	accessGuest      = 10
	accessDeveloper  = 30
	accessMaintainer = 40
)

// steps holds the scenario state the step definitions work on.
type steps struct {
	w *world
}

// initializeScenario returns what godog calls for every scenario: it registers
// the hooks and the steps, which run the given binary.
func initializeScenario(bin binary) func(*godog.ScenarioContext) {
	return func(scenario *godog.ScenarioContext) {
		state := &steps{}

		scenario.Before(state.before(bin))
		scenario.After(state.after)

		state.registerGivenSteps(scenario)
		state.registerRunSteps(scenario)
		state.registerThenSteps(scenario)
	}
}

// before builds the fake instances of the scenario.
func (s *steps) before(bin binary) func(context.Context, *godog.Scenario) (context.Context, error) {
	return func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
		created, err := newWorld(bin)
		if err != nil {
			err = fmt.Errorf("prepare the scenario %q: %w", scenario.Name, err)
		} else {
			s.w = created
		}

		return ctx, err
	}
}

// after cleans the scenario up.
func (s *steps) after(ctx context.Context, scenario *godog.Scenario, scenarioErr error) (context.Context, error) {
	return ctx, s.finish(scenario.Name, scenarioErr) //nolint:nilnil // godog takes the context back along with the error
}

// finish cleans the scenario up and returns what makes it fail. A scenario
// fails when the application reached something the fake does not implement:
// the answer it got (501) is not what a GitLab would have given.
func (s *steps) finish(name string, scenarioErr error) error {
	if s.w == nil {
		return scenarioErr
	}

	var unimplemented []string

	for _, server := range s.w.servers() {
		unimplemented = append(unimplemented, server.Unimplemented()...)
	}

	closeErr := s.w.close()

	switch {
	case scenarioErr != nil:
		return scenarioErr
	case closeErr != nil:
		return fmt.Errorf("clean up the scenario %q: %w", name, closeErr)
	case len(unimplemented) > 0:
		return fmt.Errorf("the fake GitLab does not implement: %s", strings.Join(unimplemented, ", "))
	default:
		return nil
	}
}

func (s *steps) registerRunSteps(scenario *godog.ScenarioContext) {
	scenario.Step(`^I run gitlab-sync$`, func() error { return s.runWith("", "") })
	scenario.Step(`^I run gitlab-sync with "([^"]*)"$`, func(flags string) error { return s.runWith(flags, "") })
	scenario.Step(`^I run gitlab-sync without "([^"]*)"$`, func(dropped string) error { return s.runWith("", dropped) })
	scenario.Step(`^I run gitlab-sync with "([^"]*)" without "([^"]*)"$`, s.runWith)
	scenario.Step(`^I run gitlab-sync with the bare arguments "([^"]*)"$`, func(args string) error {
		return s.w.execute(splitArgs(s.w.expand(args)))
	})
}

// runWith runs the binary with the default flags, the extra flags, minus the
// dropped flags.
func (s *steps) runWith(flags, dropped string) error {
	return s.w.execute(s.w.flagsFor(splitArgs(s.w.expand(flags)), splitArgs(dropped)))
}

// failed wraps the error of a call to the fake GitLab (nil stays nil).
func failed(err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("fake GitLab: %w", err)
}
