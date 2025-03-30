package mirroring

import (
	"path/filepath"
	"sync"

	"github.com/boxboxjason/gitlab-sync/utils"
)

func MirrorGitlabs(sourceGitlabURL string, sourceGitlabToken string, destinationGitlabURL string, destinationGitlabToken string, mirrorMapping *utils.MirrorMapping) error {
	sourceGitlabInstance, err := newGitlabInstance(sourceGitlabURL, sourceGitlabToken)
	if err != nil {
		return err
	}

	destinationGitlabInstance, err := newGitlabInstance(destinationGitlabURL, destinationGitlabToken)
	if err != nil {
		return err
	}

	sourceProjectFilters, sourceGroupFilters, destinationProjectFilters, destinationGroupFilters := processFilters(mirrorMapping)

	wg := sync.WaitGroup{}
	errCh := make(chan error, 4)
	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := fetchAll(sourceGitlabInstance, sourceProjectFilters, sourceGroupFilters, mirrorMapping, true); err != nil {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		if err := fetchAll(destinationGitlabInstance, destinationProjectFilters, destinationGroupFilters, mirrorMapping, false); err != nil {
			errCh <- err
		}
	}()

	wg.Wait()

	err = createGroups(sourceGitlabInstance, destinationGitlabInstance, mirrorMapping)
	if err != nil {
		errCh <- err
	}
	err = createProjects(sourceGitlabInstance, destinationGitlabInstance, mirrorMapping)
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
