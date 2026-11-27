package mirroring

import (
	"errors"
	"fmt"
	"net/http"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.uber.org/zap"
)

const (
	releasesPerPage = 100
)

// ===========================================================================
//                   	RELEASES MIRRORING FUNCTIONS                        //
// ===========================================================================

// ================
//	      GET
// ================

// FetchProjectReleases retrieves all releases for a project and returns them.
func (g *GitlabInstance) FetchProjectReleases(project *gitlab.Project) ([]*gitlab.Release, error) {
	zap.L().Debug("Fetching releases for project", zap.String("project", project.PathWithNamespace))

	fetchOpts := &gitlab.ListReleasesOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: releasesPerPage,
			Page:    1,
		},
	}

	releases := make([]*gitlab.Release, 0)

	for {
		fetchedReleases, resp, err := g.Gitlab.Releases.ListReleases(project.ID, fetchOpts)
		if err != nil {
			return nil, fmt.Errorf("failed to list releases for project %s: %w", project.PathWithNamespace, err)
		}

		releases = append(releases, fetchedReleases...)

		if resp.CurrentPage >= resp.TotalPages {
			break
		}

		fetchOpts.Page = resp.NextPage
	}

	return releases, nil
}

// FetchProjectReleasesTags retrieves all release tags for a project and returns them as a map.
func (g *GitlabInstance) FetchProjectReleasesTags(project *gitlab.Project) (map[string]struct{}, error) {
	// Fetch existing releases from the destination project
	releases, err := g.FetchProjectReleases(project)
	if err != nil {
		return nil, err
	}

	// Create a map of existing release tags for quick lookup
	releasesTags := make(map[string]struct{})

	for _, release := range releases {
		if release != nil {
			releasesTags[release.TagName] = struct{}{}
		}
	}

	return releasesTags, nil
}

// ================
//	      POST
// ================

// MirrorRelease creates a release in the destination project.
func (g *GitlabInstance) MirrorRelease(project *gitlab.Project, release *gitlab.Release) error {
	zap.L().Debug("Creating release in destination project", zap.String("release", release.TagName), zap.String(RoleDestination, project.HTTPURLToRepo))

	// Create the release in the destination project
	_, _, err := g.Gitlab.Releases.CreateRelease(project.ID, &gitlab.CreateReleaseOptions{
		Name:        &release.Name,
		TagName:     &release.TagName,
		Description: &release.Description,
		ReleasedAt:  release.ReleasedAt,
	})
	if err != nil {
		return fmt.Errorf("failed to create release %s in project %s: %w", release.TagName, project.PathWithNamespace, err)
	}

	return nil
}

// ================
//	     DELETE
// ================

// DeleteStaleRelease deletes a destination release that does not exist on the source
// project, along with the files its links point to (see CleanUpReleaseFiles).
// The git tag of the release is left to the repository mirroring.
func (g *GitlabInstance) DeleteStaleRelease(project *gitlab.Project, release *gitlab.Release, referenced map[genericPackageFile]struct{}) error {
	zap.L().Info("Deleting release that no longer exists on the source", zap.String("release", release.TagName), zap.String(RoleDestination, project.HTTPURLToRepo))

	_, response, err := g.Gitlab.Releases.DeleteRelease(project.ID, release.TagName)
	if err != nil && !g.releaseAlreadyDeleted(project, release.TagName, response) {
		return fmt.Errorf("failed to delete release %s in project %s: %w", release.TagName, project.PathWithNamespace, err)
	}

	return g.CleanUpReleaseFiles(project, release, referenced)
}

// releaseAlreadyDeleted tells whether a refused deletion is due to the release being
// gone. GitLab deletes the release of a deleted tag in the background, so a release
// whose tag the git mirroring just removed can still be listed and then vanish before
// it is deleted here. GitLab answers 403 for a release that does not exist, the same
// as for a lack of permission: only listing the releases again tells them apart.
func (g *GitlabInstance) releaseAlreadyDeleted(project *gitlab.Project, tagName string, response *gitlab.Response) bool {
	if response == nil || (response.StatusCode != http.StatusForbidden && response.StatusCode != http.StatusNotFound) {
		return false
	}

	tags, err := g.FetchProjectReleasesTags(project)
	if err != nil {
		zap.L().Debug("Could not check whether the release still exists", zap.String("release", tagName), zap.Error(err))

		return false
	}

	_, exists := tags[tagName]

	return !exists
}

// CleanUpReleaseFiles deletes the files the links of a deleted destination release
// point to (see deleteLinkedPackageFile).
func (g *GitlabInstance) CleanUpReleaseFiles(project *gitlab.Project, release *gitlab.Release, referenced map[genericPackageFile]struct{}) error {
	var errs []error

	for _, link := range release.Assets.Links {
		if link == nil {
			continue
		}

		err := g.deleteLinkedPackageFile(project, link, referenced)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to clean up the files of deleted release %s in project %s: %w", release.TagName, project.PathWithNamespace, err))
		}
	}

	return errors.Join(errs...)
}

// ================
//    CONTROLLER
// ================

// ReleasesMirroringOptions tunes MirrorReleases.
type ReleasesMirroringOptions struct {
	// DestinationReleasesBeforeGit are the destination releases as they were before
	// the git mirroring ran, when MirrorAssets is set. The git mirroring deletes the
	// destination tags the source does not have, and GitLab deletes the release of a
	// deleted tag with it: without this snapshot, the files of such a release would
	// be left behind in the destination package registry. Nil when unknown.
	DestinationReleasesBeforeGit []*gitlab.Release
	// MirrorAssets keeps the destination releases in sync (see mirrorReleasesWithAssets).
	MirrorAssets bool
}

// MirrorReleases mirrors releases from the source project to the destination project.
// It fetches existing releases from the destination project and creates new releases for those that do not exist.
// When options.MirrorAssets is set, the destination releases are kept in sync instead
// (see mirrorReleasesWithAssets): the release asset links (and the files they point
// to on the source instance) are mirrored too, and the releases, links and files that
// no longer exist on the source are deleted.
// The function handles the API calls concurrently using goroutines.
func (g *GitlabInstance) MirrorReleases(sourceGitlab *GitlabInstance, sourceProject, destinationProject *gitlab.Project, options ReleasesMirroringOptions) {
	if options.MirrorAssets {
		g.mirrorReleasesWithAssets(sourceGitlab, sourceProject, destinationProject, options.DestinationReleasesBeforeGit)

		return
	}

	mirrorProjectEntities(
		"release",
		sourceProject,
		destinationProject,
		g.FetchProjectReleasesTags,
		sourceGitlab.FetchProjectReleases,
		func(release *gitlab.Release) string {
			return release.TagName
		},
		func(project *gitlab.Project, release *gitlab.Release) error {
			return g.MirrorRelease(project, release)
		},
	)
}

// releasesByTag indexes releases by tag name, skipping nil ones.
func releasesByTag(releases []*gitlab.Release) map[string]*gitlab.Release {
	byTag := make(map[string]*gitlab.Release, len(releases))

	for _, release := range releases {
		if release != nil {
			byTag[release.TagName] = release
		}
	}

	return byTag
}
