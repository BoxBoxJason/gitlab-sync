//go:build e2e

package e2e

import (
	"fmt"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

// startedPipelines counts the pipelines GitLab did not skip.
func (s *steps) startedPipelines(name string) (int, error) {
	pipelines, _, err := s.w.env.client.Pipelines.ListProjectPipelines(s.w.fullPath(roleDestination, name), nil)
	if err != nil {
		return 0, fmt.Errorf("list the pipelines of %s: %w", name, err)
	}

	count := 0

	for _, pipeline := range pipelines {
		if pipeline.Status != skippedPipeline {
			count++
		}
	}

	return count, nil
}

func (s *steps) runsPipeline(name string) error {
	return eventually(func() error {
		count, err := s.startedPipelines(name)
		if err != nil {
			return err
		}

		if count == 0 {
			return fmt.Errorf("the destination project %s has no pipeline%s", name, s.w.describe())
		}

		return nil
	})
}

// runsNoPipeline waits for GitLab to have processed the push, which is when it
// creates the pipelines, before concluding there is none.
func (s *steps) runsNoPipeline(name string) error {
	err := eventually(func() error { return s.pushProcessed(name) })
	if err != nil {
		return err
	}

	deadline := time.Now().Add(pipelineGrace)

	for {
		count, listErr := s.startedPipelines(name)
		if listErr != nil {
			return listErr
		}

		if count > 0 {
			return fmt.Errorf("the destination project %s has %d pipeline(s), want none%s", name, count, s.w.describe())
		}

		if time.Now().After(deadline) {
			return nil
		}

		time.Sleep(settleInterval)
	}
}

// pushProcessed checks that GitLab recorded the push in the activity of the project.
func (s *steps) pushProcessed(name string) error {
	events, _, err := s.w.env.client.Events.ListProjectVisibleEvents(s.w.fullPath(roleDestination, name), &gitlab.ListProjectVisibleEventsOptions{
		Action: new(gitlab.PushedEventType),
	})
	if err != nil {
		return fmt.Errorf("list the events of %s: %w", name, err)
	}

	if len(events) == 0 {
		return fmt.Errorf("GitLab has not processed a push to %s%s", name, s.w.describe())
	}

	return nil
}

// membership returns the access level of the authenticated user as a direct
// member of a destination group or project.
func (s *steps) membership(kind, name string) (gitlab.AccessLevelValue, bool, error) {
	userID, err := s.w.destinationUserID()
	if err != nil {
		return 0, false, err
	}

	fullPath := s.w.fullPath(roleDestination, name)

	if kind == kindGroup {
		member, response, getErr := s.w.env.client.GroupMembers.GetGroupMember(fullPath, userID)

		ok, lookupErr := found(response, getErr)
		if lookupErr != nil || !ok {
			return 0, false, lookupErr
		}

		return member.AccessLevel, true, nil
	}

	member, response, err := s.w.env.client.ProjectMembers.GetProjectMember(fullPath, userID)

	ok, err := found(response, err)
	if err != nil || !ok {
		return 0, false, err
	}

	return member.AccessLevel, true, nil
}

func (s *steps) isOwner(kind, name string) error {
	level, member, err := s.membership(kind, name)
	if err != nil {
		return err
	}

	if !member || level != gitlab.OwnerPermissions {
		return fmt.Errorf("the authenticated user has access level %d (member: %t) on the destination %s %s, want owner (%d)%s", level, member, kind, name, gitlab.OwnerPermissions, s.w.describe())
	}

	return nil
}

func (s *steps) isNotMember(kind, name string) error {
	level, ok, err := s.membership(kind, name)
	if err != nil {
		return err
	}

	if ok {
		return fmt.Errorf("the authenticated user is a member of the destination %s %s (access level %d)", kind, name, level)
	}

	return nil
}
