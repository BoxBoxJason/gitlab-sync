//go:build e2e

package e2e

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"

	"github.com/cucumber/godog"
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

const (
	defaultBranch = "main"

	// tableColumns is the width of the attribute / value tables.
	tableColumns = 2
	// packageFileParts is the number of parts of a "name/version/file" spec.
	packageFileParts = 3
	// seedWorkers bounds how many projects are generated at the same time.
	seedWorkers = 8
	// avatarSize is the width and height of the generated avatars.
	avatarSize = 8

	attrDescription = "description"
	attrVisibility  = "visibility"
	kindGroup       = "group"
)

func (s *steps) registerGivenSteps(scenario *godog.ScenarioContext) {
	scenario.Step(`^the source has the read-only project "`+sharedProject+`" with a commit on "`+defaultBranch+`"$`, s.useSharedSource)
	scenario.Step(`^the `+role+` has the group "([^"]+)"$`, s.group)
	scenario.Step(`^the `+role+` has the project "([^"]+)"$`, s.project)
	scenario.Step(`^the `+role+` project "([^"]+)" exists$`, s.project)
	scenario.Step(`^the source project "([^"]+)" exists with the description "([^"]*)" and the topics "([^"]*)"$`, s.projectWithTopics)
	scenario.Step(`^the `+role+` has the project "([^"]+)" with:$`, s.projectWithTable)
	scenario.Step(`^the `+role+` has the project "([^"]+)" with a commit on "([^"]+)"$`, s.projectWithCommit)
	scenario.Step(`^the `+role+` has the archived project "([^"]+)"$`, s.archivedProject)
	scenario.Step(`^the source has (\d+) projects under "([^"]+)"$`, s.generatedProjects)
	scenario.Step(`^the `+role+` project "([^"]+)" has a commit on "([^"]+)"$`, s.commit)
	scenario.Step(`^the source project "([^"]+)" has a CI configuration$`, s.ciConfiguration)
	scenario.Step(`^the `+role+` project "([^"]+)" has the tag "([^"]+)" on "([^"]+)"$`, s.tag)
	scenario.Step(`^the `+role+` project "([^"]+)" has the (opened|closed) issue "([^"]*)"$`, s.issue)
	scenario.Step(`^the `+role+` project "([^"]+)" has the release "([^"]+)"$`, s.release)
	scenario.Step(`^the source project "([^"]+)" release "([^"]+)" has the link "([^"]*)" to "([^"]*)"$`, s.releaseLink)
	scenario.Step(`^the source project "([^"]+)" release "([^"]+)" has the link "([^"]*)" to the package file "([^"]+)" at "([^"]+)"$`, s.releaseLinkToPackage)
	scenario.Step(`^the source project "([^"]+)" release "([^"]+)" has the link "([^"]*)" to the uploaded file "([^"]+)"$`, s.releaseLinkToUpload)
	scenario.Step(`^the source project "([^"]+)" has the uploaded file "([^"]+)" with the content "([^"]*)"$`, s.uploadedFile)
	scenario.Step(`^the source project "([^"]+)" has the generic package file "([^"]+)" with the content "([^"]*)"$`, s.packageFile)
	scenario.Step(`^the source (group|project) "([^"]+)" has an avatar$`, s.avatar)
	scenario.Step(`^the source project "([^"]+)" now has the description "([^"]*)"$`, s.setDescription)
	scenario.Step(`^the source project "([^"]+)" no longer has the (branch|tag) "([^"]+)"$`, s.removeRef)
	scenario.Step(`^the source project "([^"]+)" no longer has the generic package file "([^"]+)"$`, s.removePackageFile)
	scenario.Step(`^the source project "([^"]+)" no longer has the release "([^"]+)"$`, s.removeRelease)
	scenario.Step(`^the source project "([^"]+)" release "([^"]+)" no longer has the link "([^"]*)"$`, s.removeReleaseLink)
	scenario.Step(`^the mirror mapping:$`, s.mirrorMapping)
}

// ensureGroup returns the ID of a group of the scenario, creating it and its
// parents on first use. The groups are public, so that they can hold projects of
// any visibility.
func (s *steps) ensureGroup(fullPath string) (int64, error) {
	if id, ok := s.w.groups[fullPath]; ok {
		return id, nil
	}

	name := path.Base(fullPath)
	options := &gitlab.CreateGroupOptions{Name: &name, Path: &name, Visibility: new(gitlab.PublicVisibility)}

	if parent := path.Dir(fullPath); parent != "." {
		id, err := s.ensureGroup(parent)
		if err != nil {
			return 0, err
		}

		options.ParentID = &id
	}

	group, _, err := s.w.env.client.Groups.CreateGroup(options)
	if err != nil {
		return 0, fmt.Errorf("create the group %s: %w", fullPath, err)
	}

	s.w.groups[fullPath] = group.ID

	return group.ID, nil
}

func (s *steps) group(role, name string) error {
	_, err := s.ensureGroup(s.w.fullPath(role, name))

	return err
}

// createProject creates a project, public unless the options say otherwise, in
// the group its path names.
func (s *steps) createProject(role, name string, options *gitlab.CreateProjectOptions) error {
	fullPath := s.w.fullPath(role, name)

	groupID, err := s.ensureGroup(path.Dir(fullPath))
	if err != nil {
		return err
	}

	base := path.Base(fullPath)
	options.Name, options.Path, options.NamespaceID = &base, &base, &groupID

	if options.Visibility == nil {
		options.Visibility = new(gitlab.PublicVisibility)
	}

	_, _, err = s.w.env.client.Projects.CreateProject(options)
	if err != nil {
		return fmt.Errorf("create the project %s: %w", fullPath, err)
	}

	return nil
}

func (s *steps) project(role, name string) error {
	return s.createProject(role, name, &gitlab.CreateProjectOptions{})
}

func (s *steps) projectWithTopics(name, description, topics string) error {
	options := &gitlab.CreateProjectOptions{Description: &description}

	if list := splitList(topics, ","); len(list) > 0 {
		options.Topics = &list
	}

	return s.createProject(roleSource, name, options)
}

// projectWithTable creates a project with the attributes of a two columns table
// (description, visibility, default branch, topics).
func (s *steps) projectWithTable(role, name string, table *godog.Table) error {
	options := &gitlab.CreateProjectOptions{}

	for _, row := range table.Rows {
		if len(row.Cells) != tableColumns {
			return errors.New("the table must have two columns: attribute, value")
		}

		value := row.Cells[1].Value

		switch attribute := row.Cells[0].Value; attribute {
		case attrDescription:
			options.Description = &value
		case attrVisibility:
			options.Visibility = new(gitlab.VisibilityValue(value))
		case "default branch":
			options.DefaultBranch = &value
		case "topics":
			options.Topics = new(splitList(value, ","))
		default:
			return fmt.Errorf("unknown project attribute %q (want description, visibility, default branch or topics)", attribute)
		}
	}

	return s.createProject(role, name, options)
}

func (s *steps) projectWithCommit(role, name, branch string) error {
	err := s.project(role, name)
	if err != nil {
		return err
	}

	return s.commit(role, name, branch)
}

func (s *steps) archivedProject(role, name string) error {
	err := s.projectWithCommit(role, name, defaultBranch)
	if err != nil {
		return err
	}

	_, _, err = s.w.env.client.Projects.ArchiveProject(s.w.fullPath(role, name))
	if err != nil {
		return fmt.Errorf("archive the project %s: %w", name, err)
	}

	return nil
}

// generatedProjects creates empty projects named project-001, project-002, ...
func (s *steps) generatedProjects(count int, parent string) error {
	// The group is created first: the workers only read the cache of groups.
	err := s.group(roleSource, parent)
	if err != nil {
		return err
	}

	var (
		waitGroup sync.WaitGroup
		slots     = make(chan struct{}, seedWorkers)
		errs      = make([]error, count)
	)

	for index := range count {
		slots <- struct{}{}

		waitGroup.Go(func() {
			defer func() { <-slots }()

			errs[index] = s.project(roleSource, fmt.Sprintf("%s/project-%03d", parent, index+1))
		})
	}

	waitGroup.Wait()

	return errors.Join(errs...)
}

func (s *steps) mirrorMapping(doc *godog.DocString) error {
	return s.w.writeMapping(doc.Content)
}

// splitList splits a separated list, ignoring the empty items.
func splitList(value, separator string) []string {
	var items []string

	for item := range strings.SplitSeq(value, separator) {
		item = strings.TrimSpace(item)
		if item != "" {
			items = append(items, item)
		}
	}

	return items
}
