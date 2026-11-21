package mirroring

import (
	"fmt"
	"net/url"
	"os"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

// DryRunReleases prints the releases that would be created in dry run mode.
// It fetches the releases from the source project and prints them, along with
// their asset links, and the destination releases and links that would be deleted,
// when mirror_release_assets is enabled.
func (g *GitlabInstance) DryRunReleases(sourceGitlabInstance *GitlabInstance, sourceProject *gitlab.Project, copyOptions *mirrorcfg.MirroringOptions) error {
	// Fetch releases from the source project
	sourceReleases, err := sourceGitlabInstance.FetchProjectReleases(sourceProject)
	if err != nil {
		return fmt.Errorf("failed to fetch releases for source project %s: %w", sourceProject.HTTPURLToRepo, err)
	}

	var destinationReleases map[string]*gitlab.Release

	mirrorAssets := helpers.Deref(copyOptions.MirrorReleaseAssets, false)
	if mirrorAssets {
		destinationReleases, err = g.dryRunDestinationReleases(copyOptions.DestinationPath)
		if err != nil {
			return err
		}
	}

	sourceRoot := instanceRootURL(sourceGitlabInstance.Gitlab)
	destinationURL := instanceRootURL(g.Gitlab).String() + copyOptions.DestinationPath

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
func (g *GitlabInstance) dryRunDestinationReleases(destinationPath string) (map[string]*gitlab.Release, error) {
	destinationProject := g.GetProject(destinationPath)
	if destinationProject == nil {
		return make(map[string]*gitlab.Release), nil
	}

	releases, err := g.FetchProjectReleases(destinationProject)
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
