package mirroring

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/boxboxjason/gitlab-sync/internal/utils"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	"go.uber.org/zap"
)

const (
	initialFetchWorkers        = 2
	initialFetchErrorBufferLen = 4
	processFilterWorkers       = 2
)

func createMirroringInstances(gitlabMirrorArgs *utils.ParserArgs) (*GitlabInstance, *GitlabInstance, error) {
	sourceGitlabSize := INSTANCE_SIZE_SMALL
	if gitlabMirrorArgs.SourceGitlabIsBig {
		sourceGitlabSize = INSTANCE_SIZE_BIG
	}

	sourceGitlabInstance, err := NewGitlabInstance(&GitlabInstanceOpts{
		GitlabURL:    gitlabMirrorArgs.SourceGitlabURL,
		GitlabToken:  gitlabMirrorArgs.SourceGitlabToken,
		Role:         ROLE_SOURCE,
		MaxRetries:   gitlabMirrorArgs.Retry,
		InstanceSize: sourceGitlabSize,
	})
	if err != nil {
		return nil, nil, err
	}

	destinationGitlabSize := INSTANCE_SIZE_SMALL
	if gitlabMirrorArgs.DestinationGitlabIsBig {
		destinationGitlabSize = INSTANCE_SIZE_BIG
	}

	destinationGitlabInstance, err := NewGitlabInstance(&GitlabInstanceOpts{
		GitlabURL:    gitlabMirrorArgs.DestinationGitlabURL,
		GitlabToken:  gitlabMirrorArgs.DestinationGitlabToken,
		Role:         ROLE_DESTINATION,
		MaxRetries:   gitlabMirrorArgs.Retry,
		InstanceSize: destinationGitlabSize,
	})
	if err != nil {
		return nil, nil, err
	}

	return sourceGitlabInstance, destinationGitlabInstance, nil
}

func setPullMirrorAvailability(destinationGitlabInstance *GitlabInstance, gitlabMirrorArgs *utils.ParserArgs) error {
	pullMirrorAvailable, err := destinationGitlabInstance.IsPullMirrorAvailable(gitlabMirrorArgs.ForcePremium, gitlabMirrorArgs.ForceNonPremium)
	switch {
	case err != nil:
		return err
	case pullMirrorAvailable:
		zap.L().Info("GitLab instance is compatible with the pull mirroring process", zap.String(ROLE, destinationGitlabInstance.Role), zap.String(INSTANCE_SIZE, destinationGitlabInstance.InstanceSize))
	default:
		zap.L().Warn("Destination GitLab instance is not compatible with the pull mirroring process (requires a >= 17.6 ; >= Premium destination GitLab instance)", zap.String(ROLE, destinationGitlabInstance.Role), zap.String(INSTANCE_SIZE, destinationGitlabInstance.InstanceSize))
		zap.L().Warn("Will use local pull / push mirroring instead (takes a lot longer)", zap.String(ROLE, destinationGitlabInstance.Role), zap.String(INSTANCE_SIZE, destinationGitlabInstance.InstanceSize))
	}

	destinationGitlabInstance.PullMirrorAvailable = pullMirrorAvailable

	return nil
}

func fetchInitialData(
	sourceGitlabInstance *GitlabInstance,
	destinationGitlabInstance *GitlabInstance,
	gitlabMirrorArgs *utils.ParserArgs,
	sourceProjectFilters map[string]struct{},
	sourceGroupFilters map[string]struct{},
	destinationProjectFilters map[string]struct{},
	destinationGroupFilters map[string]struct{},
	errChannel chan []error,
) {
	var waitGroup sync.WaitGroup
	waitGroup.Add(initialFetchWorkers)

	go func() {
		defer waitGroup.Done()

		errChannel <- sourceGitlabInstance.FetchAll(sourceProjectFilters, sourceGroupFilters, gitlabMirrorArgs.MirrorMapping)
	}()
	go func() {
		defer waitGroup.Done()

		errChannel <- destinationGitlabInstance.FetchAll(destinationProjectFilters, destinationGroupFilters, gitlabMirrorArgs.MirrorMapping)
	}()

	waitGroup.Wait()
}

// MirrorGitlabs is the main function that handles the mirroring process between two GitLab instances.
// It takes a ParserArgs struct as an argument, which contains the necessary parameters for the mirroring process.
// It creates two GitLab instances (source and destination) and fetches the groups and projects from both instances.
// It then processes the filters for groups and projects, and finally creates the groups and projects in the destination GitLab instance.
// If the dry run flag is set, it will only print the groups and projects that would be created or updated.
func MirrorGitlabs(gitlabMirrorArgs *utils.ParserArgs) []error {
	zap.L().Info("Starting GitLab mirroring process", zap.String(ROLE_SOURCE, gitlabMirrorArgs.SourceGitlabURL), zap.String(ROLE_DESTINATION, gitlabMirrorArgs.DestinationGitlabURL))

	sourceGitlabInstance, destinationGitlabInstance, err := createMirroringInstances(gitlabMirrorArgs)
	if err != nil {
		return []error{helpers.NewBlocking(err)}
	}

	err = setPullMirrorAvailability(destinationGitlabInstance, gitlabMirrorArgs)
	if err != nil {
		return []error{helpers.NewBlocking(err)}
	}

	sourceProjectFilters, sourceGroupFilters, destinationProjectFilters, destinationGroupFilters := processFilters(gitlabMirrorArgs.MirrorMapping)
	errCh := make(chan []error, initialFetchErrorBufferLen)
	fetchInitialData(
		sourceGitlabInstance,
		destinationGitlabInstance,
		gitlabMirrorArgs,
		sourceProjectFilters,
		sourceGroupFilters,
		destinationProjectFilters,
		destinationGroupFilters,
		errCh,
	)

	zap.L().Debug("Fully Computed Mirror Mapping", zap.Any("MirrorMapping", gitlabMirrorArgs.MirrorMapping))

	// In case of dry run, simply print the groups and projects that would be created or updated
	if gitlabMirrorArgs.DryRun {
		destinationGitlabInstance.DryRun(sourceGitlabInstance, gitlabMirrorArgs.MirrorMapping)

		return nil
	}

	// Create groups and projects in the destination GitLab instance (Groups must be created before projects)
	errCh <- destinationGitlabInstance.CreateGroups(sourceGitlabInstance, gitlabMirrorArgs.MirrorMapping)

	errCh <- destinationGitlabInstance.CreateProjects(sourceGitlabInstance, gitlabMirrorArgs.MirrorMapping)

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
	var (
		mappingMutex    sync.Mutex
		filterWaitGroup sync.WaitGroup
	)

	filterWaitGroup.Add(processFilterWorkers)

	// Process group filters concurrently
	go func() {
		defer filterWaitGroup.Done()

		for group, copyOptions := range filters.Groups {
			sourceGroupFilters[group] = struct{}{}

			mappingMutex.Lock()
			destinationGroupFilters[copyOptions.DestinationPath] = struct{}{}
			mappingMutex.Unlock()
		}
	}()

	// Process project filters concurrently
	go func() {
		defer filterWaitGroup.Done()

		for project, copyOptions := range filters.Projects {
			sourceProjectFilters[project] = struct{}{}
			destinationProjectFilters[copyOptions.DestinationPath] = struct{}{}

			destinationGroupPath := filepath.Dir(copyOptions.DestinationPath)
			if destinationGroupPath != "" && destinationGroupPath != "." && destinationGroupPath != "/" {
				mappingMutex.Lock()
				destinationGroupFilters[destinationGroupPath] = struct{}{}
				mappingMutex.Unlock()
			}
		}
	}()

	filterWaitGroup.Wait()

	return sourceProjectFilters, sourceGroupFilters, destinationProjectFilters, destinationGroupFilters
}

// DryRun prints the groups and projects that would be created or updated in dry run mode.
func (destinationGitlabInstance *GitlabInstance) DryRun(sourceGitlabInstance *GitlabInstance, mirrorMapping *utils.MirrorMapping) []error {
	zap.L().Info("Dry run mode enabled, will not create groups or projects")
	zap.L().Info("Groups that will be created (or updated if they already exist):")

	for sourceGroupPath, copyOptions := range mirrorMapping.Groups {
		if sourceGroup, ok := sourceGitlabInstance.Groups[sourceGroupPath]; ok {
			_, err := fmt.Fprintf(os.Stdout, "  - %s (source gitlab) -> %s (destination gitlab)\n", sourceGroup.WebURL, copyOptions.DestinationPath)
			if err != nil {
				return []error{helpers.NewNonBlocking(fmt.Errorf("failed to print group dry-run output: %w", err))}
			}
		}
	}

	zap.L().Info("Projects that will be created (or updated if they already exist):")

	for sourceProjectPath, copyOptions := range mirrorMapping.Projects {
		if sourceProject, ok := sourceGitlabInstance.Projects[sourceProjectPath]; ok {
			_, err := fmt.Fprintf(os.Stdout, "  - %s (source gitlab) -> %s (destination gitlab)\n", sourceProject.WebURL, copyOptions.DestinationPath)
			if err != nil {
				return []error{helpers.NewNonBlocking(fmt.Errorf("failed to print project dry-run output: %w", err))}
			}

			if helpers.Deref(copyOptions.MirrorReleases, false) {
				err := destinationGitlabInstance.DryRunReleases(sourceGitlabInstance, sourceProject, copyOptions)
				if err != nil {
					zap.L().Error("Failed to dry run releases", zap.Error(err))

					return []error{helpers.NewNonBlocking(err)}
				}
			}
		}
	}

	zap.L().Info("Dry run completed")

	return nil
}

// ===========================================================================
//                       INSTANCE HEALTH MANAGEMENT                         //
// ===========================================================================

// IsPullMirrorAvailable checks the destination GitLab instance for version and license compatibility.
func (g *GitlabInstance) IsPullMirrorAvailable(forcePremium, forceNonPremium bool) (bool, error) {
	zap.L().Info("Checking destination GitLab instance")

	thresholdOk, err := g.IsVersionGreaterThanThreshold()
	if err != nil {
		return false, fmt.Errorf("destination GitLab instance version check failed: %w", err)
	}

	isPremium, err := g.IsLicensePremium()
	if err != nil {
		if !forcePremium && !forceNonPremium {
			return false, fmt.Errorf("failed to check if destination GitLab instance is premium: %w", err)
		}
	}

	return !forceNonPremium && (thresholdOk && (isPremium || forcePremium)), nil
}
