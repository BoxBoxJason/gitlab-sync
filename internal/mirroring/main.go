package mirroring

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/boxboxjason/gitlab-sync/internal/utils"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	"go.uber.org/zap"
)

const (
	initialFetchWorkers  = 2
	processFilterWorkers = 2
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

// setupGitCache attaches the on-disk repository cache to the destination
// instance and expires the entries that went unused for too long.
//
// The cache only ever backs the clone/push path, so it is skipped entirely on a
// premium destination: pull mirroring never touches a local clone, and creating
// the directory there would only leave an empty tree behind.
func setupGitCache(destinationGitlabInstance *GitlabInstance, gitlabMirrorArgs *utils.ParserArgs) error {
	cacheDir := strings.TrimSpace(gitlabMirrorArgs.CacheDir)
	if cacheDir == "" {
		return nil
	}

	if destinationGitlabInstance.PullMirrorAvailable {
		zap.L().Debug("Ignoring the git repository cache: the destination uses pull mirroring", zap.String("path", cacheDir))

		return nil
	}

	cache, err := helpers.NewGitCache(cacheDir, gitlabMirrorArgs.CacheMaxAge)
	if err != nil {
		return fmt.Errorf("failed to set up the git repository cache: %w", err)
	}

	cache.Prune()

	destinationGitlabInstance.GitCache = cache

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
) {
	var waitGroup sync.WaitGroup
	waitGroup.Add(initialFetchWorkers)

	go func() {
		defer waitGroup.Done()

		sourceGitlabInstance.FetchAll(sourceProjectFilters, sourceGroupFilters, gitlabMirrorArgs.MirrorMapping)
	}()
	go func() {
		defer waitGroup.Done()

		destinationGitlabInstance.FetchAll(destinationProjectFilters, destinationGroupFilters, gitlabMirrorArgs.MirrorMapping)
	}()

	waitGroup.Wait()
}

// MirrorGitlabs is the main function that handles the mirroring process between two GitLab instances.
// It takes a ParserArgs struct as an argument, which contains the necessary parameters for the mirroring process.
// It creates two GitLab instances (source and destination) and fetches the groups and projects from both instances.
// It then processes the filters for groups and projects, and finally creates the groups and projects in the destination GitLab instance.
// If the dry run flag is set, it will only print the groups and projects that would be created or updated.
//
// Errors are logged the moment they happen (via helpers.Report); callers read
// the aggregate outcome through helpers.ExitCode.
func MirrorGitlabs(gitlabMirrorArgs *utils.ParserArgs) {
	zap.L().Info("Starting GitLab mirroring process", zap.String(ROLE_SOURCE, gitlabMirrorArgs.SourceGitlabURL), zap.String(ROLE_DESTINATION, gitlabMirrorArgs.DestinationGitlabURL))

	sourceGitlabInstance, destinationGitlabInstance, err := createMirroringInstances(gitlabMirrorArgs)
	if err != nil {
		helpers.ReportBlocking(err)

		return
	}

	err = setPullMirrorAvailability(destinationGitlabInstance, gitlabMirrorArgs)
	if err != nil {
		helpers.ReportBlocking(err)

		return
	}

	// Done before the (long) initial fetch so an unusable cache directory is
	// reported straight away. A dry run writes nothing, so it needs no cache.
	if !gitlabMirrorArgs.DryRun {
		err = setupGitCache(destinationGitlabInstance, gitlabMirrorArgs)
		if err != nil {
			helpers.ReportBlocking(err)

			return
		}
	}

	sourceProjectFilters, sourceGroupFilters, destinationProjectFilters, destinationGroupFilters := processFilters(gitlabMirrorArgs.MirrorMapping)
	fetchInitialData(
		sourceGitlabInstance,
		destinationGitlabInstance,
		gitlabMirrorArgs,
		sourceProjectFilters,
		sourceGroupFilters,
		destinationProjectFilters,
		destinationGroupFilters,
	)

	zap.L().Debug("Fully Computed Mirror Mapping", zap.Any("MirrorMapping", gitlabMirrorArgs.MirrorMapping))

	// In case of dry run, simply print the groups and projects that would be created or updated
	if gitlabMirrorArgs.DryRun {
		destinationGitlabInstance.DryRun(sourceGitlabInstance, gitlabMirrorArgs.MirrorMapping)

		return
	}

	// Create groups and projects in the destination GitLab instance (Groups must be created before projects)
	destinationGitlabInstance.CreateGroups(sourceGitlabInstance, gitlabMirrorArgs.MirrorMapping)

	destinationGitlabInstance.CreateProjects(sourceGitlabInstance, gitlabMirrorArgs.MirrorMapping)
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

		for group, copyOptions := range filters.GroupsSnapshot() {
			sourceGroupFilters[group] = struct{}{}

			mappingMutex.Lock()
			destinationGroupFilters[copyOptions.DestinationPath] = struct{}{}
			mappingMutex.Unlock()
		}
	}()

	// Process project filters concurrently
	go func() {
		defer filterWaitGroup.Done()

		for project, copyOptions := range filters.ProjectsSnapshot() {
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
func (destinationGitlabInstance *GitlabInstance) DryRun(sourceGitlabInstance *GitlabInstance, mirrorMapping *utils.MirrorMapping) {
	zap.L().Info("Dry run mode enabled, will not create groups or projects")
	zap.L().Info("Groups that will be created (or updated if they already exist):")

	for sourceGroupPath, copyOptions := range mirrorMapping.GroupsSnapshot() {
		if sourceGroup := sourceGitlabInstance.GetGroup(sourceGroupPath); sourceGroup != nil {
			_, err := fmt.Fprintf(os.Stdout, "  - %s (source gitlab) -> %s (destination gitlab)\n", sourceGroup.WebURL, copyOptions.DestinationPath)
			if err != nil {
				helpers.ReportNonBlocking(fmt.Errorf("failed to print group dry-run output: %w", err))

				return
			}
		}
	}

	zap.L().Info("Projects that will be created (or updated if they already exist):")

	for sourceProjectPath, copyOptions := range mirrorMapping.ProjectsSnapshot() {
		if sourceProject := sourceGitlabInstance.GetProject(sourceProjectPath); sourceProject != nil {
			_, err := fmt.Fprintf(os.Stdout, "  - %s (source gitlab) -> %s (destination gitlab)\n", sourceProject.WebURL, copyOptions.DestinationPath)
			if err != nil {
				helpers.ReportNonBlocking(fmt.Errorf("failed to print project dry-run output: %w", err))

				return
			}

			if helpers.Deref(copyOptions.MirrorReleases, false) {
				err = destinationGitlabInstance.DryRunReleases(sourceGitlabInstance, sourceProject, copyOptions)
				if err != nil {
					helpers.ReportNonBlocking(fmt.Errorf("failed to dry run releases: %w", err))

					return
				}
			}
		}
	}

	zap.L().Info("Dry run completed")
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
