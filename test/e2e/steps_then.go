//go:build e2e

package e2e

import (
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"github.com/boxboxjason/gitlab-sync/test/e2e/gitlabmock"
)

type projectOption = gitlabmock.ProjectOption

const (
	attrDescription = "description"
	attrVisibility  = "visibility"
	kindProject     = "project"
)

// projectOptionFor turns a row of a "has the project with:" table into an option.
func projectOptionFor(attribute, value string) (projectOption, error) {
	switch attribute {
	case attrDescription:
		return gitlabmock.WithDescription(value), nil
	case attrVisibility:
		return gitlabmock.WithVisibility(value), nil
	case "default branch":
		return gitlabmock.WithDefaultBranch(value), nil
	case "topics":
		return gitlabmock.WithTopics(splitList(value)...), nil
	default:
		return nil, fmt.Errorf("unknown project attribute %q (want description, visibility, default branch or topics)", attribute)
	}
}

func splitList(value string) []string {
	var items []string

	for item := range strings.SplitSeq(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}

	return items
}

func (s *steps) registerThenSteps(scenario *godog.ScenarioContext) {
	s.registerOutputSteps(scenario)
	s.registerRequestSteps(scenario)
	s.registerEntitySteps(scenario)
	s.registerReleaseSteps(scenario)
}

func (s *steps) registerOutputSteps(scenario *godog.ScenarioContext) {
	scenario.Step(`^the exit code is (\d+)$`, s.exitCode)
	scenario.Step(`^the exit code is not 0$`, s.exitCodeNotZero)
	scenario.Step(`^the (output|stdout|stderr) matches "(.*)"$`, s.outputMatches)
	scenario.Step(`^the (output|stdout|stderr) does not match "(.*)"$`, s.outputDoesNotMatch)

	scenario.Step(`^the file "([^"]+)" exists$`, s.fileExists)
	scenario.Step(`^the file "([^"]+)" matches "(.*)"$`, s.fileMatches)
	scenario.Step(`^"([^"]+)" does not exist$`, s.pathMissing)
}

func (s *steps) registerRequestSteps(scenario *godog.ScenarioContext) {
	scenario.Step(`^the `+role+` received no write request$`, s.noWriteRequest)
	scenario.Step(`^the `+role+` received a `+method+` request on "([^"]+)"$`, s.receivedRequest)
	scenario.Step(`^the `+role+` received a `+method+` request on "([^"]+)" with the query "([^"]*)"$`, s.receivedRequestWithQuery)
	scenario.Step(`^the `+role+` received no `+method+` request on "([^"]+)"$`, s.receivedNoRequest)
	scenario.Step(`^the `+role+` received (\d+) `+method+` requests? on "([^"]+)"$`, s.receivedRequestCount)
	scenario.Step(`^the `+role+` received a `+method+` request on "([^"]+)" with the body "(.*)"$`, s.receivedRequestWithBody)
	scenario.Step(`^the `+role+` received no `+method+` request on "([^"]+)" with the body "(.*)"$`, s.receivedNoRequestWithBody)
	scenario.Step(`^the destination received the push option "([^"]+)"$`, s.pushOption)
	scenario.Step(`^the destination received no push option$`, s.noPushOption)
	scenario.Step(`^the source group "([^"]+)" is still (public|internal|private)$`, s.groupStillVisibility)
	scenario.Step(`^the destination project "([^"]+)" has (\d+) generic package files?$`, s.packageFileCount)
	scenario.Step(`^the destination project "([^"]+)" release "([^"]+)" contains the link "([^"]*)" to the package file "([^"]+)"$`, s.hasLinkToPackage)
	scenario.Step(`^every request to the `+role+` carried the User-Agent "(.*)"$`, func(role, pattern string) error {
		return s.userAgent(role, pattern, "")
	})
	scenario.Step(`^every API request to the `+role+` carried the User-Agent "(.*)"$`, func(role, pattern string) error {
		return s.userAgent(role, pattern, "/api/")
	})
	scenario.Step(`^run (\d+) downloaded less from the source than run (\d+)$`, s.downloadedLess)
	scenario.Step(`^run (\d+) downloaded as much from the source as run (\d+)$`, s.downloadedSame)
}

func (s *steps) registerEntitySteps(scenario *godog.ScenarioContext) {
	scenario.Step(`^the `+role+` contains the (project|group) "([^"]+)"$`, s.hasEntity)
	scenario.Step(`^the `+role+` contains no (project|group) "([^"]+)"$`, s.hasNoEntity)
	scenario.Step(`^the `+role+` project "([^"]+)" has the (description|visibility|default branch|name) "(.*)"$`, s.projectAttribute)
	scenario.Step(`^the `+role+` group "([^"]+)" has the (description|visibility|name) "(.*)"$`, s.groupAttribute)
	scenario.Step(`^the `+role+` project "([^"]+)" has the topics "(.*)"$`, s.topics)
	scenario.Step(`^the destination project "([^"]+)" has the same refs as the source project "([^"]+)"$`, s.sameRefs)
	scenario.Step(`^the destination (group|project) "([^"]+)" has the avatar of the source (?:group|project) "([^"]+)"$`, s.hasAvatar)
	scenario.Step(`^the destination project "([^"]+)" has no refs$`, s.noRefs)
	scenario.Step(`^the destination project "([^"]+)" contains the ref "([^"]+)"$`, s.hasRef)
	scenario.Step(`^the destination project "([^"]+)" contains no ref "([^"]+)"$`, s.hasNoRef)
	scenario.Step(`^the destination project "([^"]+)" is a pull mirror of the source project "([^"]+)"$`, s.pullMirror)
	scenario.Step(`^the destination project "([^"]+)" is not a pull mirror$`, s.notPullMirror)
	scenario.Step(`^the destination project "([^"]+)" is in the CI/CD catalog$`, s.inCatalog)
	scenario.Step(`^the destination project "([^"]+)" is not in the CI/CD catalog$`, s.notInCatalog)
	scenario.Step(`^the destination project "([^"]+)" mirror_trigger_builds is (true|false)$`, s.triggerBuilds)
}

func (s *steps) registerReleaseSteps(scenario *godog.ScenarioContext) {
	scenario.Step(`^the destination project "([^"]+)" contains the (opened|closed) issue "([^"]*)"$`, s.hasIssue)
	scenario.Step(`^the destination project "([^"]+)" has (\d+) issues?$`, s.issueCount)
	scenario.Step(`^the destination project "([^"]+)" contains the release "([^"]+)"$`, s.hasRelease)
	scenario.Step(`^the destination project "([^"]+)" contains no release "([^"]+)"$`, s.hasNoRelease)
	scenario.Step(`^the destination project "([^"]+)" release "([^"]+)" contains the link "([^"]*)" to "(.*)"$`, s.hasLink)
	scenario.Step(`^the destination project "([^"]+)" release "([^"]+)" has (\d+) links?$`, s.linkCount)
	scenario.Step(`^the destination project "([^"]+)" release "([^"]+)" has the (name|description) "(.*)"$`, s.releaseAttribute)
	scenario.Step(`^the destination project "([^"]+)" contains the generic package file "([^"]+)" with the content "([^"]*)"$`, s.hasPackageFile)
	scenario.Step(`^the destination project "([^"]+)" contains no generic package file "([^"]+)"$`, s.hasNoPackageFile)
	scenario.Step(`^the authenticated user is an owner of the destination (project|group) "([^"]+)"$`, s.isOwner)
	scenario.Step(`^the authenticated user is not a member of the destination (project|group) "([^"]+)"$`, s.isNotMember)
}
