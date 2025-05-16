package mirroring

import (
	"fmt"
	"path/filepath"
	"sync"

	"gitlab-sync/internal/utils"

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
	err = destinationGitlabInstance.CheckVersion()
	if err != nil {
		return []error{err}
	}

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
		DryRun(sourceGitlabInstance, gitlabMirrorArgs)
		return nil
	}

	// Create groups and projects in the destination GitLab instance (Groups must be created before projects)
	errCh <- destinationGitlabInstance.createGroups(sourceGitlabInstance, gitlabMirrorArgs.MirrorMapping)
	errCh <- destinationGitlabInstance.createProjects(sourceGitlabInstance, gitlabMirrorArgs.MirrorMapping)

	close(errCh)
	return utils.MergeErrors(errCh)
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
func DryRun(sourceGitlabInstance *GitlabInstance, gitlabMirrorArgs *utils.ParserArgs) {
	zap.L().Info("Dry run mode enabled, will not create groups or projects")
	zap.L().Info("Groups that will be created (or updated if they already exist):")
	for sourceGroupPath, copyOptions := range gitlabMirrorArgs.MirrorMapping.Groups {
		if sourceGroup, ok := sourceGitlabInstance.Groups[sourceGroupPath]; ok {
			fmt.Printf("  - %s (source gitlab) -> %s (destination gitlab)\n", sourceGroup.WebURL, copyOptions.DestinationPath)
		}
	}
	zap.L().Info("Projects that will be created (or updated if they already exist):")
	for sourceProjectPath, copyOptions := range gitlabMirrorArgs.MirrorMapping.Projects {
		if sourceProject, ok := sourceGitlabInstance.Projects[sourceProjectPath]; ok {
			fmt.Printf("  - %s (source gitlab) -> %s (destination gitlab)\n", sourceProject.WebURL, copyOptions.DestinationPath)
		}
	}
}
