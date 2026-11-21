//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"

	"github.com/cucumber/godog"
)

// steps holds the scenario state the step definitions work on.
type steps struct {
	w *world
}

// initializeScenario returns what godog calls for every scenario: it registers
// the hooks and the steps, which run the given binary against the instance.
func initializeScenario(env environment) func(*godog.ScenarioContext) {
	return func(scenario *godog.ScenarioContext) {
		state := &steps{}

		scenario.Before(state.before(env))
		scenario.After(state.after)

		state.registerGivenSteps(scenario)
		state.registerWhenSteps(scenario)
		state.registerThenSteps(scenario)
	}
}

// before prepares the world of the scenario.
func (s *steps) before(env environment) func(context.Context, *godog.Scenario) (context.Context, error) {
	return func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
		created, err := newWorld(env)
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
	err := scenarioErr

	if s.w != nil {
		closeErr := s.w.close()
		if err == nil && closeErr != nil {
			err = fmt.Errorf("clean up the scenario %q: %w", scenario.Name, closeErr)
		}
	}

	return ctx, err //nolint:nilnil // godog takes the context back along with the error
}

func (s *steps) registerWhenSteps(scenario *godog.ScenarioContext) {
	scenario.Step(`^I run gitlab-sync$`, func() error { return s.w.execute(nil) })
	scenario.Step(`^I run gitlab-sync with "([^"]*)"$`, func(flags string) error {
		return s.w.execute(strings.Fields(flags))
	})
}
