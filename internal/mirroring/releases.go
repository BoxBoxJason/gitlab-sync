package mirroring

import (
	"fmt"
	"os"

	"github.com/boxboxjason/gitlab-sync/internal/utils"

	gitlab "gitlab.com/gitlab-org/api/client-go"
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
// It fetches the releases from the source project and prints them.
func (destinationGitlabInstance *GitlabInstance) DryRunReleases(sourceGitlabInstance *GitlabInstance, sourceProject *gitlab.Project, copyOptions *utils.MirroringOptions) error {
	// Fetch releases from the source project
	sourceReleases, err := sourceGitlabInstance.FetchProjectReleasesTags(sourceProject)
	if err != nil {
		return fmt.Errorf("failed to fetch releases for source project %s: %w", sourceProject.HTTPURLToRepo, err)
	}
	// Print the releases that will be created in the destination project
	for release := range sourceReleases {
		_, err = fmt.Fprintf(os.Stdout, "    - Release %s will be created in %s (if it does not already exist)\n", release, destinationGitlabInstance.Gitlab.BaseURL().String()+copyOptions.DestinationPath)
		if err != nil {
			return fmt.Errorf("failed to print dry-run release output: %w", err)
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
//    CONTROLLER
// ================

// MirrorReleases mirrors releases from the source project to the destination project.
// It fetches existing releases from the destination project and creates new releases for those that do not exist.
// The function handles the API calls concurrently using goroutines.
func (destinationGitlab *GitlabInstance) MirrorReleases(sourceGitlab *GitlabInstance, sourceProject, destinationProject *gitlab.Project) []error {
	return mirrorProjectEntities(
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
