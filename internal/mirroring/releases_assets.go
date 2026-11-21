package mirroring

import (
	"fmt"
	"sync"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.uber.org/zap"

	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

// mirrorReleasesWithAssets keeps the destination releases in sync with the source
// releases: missing releases are created, the asset links of every source release
// are mirrored (whether the release was just created or already existed), and the
// destination releases the source project does not have are deleted.
// releasesBeforeGit are the destination releases before the git mirroring ran (see
// ReleasesMirroringOptions): the ones it made disappear are cleaned up like the others.
// Every failure is logged as it happens via helpers.Report.
func (g *GitlabInstance) mirrorReleasesWithAssets(sourceGitlab *GitlabInstance, sourceProject, destinationProject *gitlab.Project, releasesBeforeGit []*gitlab.Release) {
	zap.L().Info("Starting release mirroring (with assets)", zap.String(RoleSource, sourceProject.HTTPURLToRepo), zap.String(RoleDestination, destinationProject.HTTPURLToRepo))

	destinationReleases, err := g.FetchProjectReleases(destinationProject)
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
			g.mirrorReleaseWithAssets(sourceGitlab, destinationProject, sourceRelease, existingReleases[tagName], referenced)
		})
	}

	// Releases the source project no longer has (matched by tag name).
	for tagName, destinationRelease := range existingReleases {
		if _, exists := sourceReleasesByTag[tagName]; exists {
			continue
		}

		waitGroup.Go(func() {
			helpers.Report(g.DeleteStaleRelease(destinationProject, destinationRelease, referenced))
		})
	}

	// Releases the git mirroring already removed along with their tag: only their files are left.
	for _, vanishedRelease := range vanishedReleases(sourceReleasesByTag, existingReleases, releasesBeforeGit) {
		zap.L().Info("Cleaning up the files of a release deleted along with its tag", zap.String("release", vanishedRelease.TagName), zap.String(RoleDestination, destinationProject.HTTPURLToRepo))

		waitGroup.Go(func() {
			helpers.Report(g.CleanUpReleaseFiles(destinationProject, vanishedRelease, referenced))
		})
	}

	waitGroup.Wait()

	zap.L().Info("release mirroring (with assets) completed", zap.String(RoleSource, sourceProject.HTTPURLToRepo), zap.String(RoleDestination, destinationProject.HTTPURLToRepo))
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
func (g *GitlabInstance) mirrorReleaseWithAssets(sourceGitlab *GitlabInstance, destinationProject *gitlab.Project, sourceRelease, existingRelease *gitlab.Release, referenced map[genericPackageFile]struct{}) {
	var existingLinks []*gitlab.ReleaseLink

	if existingRelease != nil {
		zap.L().Debug("release already exists", zap.String("release", sourceRelease.TagName), zap.String(RoleDestination, destinationProject.HTTPURLToRepo))

		existingLinks = existingRelease.Assets.Links
	} else {
		err := g.MirrorRelease(destinationProject, sourceRelease)
		if err != nil {
			helpers.Report(fmt.Errorf("failed to create release %s in project %s: %w", sourceRelease.TagName, destinationProject.HTTPURLToRepo, err))

			return
		}
	}

	g.MirrorReleaseAssets(sourceGitlab, destinationProject, sourceRelease, existingLinks, referenced)
}
