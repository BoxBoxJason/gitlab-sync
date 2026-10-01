package mirroring

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"

	"github.com/boxboxjason/gitlab-sync/internal/utils"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.uber.org/zap"
)

const releasesPerPage = 100

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

// DryRunReleases prints the releases that would be created in dry run mode.
// It fetches the releases from the source project and prints them, along with
// their asset links, and the destination releases and links that would be deleted,
// when mirror_release_assets is enabled.
func (destinationGitlabInstance *GitlabInstance) DryRunReleases(sourceGitlabInstance *GitlabInstance, sourceProject *gitlab.Project, copyOptions *utils.MirroringOptions) error {
	// Fetch releases from the source project
	sourceReleases, err := sourceGitlabInstance.FetchProjectReleases(sourceProject)
	if err != nil {
		return fmt.Errorf("failed to fetch releases for source project %s: %w", sourceProject.HTTPURLToRepo, err)
	}

	var destinationReleases map[string]*gitlab.Release

	mirrorAssets := helpers.Deref(copyOptions.MirrorReleaseAssets, false)
	if mirrorAssets {
		destinationReleases, err = destinationGitlabInstance.dryRunDestinationReleases(copyOptions.DestinationPath)
		if err != nil {
			return err
		}
	}

	sourceRoot := instanceRootURL(sourceGitlabInstance.Gitlab)
	destinationURL := instanceRootURL(destinationGitlabInstance.Gitlab).String() + copyOptions.DestinationPath

	// Print the releases that will be created in the destination project
	for _, release := range sourceReleases {
		if release == nil {
			continue
		}

		_, err = fmt.Fprintf(os.Stdout, "    - Release %s will be created in %s (if it does not already exist)\n", release.TagName, destinationURL)
		if err != nil {
			return fmt.Errorf("failed to print dry-run release output: %w", err)
		}

		if !mirrorAssets {
			continue
		}

		err = dryRunReleaseAssets(sourceRoot, release, destinationReleases[release.TagName])
		if err != nil {
			return err
		}

		delete(destinationReleases, release.TagName)
	}

	// What is left are the destination releases the source project does not have.
	return dryRunStaleReleases(destinationURL, destinationReleases)
}

// dryRunStaleReleases prints the destination releases that would be deleted.
func dryRunStaleReleases(destinationURL string, staleReleases map[string]*gitlab.Release) error {
	for tagName := range staleReleases {
		_, err := fmt.Fprintf(os.Stdout, "    - Release %s will be deleted from %s, it is no longer on the source (with the copies of its assets in the destination package registry, unless another release still uses them)\n", tagName, destinationURL)
		if err != nil {
			return fmt.Errorf("failed to print dry-run release output: %w", err)
		}
	}

	return nil
}

// dryRunDestinationReleases returns the releases of the destination project at
// destinationPath by tag name. It is empty when the project does not exist yet.
func (destinationGitlabInstance *GitlabInstance) dryRunDestinationReleases(destinationPath string) (map[string]*gitlab.Release, error) {
	destinationProject := destinationGitlabInstance.GetProject(destinationPath)
	if destinationProject == nil {
		return make(map[string]*gitlab.Release), nil
	}

	releases, err := destinationGitlabInstance.FetchProjectReleases(destinationProject)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch releases for destination project %s: %w", destinationProject.HTTPURLToRepo, err)
	}

	return releasesByTag(releases), nil
}

// dryRunReleaseAssets prints what mirroring the assets of release does, including
// the links of destinationRelease (nil when it does not exist) that would be deleted.
func dryRunReleaseAssets(sourceRoot *url.URL, release, destinationRelease *gitlab.Release) error {
	sourceLinkNames := make(map[string]struct{}, len(release.Assets.Links))

	for _, plan := range planReleaseAssets(sourceRoot, release) {
		sourceLinkNames[plan.Link.Name] = struct{}{}

		_, err := fmt.Fprintf(os.Stdout, "      - Asset %s: %s\n", plan.Link.Name, plan)
		if err != nil {
			return fmt.Errorf("failed to print dry-run release asset output: %w", err)
		}
	}

	if destinationRelease == nil {
		return nil
	}

	for _, link := range destinationRelease.Assets.Links {
		if link == nil {
			continue
		}

		if _, exists := sourceLinkNames[link.Name]; exists {
			continue
		}

		_, err := fmt.Fprintf(os.Stdout, "      - Asset %s: deleted, it is no longer in the source release (with its copy in the destination package registry, unless another release still uses it)\n", link.Name)
		if err != nil {
			return fmt.Errorf("failed to print dry-run release asset output: %w", err)
		}
	}

	return nil
}

// ================
//	      POST
// ================

// MirrorRelease creates a release in the destination project.
func (g *GitlabInstance) MirrorRelease(project *gitlab.Project, release *gitlab.Release) error {
	zap.L().Debug("Creating release in destination project", zap.String("release", release.TagName), zap.String(ROLE_DESTINATION, project.HTTPURLToRepo))

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
	zap.L().Info("Deleting release that no longer exists on the source", zap.String("release", release.TagName), zap.String(ROLE_DESTINATION, project.HTTPURLToRepo))

	_, _, err := g.Gitlab.Releases.DeleteRelease(project.ID, release.TagName)
	if err != nil {
		return fmt.Errorf("failed to delete release %s in project %s: %w", release.TagName, project.PathWithNamespace, err)
	}

	return g.CleanUpReleaseFiles(project, release, referenced)
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
func (destinationGitlab *GitlabInstance) MirrorReleases(sourceGitlab *GitlabInstance, sourceProject, destinationProject *gitlab.Project, options ReleasesMirroringOptions) {
	if options.MirrorAssets {
		destinationGitlab.mirrorReleasesWithAssets(sourceGitlab, sourceProject, destinationProject, options.DestinationReleasesBeforeGit)

		return
	}

	mirrorProjectEntities(
		"release",
		sourceProject,
		destinationProject,
		destinationGitlab.FetchProjectReleasesTags,
		sourceGitlab.FetchProjectReleases,
		func(release *gitlab.Release) string {
			return release.TagName
		},
		func(project *gitlab.Project, release *gitlab.Release) error {
			return destinationGitlab.MirrorRelease(project, release)
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

// mirrorReleasesWithAssets keeps the destination releases in sync with the source
// releases: missing releases are created, the asset links of every source release
// are mirrored (whether the release was just created or already existed), and the
// destination releases the source project does not have are deleted.
// releasesBeforeGit are the destination releases before the git mirroring ran (see
// ReleasesMirroringOptions): the ones it made disappear are cleaned up like the others.
// Every failure is logged as it happens via helpers.Report.
func (destinationGitlab *GitlabInstance) mirrorReleasesWithAssets(sourceGitlab *GitlabInstance, sourceProject, destinationProject *gitlab.Project, releasesBeforeGit []*gitlab.Release) {
	zap.L().Info("Starting release mirroring (with assets)", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))

	destinationReleases, err := destinationGitlab.FetchProjectReleases(destinationProject)
	if err != nil {
		helpers.Report(fmt.Errorf("failed to fetch existing release for destination project %s: %w", destinationProject.HTTPURLToRepo, err))

		return
	}

	sourceReleases, err := sourceGitlab.FetchProjectReleases(sourceProject)
	if err != nil {
		helpers.Report(fmt.Errorf("failed to fetch release for source project %s: %w", sourceProject.HTTPURLToRepo, err))

		return
	}

	existingReleases := releasesByTag(destinationReleases)
	sourceReleasesByTag := releasesByTag(sourceReleases)

	// Computed over every release, so that deleting a stale link of one release never
	// deletes a package file another release still links to.
	referenced := referencedPackageFiles(instanceRootURL(sourceGitlab.Gitlab), sourceReleases)

	var waitGroup sync.WaitGroup

	for tagName, sourceRelease := range sourceReleasesByTag {
		waitGroup.Go(func() {
			destinationGitlab.mirrorReleaseWithAssets(sourceGitlab, destinationProject, sourceRelease, existingReleases[tagName], referenced)
		})
	}

	// Releases the source project no longer has (matched by tag name).
	for tagName, destinationRelease := range existingReleases {
		if _, exists := sourceReleasesByTag[tagName]; exists {
			continue
		}

		waitGroup.Go(func() {
			helpers.Report(destinationGitlab.DeleteStaleRelease(destinationProject, destinationRelease, referenced))
		})
	}

	// Releases the git mirroring already removed along with their tag: only their files are left.
	for _, vanishedRelease := range vanishedReleases(sourceReleasesByTag, existingReleases, releasesBeforeGit) {
		zap.L().Info("Cleaning up the files of a release deleted along with its tag", zap.String("release", vanishedRelease.TagName), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))

		waitGroup.Go(func() {
			helpers.Report(destinationGitlab.CleanUpReleaseFiles(destinationProject, vanishedRelease, referenced))
		})
	}

	waitGroup.Wait()

	zap.L().Info("release mirroring (with assets) completed", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))
}

// vanishedReleases returns the releases of releasesBeforeGit (see
// ReleasesMirroringOptions) that the destination no longer has (existingReleases),
// and that the source does not have either: GitLab deleted them along with the tag
// the git mirroring removed. They cannot be deleted again - GitLab answers 403 for
// a release that does not exist - so only their files are left to clean up.
func vanishedReleases(sourceReleases, existingReleases map[string]*gitlab.Release, releasesBeforeGit []*gitlab.Release) []*gitlab.Release {
	var vanished []*gitlab.Release

	for tagName, release := range releasesByTag(releasesBeforeGit) {
		_, inSource := sourceReleases[tagName]
		_, stillThere := existingReleases[tagName]

		if !inSource && !stillThere {
			vanished = append(vanished, release)
		}
	}

	return vanished
}

// mirrorReleaseWithAssets creates sourceRelease in the destination project when
// existingRelease (its destination counterpart) is nil, then mirrors its asset links.
func (destinationGitlab *GitlabInstance) mirrorReleaseWithAssets(sourceGitlab *GitlabInstance, destinationProject *gitlab.Project, sourceRelease, existingRelease *gitlab.Release, referenced map[genericPackageFile]struct{}) {
	var existingLinks []*gitlab.ReleaseLink

	if existingRelease != nil {
		zap.L().Debug("release already exists", zap.String("release", sourceRelease.TagName), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))

		existingLinks = existingRelease.Assets.Links
	} else {
		err := destinationGitlab.MirrorRelease(destinationProject, sourceRelease)
		if err != nil {
			helpers.Report(fmt.Errorf("failed to create release %s in project %s: %w", sourceRelease.TagName, destinationProject.HTTPURLToRepo, err))

			return
		}
	}

	destinationGitlab.MirrorReleaseAssets(sourceGitlab, destinationProject, sourceRelease, existingLinks, referenced)
}
