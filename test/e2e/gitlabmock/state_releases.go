package gitlabmock

import (
	"fmt"
	"slices"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

// AddIssue adds an issue to a project. state is "opened" or "closed".
func (s *Server) AddIssue(fullPath, title, description, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	target.issues = append(target.issues, s.newIssue(target, title, description, state))

	return nil
}

func (s *Server) newIssue(target *project, title, description, state string) *gitlab.Issue {
	now := time.Now().UTC()

	return &gitlab.Issue{
		ID:          s.id(),
		IID:         int64(len(target.issues) + 1),
		ProjectID:   target.ID,
		Title:       title,
		Description: description,
		State:       state,
		CreatedAt:   &now,
		UpdatedAt:   &now,
	}
}

// Issues returns a copy of the issues of a project.
func (s *Server) Issues(fullPath string) []gitlab.Issue {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.projectByPath(fullPath)
	if found == nil {
		return nil
	}

	issues := make([]gitlab.Issue, 0, len(found.issues))
	for _, issue := range found.issues {
		issues = append(issues, *issue)
	}

	return issues
}

// AddRelease adds a release to a project.
func (s *Server) AddRelease(fullPath, tag, name, description string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	now := time.Now().UTC()
	target.releases = append(target.releases, &gitlab.Release{TagName: tag, Name: name, Description: description, CreatedAt: &now, ReleasedAt: &now})

	return nil
}

// AddReleaseLink adds an asset link to the release tag of a project. directPath
// is the optional direct asset path ("/bin/tool").
func (s *Server) AddReleaseLink(fullPath, tag, name, url, directPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	target.links[tag] = append(target.links[tag], s.newReleaseLink(target, tag, name, url, directPath, "other"))

	return nil
}

func (s *Server) newReleaseLink(target *project, tag, name, url, directPath, linkType string) *gitlab.ReleaseLink {
	link := &gitlab.ReleaseLink{ID: s.id(), Name: name, URL: url, LinkType: gitlab.LinkTypeValue(linkType), DirectAssetURL: url}
	if directPath != "" {
		link.DirectAssetURL = s.publicBase + "/" + target.PathWithNamespace + "/-/releases/" + tag + "/downloads" + directPath
	}

	return link
}

// Releases returns a copy of the releases of a project, with their links.
func (s *Server) Releases(fullPath string) []gitlab.Release {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.projectByPath(fullPath)
	if found == nil {
		return nil
	}

	releases := make([]gitlab.Release, 0, len(found.releases))
	for _, release := range found.releases {
		releases = append(releases, found.releaseView(release))
	}

	return releases
}

// releaseView copies a release and fills its asset links.
func (p *project) releaseView(release *gitlab.Release) gitlab.Release {
	view := *release

	view.Assets.Links = append(view.Assets.Links, p.links[release.TagName]...)

	view.Assets.Count = int64(len(view.Assets.Links))

	return view
}

// RemoveRelease deletes the release of a tag (its tag is left alone).
func (s *Server) RemoveRelease(fullPath, tag string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	target.removeRelease(tag)

	return nil
}

// RemoveReleaseLink deletes a link of a release by name.
func (s *Server) RemoveReleaseLink(fullPath, tag, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	kept := target.links[tag][:0]

	for _, link := range target.links[tag] {
		if link.Name != name {
			kept = append(kept, link)
		}
	}

	target.links[tag] = kept

	return nil
}

func (p *project) removeRelease(tag string) {
	for index, release := range p.releases {
		if release.TagName == tag {
			p.releases = append(p.releases[:index], p.releases[index+1:]...)

			break
		}
	}

	delete(p.links, tag)
}

// dropReleasesOfDeletedTags deletes the releases whose tag is gone from the
// repository, like GitLab does when a tag is deleted. The caller holds the lock.
func (p *project) dropReleasesOfDeletedTags() {
	repository, err := p.repo()
	if err != nil {
		return
	}

	for _, release := range slices.Clone(p.releases) {
		_, tagErr := repository.Reference(plumbing.NewTagReferenceName(release.TagName), true)
		if tagErr != nil {
			p.removeRelease(release.TagName)
		}
	}
}
