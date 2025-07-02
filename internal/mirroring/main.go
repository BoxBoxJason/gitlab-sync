package mirroring

import (
	"fmt"
	"path/filepath"
	"sync"

	"gitlab-sync/internal/utils"
	"gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

// MirrorGitlabs is the main function that handles the mirroring process between two GitLab instances.
// It takes a ParserArgs struct as an argument, which contains the necessary parameters for the mirroring process.
// It creates two GitLab instances (source and destination) and fetches the groups and projects from both instances.
// It then processes the filters for groups and projects, and finally creates the groups and projects in the destination GitLab instance.
// If the dry run flag is set, it will only print the groups and projects that would be created or updated.
func MirrorGitlabs(gitlabMirrorArgs *utils.ParserArgs) []error {
	zap.L().Info("Starting GitLab mirroring process", zap.String(ROLE_SOURCE, gitlabMirrorArgs.SourceGitlabURL), zap.String(ROLE_DESTINATION, gitlabMirrorArgs.DestinationGitlabURL))
	sourceGitlabSize := INSTANCE_SIZE_SMALL
	if gitlabMirrorArgs.SourceGitlabIsBig {
		sourceGitlabSize = INSTANCE_SIZE_BIG
	}
	sourceGitlabInstance, err := newGitlabInstance(&GitlabInstanceOpts{
		GitlabURL:    gitlabMirrorArgs.SourceGitlabURL,
		GitlabToken:  gitlabMirrorArgs.SourceGitlabToken,
		Role:         ROLE_SOURCE,
		MaxRetries:   gitlabMirrorArgs.Retry,
		InstanceSize: sourceGitlabSize,
	})
	if err != nil {
		return []error{err}
	}

	destinationGitlabSize := INSTANCE_SIZE_SMALL
	if gitlabMirrorArgs.DestinationGitlabIsBig {
		destinationGitlabSize = INSTANCE_SIZE_BIG
	}
	destinationGitlabInstance, err := newGitlabInstance(&GitlabInstanceOpts{
		GitlabURL:    gitlabMirrorArgs.DestinationGitlabURL,
		GitlabToken:  gitlabMirrorArgs.DestinationGitlabToken,
		Role:         ROLE_DESTINATION,
		MaxRetries:   gitlabMirrorArgs.Retry,
		InstanceSize: destinationGitlabSize,
	})
	if err != nil {
		return []error{err}
	}
	pullMirrorAvailable, err := destinationGitlabInstance.IsPullMirrorAvailable()
	if err != nil {
		return []error{err}
	} else if pullMirrorAvailable {
		zap.L().Info("GitLab instance is compatible with the pull mirroring process", zap.String(ROLE, destinationGitlabInstance.Role), zap.String(INSTANCE_SIZE, destinationGitlabInstance.InstanceSize))
	} else {
		zap.L().Warn("Destination GitLab instance is not compatible with the pull mirroring process (requires a >= 17.6 ; >= Premium destination GitLab instance)", zap.String(ROLE, destinationGitlabInstance.Role), zap.String(INSTANCE_SIZE, destinationGitlabInstance.InstanceSize))
		zap.L().Warn("Will use local pull / push mirroring instead (takes a lot longer)", zap.String(ROLE, destinationGitlabInstance.Role), zap.String(INSTANCE_SIZE, destinationGitlabInstance.InstanceSize))
	}
	destinationGitlabInstance.PullMirrorAvailable = pullMirrorAvailable

	sourceProjectFilters, sourceGroupFilters, destinationProjectFilters, destinationGroupFilters := processFilters(gitlabMirrorArgs.MirrorMapping)

	wg := sync.WaitGroup{}
	errCh := make(chan []error, 4)
	wg.Add(2)

	go func() {
		defer wg.Done()
		errCh <- sourceGitlabInstance.fetchAll(sourceProjectFilters, sourceGroupFilters, gitlabMirrorArgs.MirrorMapping)
	}()
	go func() {
		defer wg.Done()
		errCh <- destinationGitlabInstance.fetchAll(destinationProjectFilters, destinationGroupFilters, gitlabMirrorArgs.MirrorMapping)
	}()

	wg.Wait()

	// In case of dry run, simply print the groups and projects that would be created or updated
	if gitlabMirrorArgs.DryRun {
		destinationGitlabInstance.DryRun(sourceGitlabInstance, gitlabMirrorArgs.MirrorMapping)
		return nil
	}

	// Create groups and projects in the destination GitLab instance (Groups must be created before projects)
	errCh <- destinationGitlabInstance.createGroups(sourceGitlabInstance, gitlabMirrorArgs.MirrorMapping)
	errCh <- destinationGitlabInstance.createProjects(sourceGitlabInstance, gitlabMirrorArgs.MirrorMapping)

	close(errCh)
	return helpers.MergeErrors(errCh)
}

// processFilters processes the filters for groups and projects.
// It returns four maps: sourceProjectFilters, sourceGroupFilters, destinationProjectFilters, and destinationGroupFilters.
func processFilters(filters *utils.MirrorMapping) (map[string]struct{}, map[string]struct{}, map[string]struct{}, map[string]struct{}) {
	zap.L().Info("Checking mirror mapping filters")
	sourceProjectFilters := make(map[string]struct{})
	sourceGroupFilters := make(map[string]struct{})
	destinationProjectFilters := make(map[string]struct{})
	destinationGroupFilters := make(map[string]struct{})

	// Initialize concurrency control
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)

	// Process group filters concurrently.
	go func() {
		defer wg.Done()
		for group, copyOptions := range filters.Groups {
			sourceGroupFilters[group] = struct{}{}
			mu.Lock()
			destinationGroupFilters[copyOptions.DestinationPath] = struct{}{}
			mu.Unlock()
		}
	}()

	// Process project filters concurrently.
	go func() {
		defer wg.Done()
		for project, copyOptions := range filters.Projects {
			sourceProjectFilters[project] = struct{}{}
			destinationProjectFilters[copyOptions.DestinationPath] = struct{}{}
			destinationGroupPath := filepath.Dir(copyOptions.DestinationPath)
			if destinationGroupPath != "" && destinationGroupPath != "." && destinationGroupPath != "/" {
				mu.Lock()
				destinationGroupFilters[destinationGroupPath] = struct{}{}
				mu.Unlock()
			}
		}
	}()

	wg.Wait()
	return sourceProjectFilters, sourceGroupFilters, destinationProjectFilters, destinationGroupFilters
}

// DryRun prints the groups and projects that would be created or updated in dry run mode.
func (destinationGitlabInstance *GitlabInstance) DryRun(sourceGitlabInstance *GitlabInstance, mirrorMapping *utils.MirrorMapping) []error {
	zap.L().Info("Dry run mode enabled, will not create groups or projects")
	zap.L().Info("Groups that will be created (or updated if they already exist):")
	for sourceGroupPath, copyOptions := range mirrorMapping.Groups {
		if sourceGroup, ok := sourceGitlabInstance.Groups[sourceGroupPath]; ok {
			fmt.Printf("  - %s (source gitlab) -> %s (destination gitlab)\n", sourceGroup.WebURL, copyOptions.DestinationPath)
		}
	}
	zap.L().Info("Projects that will be created (or updated if they already exist):")
	for sourceProjectPath, copyOptions := range mirrorMapping.Projects {
		if sourceProject, ok := sourceGitlabInstance.Projects[sourceProjectPath]; ok {
			fmt.Printf("  - %s (source gitlab) -> %s (destination gitlab)\n", sourceProject.WebURL, copyOptions.DestinationPath)

			if copyOptions.MirrorReleases {
				if err := destinationGitlabInstance.DryRunReleases(sourceGitlabInstance, sourceProject, copyOptions); err != nil {
					zap.L().Error("Failed to dry run releases", zap.Error(err))
					return []error{err}
				}
			}
		}

	}
	zap.L().Info("Dry run completed")
	return nil
}

// DryRunReleases prints the releases that would be created in dry run mode.
// It fetches the releases from the source project and prints them.
// It does not create any releases in the destination project.
func (destinationGitlabInstance *GitlabInstance) DryRunReleases(sourceGitlabInstance *GitlabInstance, sourceProject *gitlab.Project, copyOptions *utils.MirroringOptions) error {
	// Fetch releases from the source project
	sourceReleases, _, err := sourceGitlabInstance.Gitlab.Releases.ListReleases(sourceProject.ID, &gitlab.ListReleasesOptions{})
	if err != nil {
		return fmt.Errorf("failed to fetch releases for source project %s: %s", sourceProject.HTTPURLToRepo, err)
	}
	// Print the releases that will be created in the destination project
	for _, release := range sourceReleases {
		fmt.Printf("    - Release %s will be created in %s (if it does not already exist)\n", release.Name, destinationGitlabInstance.Gitlab.BaseURL().String()+copyOptions.DestinationPath)
	}
	return nil
}

// IsPullMirrorAvailable checks the destination GitLab instance for version and license compatibility.
func (g *GitlabInstance) IsPullMirrorAvailable() (bool, error) {
	zap.L().Info("Checking destination GitLab instance")
	thresholdOk, err := g.IsVersionGreaterThanThreshold()
	if err != nil {
		return false, fmt.Errorf("destination GitLab instance version check failed: %w", err)
	}

	isPremium, err := g.IsLicensePremium()
	if err != nil {
		return false, fmt.Errorf("failed to check if destination GitLab instance is premium: %w", err)
	}

	return thresholdOk && isPremium, nil
}
