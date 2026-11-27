//go:build e2e

package e2e

import (
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/cucumber/godog"
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

// tokenLifetime is how long the tokens of the scenarios are valid.
const tokenLifetime = 48 * time.Hour

func (s *steps) registerAccountSteps(scenario *godog.ScenarioContext) {
	scenario.Step(`^gitlab-sync uses a different account on each instance$`, s.separateAccounts)
	scenario.Step(`^gitlab-sync runs on the destination as an administrator who is a member of nothing$`, s.separateDestinationAdmin)
	scenario.Step(`^gitlab-sync runs on the destination as a maintainer of the group "([^"]+)"$`, s.destinationMaintainer)
	scenario.Step(`^gitlab-sync runs on the source as a guest of the private project "([^"]+)"$`, s.sourceGuest)
	scenario.Step(`^the `+role+` token is unknown to the instance$`, s.unknownToken)
	scenario.Step(`^the `+role+` token only has the scope "([^"]+)"$`, s.scopedToken)
	scenario.Step(`^the destination project "([^"]+)" has the authenticated user as developer$`, s.developerMember)
	scenario.Step(`^the destination project "([^"]+)" has a protected default branch$`, s.protectedDefaultBranch)
	scenario.Step(`^the `+role+` cannot be reached$`, s.unreachable)
	scenario.Step(`^the `+role+` answers (\d+)(?: (\d+) times?)? on `+method+` (\S+)$`, s.fault)
}

// account is a user of the instance, along with a token of theirs.
type account struct {
	token  string
	userID int64
}

// newAccount creates a user of the instance.
func (s *steps) newAccount(admin bool, scopes ...string) (account, error) {
	s.w.users++

	name := fmt.Sprintf("e2e-%s-%d", s.w.suffix, s.w.users)

	user, _, err := s.w.env.client.Users.CreateUser(&gitlab.CreateUserOptions{
		Name:             &name,
		Username:         &name,
		Email:            new(name + "@example.com"),
		Password:         new(rand.Text()),
		Admin:            &admin,
		SkipConfirmation: new(true),
	})
	if err != nil {
		return account{}, fmt.Errorf("create the user %s: %w", name, err)
	}

	token, err := s.newToken(user.ID, scopes...)

	return account{userID: user.ID, token: token}, err
}

func (s *steps) newToken(userID int64, scopes ...string) (string, error) {
	if len(scopes) == 0 {
		scopes = []string{"api", "read_repository", "write_repository"}
	}

	token, _, err := s.w.env.client.Users.CreatePersonalAccessToken(userID, &gitlab.CreatePersonalAccessTokenOptions{
		Name:      new("e2e-" + strconv.FormatInt(time.Now().UnixNano(), 36)),
		Scopes:    &scopes,
		ExpiresAt: new(gitlab.ISOTime(time.Now().Add(tokenLifetime))),
	})
	if err != nil {
		return "", fmt.Errorf("create a token for the user %d: %w", userID, err)
	}

	return token.Token, nil
}

func (s *steps) separateAccounts() error {
	source, err := s.newAccount(true)
	if err != nil {
		return err
	}

	s.w.tokens[roleSource] = source.token

	return s.separateDestinationAdmin()
}

func (s *steps) separateDestinationAdmin() error {
	destination, err := s.newAccount(true)
	if err != nil {
		return err
	}

	s.useDestinationAccount(destination)

	return nil
}

func (s *steps) destinationMaintainer(group string) error {
	groupID, err := s.ensureGroup(s.w.fullPath(roleDestination, group))
	if err != nil {
		return err
	}

	maintainer, err := s.newAccount(false)
	if err != nil {
		return err
	}

	_, _, err = s.w.env.client.GroupMembers.AddGroupMember(groupID, &gitlab.AddGroupMemberOptions{
		UserID:      &maintainer.userID,
		AccessLevel: new(gitlab.MaintainerPermissions),
	})
	if err != nil {
		return fmt.Errorf("add the maintainer of %s: %w", group, err)
	}

	s.useDestinationAccount(maintainer)

	return nil
}

// useDestinationAccount runs gitlab-sync as a given user on the destination.
func (s *steps) useDestinationAccount(user account) {
	s.w.tokens[roleDestination], s.w.destinationUser = user.token, user.userID
}

// sourceGuest runs gitlab-sync as a user who can see a private project but is
// not allowed to download its code.
func (s *steps) sourceGuest(name string) error {
	guest, err := s.newAccount(false)
	if err != nil {
		return err
	}

	fullPath := s.w.fullPath(roleSource, name)

	_, _, err = s.w.env.client.Projects.EditProject(fullPath, &gitlab.EditProjectOptions{Visibility: new(gitlab.PrivateVisibility)})
	if err != nil {
		return fmt.Errorf("make %s private: %w", fullPath, err)
	}

	_, _, err = s.w.env.client.ProjectMembers.AddProjectMember(fullPath, &gitlab.AddProjectMemberOptions{
		UserID:      &guest.userID,
		AccessLevel: new(gitlab.GuestPermissions),
	})
	if err != nil {
		return fmt.Errorf("add the guest of %s: %w", fullPath, err)
	}

	s.w.tokens[roleSource] = guest.token

	return s.waitForAccess(guest.token, fullPath)
}

// waitForAccess waits until a token lists a project: GitLab grants access in the
// background.
func (s *steps) waitForAccess(token, fullPath string) error {
	client, err := gitlab.NewClient(token, gitlab.WithBaseURL(s.w.env.url))
	if err != nil {
		return fmt.Errorf("create a GitLab client: %w", err)
	}

	return eventually(func() error {
		projects, _, listErr := client.Projects.ListProjects(&gitlab.ListProjectsOptions{
			Membership: new(true),
		})
		if listErr != nil {
			return fmt.Errorf("list the projects: %w", listErr)
		}

		if !slices.ContainsFunc(projects, func(project *gitlab.Project) bool { return project.PathWithNamespace == fullPath }) {
			return fmt.Errorf("%s is not listed yet", fullPath)
		}

		return nil
	})
}

func (s *steps) unknownToken(role string) error {
	s.w.tokens[role] = "glpat-unknown-" + s.w.suffix

	return nil
}

func (s *steps) scopedToken(role, scope string) error {
	scoped, err := s.newAccount(true, scope)
	if err != nil {
		return err
	}

	if role == roleDestination {
		s.useDestinationAccount(scoped)
	} else {
		s.w.tokens[role] = scoped.token
	}

	return nil
}

func (s *steps) developerMember(name string) error {
	userID, err := s.w.destinationUserID()
	if err != nil {
		return err
	}

	_, _, err = s.w.env.client.ProjectMembers.AddProjectMember(s.w.fullPath(roleDestination, name), &gitlab.AddProjectMemberOptions{
		UserID:      &userID,
		AccessLevel: new(gitlab.DeveloperPermissions),
	})
	if err != nil {
		return fmt.Errorf("add the developer of %s: %w", name, err)
	}

	return nil
}

// protectedDefaultBranch waits until GitLab protected the default branch, which
// it does in the background after the first commit.
func (s *steps) protectedDefaultBranch(name string) error {
	fullPath := s.w.fullPath(roleDestination, name)

	return eventually(func() error {
		branch, _, err := s.w.env.client.Branches.GetBranch(fullPath, defaultBranch)
		if err != nil {
			return fmt.Errorf("get the default branch of %s: %w", fullPath, err)
		}

		if !branch.Protected {
			return fmt.Errorf("the default branch of %s is not protected", fullPath)
		}

		return nil
	})
}

func (s *steps) unreachable(role string) error {
	return s.w.makeUnreachable(role)
}

// errNotObserved is returned by the steps that need a proxy.
var errNotObserved = errors.New("the scenario needs the " + observedTag + " tag")

// fault fails the requests matching the method and the path pattern; times is
// empty when every matching request must fail.
func (s *steps) fault(role string, status int, times, method, pattern string) error {
	running, ok := s.w.proxies[role]
	if !ok {
		return errNotObserved
	}

	count := -1

	if times != "" {
		parsed, err := strconv.Atoi(times)
		if err != nil {
			return fmt.Errorf("parse the number of failures %q: %w", times, err)
		}

		count = parsed
	}

	running.inject(&fault{method: method, pattern: s.w.expand(pattern), status: status, remaining: count})

	return nil
}
