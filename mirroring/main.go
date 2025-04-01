package mirroring

import (
	"fmt"
	"log"
	"path/filepath"
	"sync"

	"gitlab-sync/utils"
)

func MirrorGitlabs(gitlabMirrorArgs *utils.ParserArgs) error {
	sourceGitlabInstance, err := newGitlabInstance(gitlabMirrorArgs.SourceGitlabURL, gitlabMirrorArgs.SourceGitlabToken)
	if err != nil {
		return err
	}

	destinationGitlabInstance, err := newGitlabInstance(gitlabMirrorArgs.DestinationGitlabURL, gitlabMirrorArgs.DestinationGitlabToken)
	if err != nil {
		return err
	}

	sourceProjectFilters, sourceGroupFilters, destinationProjectFilters, destinationGroupFilters := processFilters(gitlabMirrorArgs.MirrorMapping)

	wg := sync.WaitGroup{}
	errCh := make(chan error, 4)
	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := fetchAll(sourceGitlabInstance, sourceProjectFilters, sourceGroupFilters, gitlabMirrorArgs.MirrorMapping, true); err != nil {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		if err := fetchAll(destinationGitlabInstance, destinationProjectFilters, destinationGroupFilters, gitlabMirrorArgs.MirrorMapping, false); err != nil {
			errCh <- err
		}
	}()

	wg.Wait()

	// In case of dry run, simply print the groups and projects that would be created or updated
	if gitlabMirrorArgs.DryRun {
		log.Println("Dry run mode enabled, will not create groups or projects")
		fmt.Println("Groups that will be created (or updated if they already exist):")
		for sourceGroupPath, copyOptions := range gitlabMirrorArgs.MirrorMapping.Groups {
			if sourceGitlabInstance.Groups[sourceGroupPath] != nil {
				fmt.Printf("  - %s (source gitlab) -> %s (destination gitlab)\n", sourceGroupPath, copyOptions.DestinationPath)
			}
		}
		fmt.Println("Projects that will be created (or updated if they already exist):")
		for sourceProjectPath, copyOptions := range gitlabMirrorArgs.MirrorMapping.Projects {
			if sourceGitlabInstance.Projects[sourceProjectPath] != nil {
				fmt.Printf("  - %s (source gitlab) -> %s (destination gitlab)\n", sourceProjectPath, copyOptions.DestinationPath)
			}
		}
		return nil
	}

	// Create groups and projects in the destination GitLab instance
	err = createGroups(sourceGitlabInstance, destinationGitlabInstance, gitlabMirrorArgs.MirrorMapping)
	if err != nil {
		errCh <- err
	}
	err = createProjects(sourceGitlabInstance, destinationGitlabInstance, gitlabMirrorArgs.MirrorMapping)
	if err != nil {
		errCh <- err
	}
	close(errCh)
	return utils.MergeErrors(errCh, 2)
}

func processFilters(filters *utils.MirrorMapping) (map[string]bool, map[string]bool, map[string]bool, map[string]bool) {
	sourceProjectFilters := make(map[string]bool)
	sourceGroupFilters := make(map[string]bool)
	destinationProjectFilters := make(map[string]bool)
	destinationGroupFilters := make(map[string]bool)

	// Initialize concurrency control
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)

	// Process group filters concurrently.
	go func() {
		defer wg.Done()
		for group, copyOptions := range filters.Groups {
			sourceGroupFilters[group] = true
			mu.Lock()
			destinationGroupFilters[copyOptions.DestinationPath] = true
			mu.Unlock()
		}
	}()

	// Process project filters concurrently.
	go func() {
		defer wg.Done()
		for project, copyOptions := range filters.Projects {
			sourceProjectFilters[project] = true
			destinationProjectFilters[copyOptions.DestinationPath] = true
			destinationGroupPath := filepath.Dir(copyOptions.DestinationPath)
			if destinationGroupPath != "" && destinationGroupPath != "." && destinationGroupPath != "/" {
				mu.Lock()
				destinationGroupFilters[destinationGroupPath] = true
				mu.Unlock()
			}

		}
	}()

	wg.Wait()
	return sourceProjectFilters, sourceGroupFilters, destinationProjectFilters, destinationGroupFilters
}
