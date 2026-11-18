//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/boxboxjason/gitlab-sync/test/e2e/gitlabmock"
	"github.com/cucumber/godog"
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

type projectOption = gitlabmock.ProjectOption

// projectOptionFor turns a row of a "has the project with:" table into an option.
func projectOptionFor(attribute, value string) (projectOption, error) {
	switch attribute {
	case "description":
		return gitlabmock.WithDescription(value), nil
	case "visibility":
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
	scenario.Step(`^the exit code is (\d+)$`, s.exitCode)
	scenario.Step(`^the exit code is not 0$`, s.exitCodeNotZero)
	scenario.Step(`^the (output|stdout|stderr) matches "(.*)"$`, s.outputMatches)
	scenario.Step(`^the (output|stdout|stderr) does not match "(.*)"$`, s.outputDoesNotMatch)

	scenario.Step(`^the `+role+` received no write request$`, s.noWriteRequest)
	scenario.Step(`^the `+role+` received a `+method+` request on "([^"]+)"$`, s.receivedRequest)
	scenario.Step(`^the `+role+` received a `+method+` request on "([^"]+)" with the query "([^"]*)"$`, s.receivedRequestWithQuery)
	scenario.Step(`^the `+role+` received no `+method+` request on "([^"]+)"$`, s.receivedNoRequest)
	scenario.Step(`^the `+role+` received (\d+) `+method+` requests? on "([^"]+)"$`, s.receivedRequestCount)
	scenario.Step(`^every request to the `+role+` carried the User-Agent "(.*)"$`, func(role, pattern string) error {
		return s.userAgent(role, pattern, "")
	})
	scenario.Step(`^every API request to the `+role+` carried the User-Agent "(.*)"$`, func(role, pattern string) error {
		return s.userAgent(role, pattern, "/api/")
	})
	scenario.Step(`^run (\d+) downloaded less from the source than run (\d+)$`, s.downloadedLess)
	scenario.Step(`^run (\d+) downloaded as much from the source as run (\d+)$`, s.downloadedSame)

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
	scenario.Step(`^the destination project "([^"]+)" contains the (opened|closed) issue "([^"]*)"$`, s.hasIssue)
	scenario.Step(`^the destination project "([^"]+)" has (\d+) issues?$`, s.issueCount)
	scenario.Step(`^the destination project "([^"]+)" contains the release "([^"]+)"$`, s.hasRelease)
	scenario.Step(`^the destination project "([^"]+)" contains no release "([^"]+)"$`, s.hasNoRelease)
	scenario.Step(`^the destination project "([^"]+)" release "([^"]+)" contains the link "([^"]*)" to "(.*)"$`, s.hasLink)
	scenario.Step(`^the destination project "([^"]+)" release "([^"]+)" has (\d+) links?$`, s.linkCount)
	scenario.Step(`^the destination project "([^"]+)" contains the generic package file "([^"]+)" with the content "([^"]*)"$`, s.hasPackageFile)
	scenario.Step(`^the destination project "([^"]+)" contains no generic package file "([^"]+)"$`, s.hasNoPackageFile)
	scenario.Step(`^the authenticated user is an owner of the destination (project|group) "([^"]+)"$`, s.isOwner)
	scenario.Step(`^the authenticated user is not a member of the destination (project|group) "([^"]+)"$`, s.isNotMember)

	scenario.Step(`^the file "([^"]+)" exists$`, s.fileExists)
	scenario.Step(`^the file "([^"]+)" matches "(.*)"$`, s.fileMatches)
	scenario.Step(`^"([^"]+)" does not exist$`, s.pathMissing)
}

// --- the run ----------------------------------------------------------------

func (s *steps) exitCode(want int) error {
	last, err := s.w.lastRun()
	if err != nil {
		return err
	}

	if last.exit != want {
		return fmt.Errorf("the exit code is %d, want %d%s", last.exit, want, s.w.describeLastRun())
	}

	return nil
}

func (s *steps) exitCodeNotZero() error {
	last, err := s.w.lastRun()
	if err != nil {
		return err
	}

	if last.exit == 0 {
		return fmt.Errorf("the exit code is 0, want a failure%s", s.w.describeLastRun())
	}

	return nil
}

func (s *steps) outputOf(stream string) (string, error) {
	last, err := s.w.lastRun()
	if err != nil {
		return "", err
	}

	switch stream {
	case "stdout":
		return last.stdout, nil
	case "stderr":
		return last.stderr, nil
	default:
		return last.stdout + last.stderr, nil
	}
}

func (s *steps) outputMatches(stream, pattern string) error {
	output, err := s.outputOf(stream)
	if err != nil {
		return err
	}

	matched, err := regexp.MatchString(pattern, output)
	if err != nil {
		return fmt.Errorf("bad pattern %q: %w", pattern, err)
	}

	if !matched {
		return fmt.Errorf("the %s does not match %q%s", stream, pattern, s.w.describeLastRun())
	}

	return nil
}

func (s *steps) outputDoesNotMatch(stream, pattern string) error {
	output, err := s.outputOf(stream)
	if err != nil {
		return err
	}

	matched, err := regexp.MatchString(pattern, output)
	if err != nil {
		return fmt.Errorf("bad pattern %q: %w", pattern, err)
	}

	if matched {
		return fmt.Errorf("the %s matches %q but should not%s", stream, pattern, s.w.describeLastRun())
	}

	return nil
}

// --- the request journal ----------------------------------------------------

func (s *steps) matching(role, method, pattern string) ([]gitlabmock.Request, error) {
	requests, err := s.w.requests(role)
	if err != nil {
		return nil, err
	}

	var matched []gitlabmock.Request

	for _, request := range requests {
		if request.Method == method && globMatches(pattern, request.Path) {
			matched = append(matched, request)
		}
	}

	return matched, nil
}

// globMatches matches path against a pattern where "*" is any run of characters.
func globMatches(pattern, path string) bool {
	return regexp.MustCompile("^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$").MatchString(path)
}

func (s *steps) noWriteRequest(role string) error {
	requests, err := s.w.requests(role)
	if err != nil {
		return err
	}

	var writes []string

	for _, request := range requests {
		if request.IsWrite() {
			writes = append(writes, request.String())
		}
	}

	if len(writes) > 0 {
		return fmt.Errorf("the %s received write requests: %s", role, strings.Join(writes, ", "))
	}

	return nil
}

func (s *steps) receivedRequest(role, method, pattern string) error {
	matched, err := s.matching(role, method, pattern)
	if err != nil {
		return err
	}

	if len(matched) == 0 {
		return fmt.Errorf("the %s received no %s request on %s%s", role, method, pattern, s.requestsOf(role))
	}

	return nil
}

func (s *steps) receivedRequestWithQuery(role, method, pattern, query string) error {
	matched, err := s.matching(role, method, pattern)
	if err != nil {
		return err
	}

	for _, request := range matched {
		if strings.Contains(request.Query, query) {
			return nil
		}
	}

	return fmt.Errorf("the %s received no %s request on %s with the query %q%s", role, method, pattern, query, s.requestsOf(role))
}

func (s *steps) receivedNoRequest(role, method, pattern string) error {
	matched, err := s.matching(role, method, pattern)
	if err != nil {
		return err
	}

	if len(matched) > 0 {
		return fmt.Errorf("the %s received %d %s request(s) on %s, first one: %s", role, len(matched), method, pattern, matched[0])
	}

	return nil
}

func (s *steps) receivedRequestCount(role string, count int, method, pattern string) error {
	matched, err := s.matching(role, method, pattern)
	if err != nil {
		return err
	}

	if len(matched) != count {
		return fmt.Errorf("the %s received %d %s request(s) on %s, want %d%s", role, len(matched), method, pattern, count, s.requestsOf(role))
	}

	return nil
}

// userAgent checks the User-Agent of the requests of the last run whose path
// starts with pathPrefix (all of them when it is empty).
func (s *steps) userAgent(role, pattern, pathPrefix string) error {
	all, err := s.w.requests(role)
	if err != nil {
		return err
	}

	var requests []gitlabmock.Request

	for _, request := range all {
		if strings.HasPrefix(request.Path, pathPrefix) {
			requests = append(requests, request)
		}
	}

	if len(requests) == 0 {
		return fmt.Errorf("the %s received no request under %q", role, pathPrefix)
	}

	expression, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("bad pattern %q: %w", pattern, err)
	}

	for _, request := range requests {
		if agent := request.Header.Get("User-Agent"); !expression.MatchString(agent) {
			return fmt.Errorf("%s carried the User-Agent %q, which does not match %q", request, agent, pattern)
		}
	}

	return nil
}

// requestsOf lists what a role received during the last run, to explain a failure.
func (s *steps) requestsOf(role string) string {
	requests, err := s.w.requests(role)
	if err != nil {
		return ""
	}

	var listing bytes.Buffer

	listing.WriteString("\nrequests received by the " + role + ":")

	for _, request := range requests {
		fmt.Fprintf(&listing, "\n  %s -> %d", request, request.Status)
	}

	return listing.String()
}

// downloaded is how many bytes of git objects the source sent during run number
// n (counted from 1).
func (s *steps) downloaded(n int) (int64, error) {
	if n < 1 || n > len(s.w.runs) {
		return 0, fmt.Errorf("there is no run %d, %d run(s) were made", n, len(s.w.runs))
	}

	requests := s.w.source.Requests()
	window := requests[s.w.runs[n-1].started[s.w.source]:]

	// A run's requests end where the next run's start.
	if n < len(s.w.runs) {
		window = requests[s.w.runs[n-1].started[s.w.source]:s.w.runs[n].started[s.w.source]]
	}

	var total int64

	for _, request := range window {
		if request.Method == "POST" && strings.HasSuffix(request.Path, "/git-upload-pack") {
			total += request.Bytes
		}
	}

	return total, nil
}

func (s *steps) downloadedLess(later, earlier int) error {
	laterBytes, err := s.downloaded(later)
	if err != nil {
		return err
	}

	earlierBytes, err := s.downloaded(earlier)
	if err != nil {
		return err
	}

	if laterBytes >= earlierBytes {
		return fmt.Errorf("run %d downloaded %d bytes from the source, run %d downloaded %d: want less", later, laterBytes, earlier, earlierBytes)
	}

	return nil
}

func (s *steps) downloadedSame(later, earlier int) error {
	laterBytes, err := s.downloaded(later)
	if err != nil {
		return err
	}

	earlierBytes, err := s.downloaded(earlier)
	if err != nil {
		return err
	}

	if laterBytes != earlierBytes {
		return fmt.Errorf("run %d downloaded %d bytes from the source, run %d downloaded %d: want the same", later, laterBytes, earlier, earlierBytes)
	}

	return nil
}

// --- the state of the fake instances ----------------------------------------

func (s *steps) hasEntity(role, kind, path string) error {
	if s.entityExists(role, kind, path) {
		return nil
	}

	return fmt.Errorf("the %s has no %s %q (it has %s)%s", role, kind, path, s.entities(role, kind), s.w.describeLastRun())
}

func (s *steps) hasNoEntity(role, kind, path string) error {
	if s.entityExists(role, kind, path) {
		return fmt.Errorf("the %s has the %s %q", role, kind, path)
	}

	return nil
}

func (s *steps) entityExists(role, kind, path string) bool {
	if kind == "project" {
		_, ok := s.w.server(role).Project(path)

		return ok
	}

	_, ok := s.w.server(role).Group(path)

	return ok
}

func (s *steps) entities(role, kind string) string {
	if kind == "project" {
		return fmt.Sprint(s.w.server(role).ProjectPaths())
	}

	return fmt.Sprint(s.w.server(role).GroupPaths())
}

func (s *steps) projectAttribute(role, path, attribute, want string) error {
	project, ok := s.w.server(role).Project(path)
	if !ok {
		return fmt.Errorf("the %s has no project %q", role, path)
	}

	got := map[string]string{
		"description":    project.Description,
		"visibility":     string(project.Visibility),
		"default branch": project.DefaultBranch,
		"name":           project.Name,
	}[attribute]

	if got != want {
		return fmt.Errorf("the %s project %s has the %s %q, want %q", role, path, attribute, got, want)
	}

	return nil
}

func (s *steps) groupAttribute(role, path, attribute, want string) error {
	group, ok := s.w.server(role).Group(path)
	if !ok {
		return fmt.Errorf("the %s has no group %q", role, path)
	}

	got := map[string]string{
		"description": group.Description,
		"visibility":  string(group.Visibility),
		"name":        group.Name,
	}[attribute]

	if got != want {
		return fmt.Errorf("the %s group %s has the %s %q, want %q", role, path, attribute, got, want)
	}

	return nil
}

func (s *steps) topics(role, path, want string) error {
	project, ok := s.w.server(role).Project(path)
	if !ok {
		return fmt.Errorf("the %s has no project %q", role, path)
	}

	wanted := splitList(want)
	got := slices.Clone(project.Topics)

	slices.Sort(wanted)
	slices.Sort(got)

	if !slices.Equal(got, wanted) {
		return fmt.Errorf("the %s project %s has the topics %v, want %v", role, path, project.Topics, wanted)
	}

	return nil
}

func (s *steps) refs(role, path string) (map[string]string, error) {
	return s.w.server(role).Refs(path)
}

func (s *steps) sameRefs(destinationPath, sourcePath string) error {
	want, err := s.refs(roleSource, sourcePath)
	if err != nil {
		return err
	}

	got, err := s.refs(roleDestination, destinationPath)
	if err != nil {
		return err
	}

	if len(want) == 0 {
		return fmt.Errorf("the source project %s has no refs, the comparison would prove nothing", sourcePath)
	}

	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("the destination project %s has the refs %v, want those of the source project %s: %v%s", destinationPath, got, sourcePath, want, s.w.describeLastRun())
	}

	return nil
}

func (s *steps) hasAvatar(kind, path, sourcePath string) error {
	want := []byte("avatar of " + sourcePath)

	got := s.w.destination.ProjectAvatar(path)
	if kind == "group" {
		got = s.w.destination.GroupAvatar(path)
	}

	if string(got) != string(want) {
		return fmt.Errorf("the destination %s %s has the avatar %q, want %q", kind, path, got, want)
	}

	return nil
}

func (s *steps) noRefs(path string) error {
	got, err := s.refs(roleDestination, path)
	if err != nil {
		return err
	}

	if len(got) > 0 {
		return fmt.Errorf("the destination project %s has the refs %v, want none", path, got)
	}

	return nil
}

func (s *steps) hasRef(path, ref string) error {
	got, err := s.refs(roleDestination, path)
	if err != nil {
		return err
	}

	if _, ok := got[ref]; !ok {
		return fmt.Errorf("the destination project %s has the refs %v, want %s", path, got, ref)
	}

	return nil
}

func (s *steps) hasNoRef(path, ref string) error {
	got, err := s.refs(roleDestination, path)
	if err != nil {
		return err
	}

	if _, ok := got[ref]; ok {
		return fmt.Errorf("the destination project %s still has %s", path, ref)
	}

	return nil
}

func (s *steps) destinationProject(path string) (gitlab.Project, error) {
	project, ok := s.w.destination.Project(path)
	if !ok {
		return project, fmt.Errorf("the destination has no project %q (it has %v)%s", path, s.w.destination.ProjectPaths(), s.w.describeLastRun())
	}

	return project, nil
}

func (s *steps) pullMirror(path, sourcePath string) error {
	project, err := s.destinationProject(path)
	if err != nil {
		return err
	}

	want := s.w.source.URL() + "/" + sourcePath + ".git"
	if !project.Mirror || project.ImportURL != want {
		return fmt.Errorf("the destination project %s has mirror=%t and import URL %q, want a pull mirror of %q", path, project.Mirror, project.ImportURL, want)
	}

	return nil
}

func (s *steps) notPullMirror(path string) error {
	project, err := s.destinationProject(path)
	if err != nil {
		return err
	}

	if project.Mirror || project.ImportURL != "" {
		return fmt.Errorf("the destination project %s is a pull mirror (mirror=%t, import URL %q)", path, project.Mirror, project.ImportURL)
	}

	return nil
}

func (s *steps) inCatalog(path string) error {
	project, err := s.destinationProject(path)
	if err != nil {
		return err
	}

	if !project.CICDCatalogEnabled {
		return fmt.Errorf("the destination project %s is not in the CI/CD catalog%s", path, s.w.describeLastRun())
	}

	return nil
}

func (s *steps) notInCatalog(path string) error {
	project, err := s.destinationProject(path)
	if err != nil {
		return err
	}

	if project.CICDCatalogEnabled {
		return fmt.Errorf("the destination project %s is in the CI/CD catalog", path)
	}

	return nil
}

func (s *steps) triggerBuilds(path, want string) error {
	project, err := s.destinationProject(path)
	if err != nil {
		return err
	}

	if fmt.Sprint(project.MirrorTriggerBuilds) != want {
		return fmt.Errorf("the destination project %s has mirror_trigger_builds=%t, want %s", path, project.MirrorTriggerBuilds, want)
	}

	return nil
}

func (s *steps) hasIssue(path, state, title string) error {
	for _, issue := range s.w.destination.Issues(path) {
		if issue.Title == title && issue.State == state {
			return nil
		}
	}

	return fmt.Errorf("the destination project %s has no %s issue %q, it has %s", path, state, title, s.issueSummary(path))
}

func (s *steps) issueSummary(path string) string {
	var summary []string

	for _, issue := range s.w.destination.Issues(path) {
		summary = append(summary, fmt.Sprintf("%s (%s)", issue.Title, issue.State))
	}

	return fmt.Sprint(summary)
}

func (s *steps) issueCount(path string, want int) error {
	if got := len(s.w.destination.Issues(path)); got != want {
		return fmt.Errorf("the destination project %s has %d issue(s), want %d: %s", path, got, want, s.issueSummary(path))
	}

	return nil
}

func (s *steps) hasRelease(path, tag string) error {
	for _, release := range s.w.destination.Releases(path) {
		if release.TagName == tag {
			return nil
		}
	}

	return fmt.Errorf("the destination project %s has no release %q%s", path, tag, s.w.describeLastRun())
}

func (s *steps) hasNoRelease(path, tag string) error {
	for _, release := range s.w.destination.Releases(path) {
		if release.TagName == tag {
			return fmt.Errorf("the destination project %s still has the release %q", path, tag)
		}
	}

	return nil
}

func (s *steps) releaseLinks(path, tag string) ([]*gitlab.ReleaseLink, error) {
	for _, release := range s.w.destination.Releases(path) {
		if release.TagName == tag {
			return release.Assets.Links, nil
		}
	}

	return nil, fmt.Errorf("the destination project %s has no release %q", path, tag)
}

func (s *steps) hasLink(path, tag, name, target string) error {
	links, err := s.releaseLinks(path, tag)
	if err != nil {
		return err
	}

	target = s.w.expand(target)

	for _, link := range links {
		if link.Name == name && link.URL == target {
			return nil
		}
	}

	var found []string
	for _, link := range links {
		found = append(found, link.Name+" -> "+link.URL)
	}

	return fmt.Errorf("the release %s of the destination project %s has no link %q to %q, it has %v", tag, path, name, target, found)
}

func (s *steps) linkCount(path, tag string, want int) error {
	links, err := s.releaseLinks(path, tag)
	if err != nil {
		return err
	}

	if len(links) != want {
		return fmt.Errorf("the release %s of the destination project %s has %d link(s), want %d", tag, path, len(links), want)
	}

	return nil
}

func (s *steps) hasPackageFile(path, file, content string) error {
	packageName, version, fileName, err := splitPackageFile(file)
	if err != nil {
		return err
	}

	var found []string

	for _, candidate := range s.w.destination.PackageFiles(path) {
		found = append(found, candidate.Package+"/"+candidate.Version+"/"+candidate.File)

		if candidate.Package == packageName && candidate.Version == version && candidate.File == fileName && string(candidate.Content) == content {
			return nil
		}
	}

	return fmt.Errorf("the destination project %s has no generic package file %s with the content %q, it has %v%s", path, file, content, found, s.w.describeLastRun())
}

func (s *steps) hasNoPackageFile(path, file string) error {
	packageName, version, fileName, err := splitPackageFile(file)
	if err != nil {
		return err
	}

	for _, candidate := range s.w.destination.PackageFiles(path) {
		if candidate.Package == packageName && candidate.Version == version && candidate.File == fileName {
			return fmt.Errorf("the destination project %s still has the generic package file %s", path, file)
		}
	}

	return nil
}

const ownerLevel = 50

func (s *steps) membership(kind, path string) (int64, bool) {
	if kind == "project" {
		return s.w.destination.ProjectMember(path)
	}

	return s.w.destination.GroupMember(path)
}

func (s *steps) isOwner(kind, path string) error {
	level, ok := s.membership(kind, path)
	if !ok || level != ownerLevel {
		return fmt.Errorf("the authenticated user has access level %d (member: %t) on the destination %s %s, want owner (%d)", level, ok, kind, path, ownerLevel)
	}

	return nil
}

func (s *steps) isNotMember(kind, path string) error {
	if level, ok := s.membership(kind, path); ok {
		return fmt.Errorf("the authenticated user is a member of the destination %s %s (access level %d)", kind, path, level)
	}

	return nil
}

// --- files ------------------------------------------------------------------

func (s *steps) fileExists(path string) error {
	if _, err := os.Stat(s.w.expand(path)); err != nil {
		return fmt.Errorf("the file %s does not exist: %w", path, err)
	}

	return nil
}

func (s *steps) fileMatches(path, pattern string) error {
	content, err := os.ReadFile(s.w.expand(path))
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	matched, err := regexp.Match(pattern, content)
	if err != nil {
		return fmt.Errorf("bad pattern %q: %w", pattern, err)
	}

	if !matched {
		return fmt.Errorf("the file %s does not match %q, it holds:\n%s", path, pattern, content)
	}

	return nil
}

func (s *steps) pathMissing(path string) error {
	if _, err := os.Stat(s.w.expand(path)); err == nil {
		return fmt.Errorf("%s exists", path)
	}

	return nil
}
