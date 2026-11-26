//go:build e2e

package e2e

import (
	"context"
	"fmt"

	"github.com/cucumber/godog"
)

const (
	role   = `(source|destination)`
	method = `(GET|POST|PUT|DELETE|PATCH)`

	// observedTag marks the scenarios that need a proxy between gitlab-sync and
	// the instance.
	observedTag = "@observed"
)

// steps holds the scenario state the step definitions work on.
type steps struct {
	w *world
}

// initializeScenario returns what godog calls for every scenario: it registers
// the hooks and the steps, which run the given binary against the instance.
func initializeScenario(env *environment) func(*godog.ScenarioContext) {
	return func(scenario *godog.ScenarioContext) {
		state := &steps{}

		scenario.Before(state.before(env))
		scenario.After(state.after)

		state.registerGivenSteps(scenario)
		state.registerAccountSteps(scenario)
		state.registerRunSteps(scenario)
		state.registerOutputSteps(scenario)
		state.registerRequestSteps(scenario)
		state.registerEntitySteps(scenario)
		state.registerReleaseSteps(scenario)
	}
}

// before prepares the world of the scenario.
func (s *steps) before(env *environment) func(context.Context, *godog.Scenario) (context.Context, error) {
	return func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
		observed := false
		for _, tag := range scenario.Tags {
			observed = observed || tag.Name == observedTag
		}

		created, err := newWorld(env)
		if err == nil {
			s.w = created

			if observed {
				err = created.observe()
			}
		}

		if err != nil {
			err = fmt.Errorf("prepare the scenario %q: %w", scenario.Name, err)
		}

		return ctx, err
	}
}

// after cleans the scenario up.
func (s *steps) after(ctx context.Context, scenario *godog.Scenario, scenarioErr error) (context.Context, error) {
	err := scenarioErr

	if s.w != nil {
		closeErr := s.w.close()
		if err == nil && closeErr != nil {
			err = fmt.Errorf("clean up the scenario %q: %w", scenario.Name, closeErr)
		}
	}

	return ctx, err //nolint:nilnil // godog takes the context back along with the error
}

func (s *steps) registerRunSteps(scenario *godog.ScenarioContext) {
	scenario.Step(`^I run gitlab-sync$`, func() error { return s.runWith("", "") })
	scenario.Step(`^I run gitlab-sync with "([^"]*)"$`, func(flags string) error { return s.runWith(flags, "") })
	scenario.Step(`^I run gitlab-sync without "([^"]*)"$`, func(dropped string) error { return s.runWith("", dropped) })
	scenario.Step(`^I run gitlab-sync with "([^"]*)" without "([^"]*)"$`, s.runWith)
	scenario.Step(`^I run gitlab-sync with the bare arguments "([^"]*)"$`, func(args string) error {
		return s.w.execute(splitList(s.w.expand(args), " "))
	})
}

// runWith runs the binary with the default flags, the extra flags, minus the
// dropped flags.
func (s *steps) runWith(flags, dropped string) error {
	return s.w.execute(s.w.flagsFor(splitList(s.w.expand(flags), " "), splitList(dropped, " ")))
}
