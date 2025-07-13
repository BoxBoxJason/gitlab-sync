package mirroring

import (
	"fmt"
	"gitlab-sync/pkg/helpers"
	"sync"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

// ===========================================================================
//                   	RELEASES MIRRORING FUNCTIONS                        //
// ===========================================================================

// ================
//	      GET
// ================

// FetchProjectReleases retrieves all releases for a project and returns them
func (g *GitlabInstance) FetchProjectReleases(project *gitlab.Project) ([]*gitlab.Release, error) {
	zap.L().Debug("Fetching releases for project", zap.String("project", project.PathWithNamespace))
	fetchOpts := &gitlab.ListReleasesOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
			Page:    1,
		},
	}

	var releases = make([]*gitlab.Release, 0)

	for {
		fetchedReleases, resp, err := g.Gitlab.Releases.ListReleases(project.ID, fetchOpts)
		if err != nil {
			return nil, err
		}
		releases = append(releases, fetchedReleases...)

		if resp.CurrentPage >= resp.TotalPages {
			break
		}
		fetchOpts.Page = resp.NextPage
	}

	return releases, nil
}

// FetchProjectReleasesTags retrieves all release tags for a project and returns them as a map
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

// MirrorRelease creates a release in the destination project
func (g *GitlabInstance) MirrorRelease(project *gitlab.Project, release *gitlab.Release) error {
	zap.L().Debug("Creating release in destination project", zap.String("release", release.TagName), zap.String(ROLE_DESTINATION, project.HTTPURLToRepo))

	// Create the release in the destination project
	_, _, err := g.Gitlab.Releases.CreateRelease(project.ID, &gitlab.CreateReleaseOptions{
		Name:        &release.Name,
		TagName:     &release.TagName,
		Description: &release.Description,
		ReleasedAt:  release.ReleasedAt,
	})
	return err
}

// ================
//    CONTROLLER
// ================

// MirrorReleases mirrors releases from the source project to the destination project.
// It fetches existing releases from the destination project and creates new releases for those that do not exist.
// The function handles the API calls concurrently using goroutines
func (destinationGitlab *GitlabInstance) MirrorReleases(sourceGitlab *GitlabInstance, sourceProject *gitlab.Project, destinationProject *gitlab.Project) []error {
	zap.L().Info("Starting releases mirroring", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))
	// Fetch existing releases from the destination project
	existingReleasesTags, err := destinationGitlab.FetchProjectReleasesTags(destinationProject)
	if err != nil {
		return []error{fmt.Errorf("failed to fetch existing releases for destination project %s: %s", destinationProject.HTTPURLToRepo, err)}
	}

	// Fetch releases from the source project
	sourceReleases, err := sourceGitlab.FetchProjectReleases(sourceProject)
	if err != nil {
		return []error{fmt.Errorf("failed to fetch releases for source project %s: %s", sourceProject.HTTPURLToRepo, err)}
	}

	// Create a wait group and an error channel for handling API calls concurrently
	var wg sync.WaitGroup
	errorChan := make(chan error, len(sourceReleases))

	// Iterate over each source release
	for _, release := range sourceReleases {
		// Check if the release already exists in the destination project
		if _, exists := existingReleasesTags[release.TagName]; exists {
			zap.L().Debug("Release already exists", zap.String("release", release.TagName), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))
			continue
		}

		// Increment the wait group counter
		wg.Add(1)

		// Define the API call logic for creating a release
		go func(releaseToMirror *gitlab.Release) {
			defer wg.Done()
			err := destinationGitlab.MirrorRelease(destinationProject, releaseToMirror)
			if err != nil {
				errorChan <- fmt.Errorf("failed to create release %s in project %s: %s", releaseToMirror.TagName, destinationProject.HTTPURLToRepo, err)
			}
		}(release)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(errorChan)

	zap.L().Info("Releases mirroring completed", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))
	return helpers.MergeErrors(errorChan)
}
