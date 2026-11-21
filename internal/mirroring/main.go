package mirroring

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	"go.uber.org/zap"
)

const (
	initialFetchWorkers  = 2
	processFilterWorkers = 2
)

// mirroringInstances is the pair of GitLab instances taking part in a mirroring run.
type mirroringInstances struct {
	source      *GitlabInstance
	destination *GitlabInstance
}

func createMirroringInstances(gitlabMirrorArgs *mirrorcfg.ParserArgs) (mirroringInstances, error) {
	sourceGitlabSize := InstanceSizeSmall
	if gitlabMirrorArgs.SourceGitlabIsBig {
		sourceGitlabSize = InstanceSizeBig
	}

	sourceGitlabInstance, err := NewGitlabInstance(&GitlabInstanceOpts{
		GitlabURL:      gitlabMirrorArgs.SourceGitlabURL,
		GitlabToken:    gitlabMirrorArgs.SourceGitlabToken,
		Role:           RoleSource,
		MaxRetries:     gitlabMirrorArgs.Retry,
		APIConcurrency: gitlabMirrorArgs.APIConcurrency,
		GitConcurrency: gitlabMirrorArgs.GitConcurrency,
		InstanceSize:   sourceGitlabSize,
		UserAgent:      gitlabMirrorArgs.UserAgent,
	})
	if err != nil {
		return mirroringInstances{}, err
	}

	destinationGitlabSize := InstanceSizeSmall
	if gitlabMirrorArgs.DestinationGitlabIsBig {
		destinationGitlabSize = InstanceSizeBig
	}

	destinationGitlabInstance, err := NewGitlabInstance(&GitlabInstanceOpts{
		GitlabURL:      gitlabMirrorArgs.DestinationGitlabURL,
		GitlabToken:    gitlabMirrorArgs.DestinationGitlabToken,
		Role:           RoleDestination,
		MaxRetries:     gitlabMirrorArgs.Retry,
		APIConcurrency: gitlabMirrorArgs.APIConcurrency,
		GitConcurrency: gitlabMirrorArgs.GitConcurrency,
		InstanceSize:   destinationGitlabSize,
		UserAgent:      gitlabMirrorArgs.UserAgent,
	})
	if err != nil {
		return mirroringInstances{}, err
	}

	return mirroringInstances{source: sourceGitlabInstance, destination: destinationGitlabInstance}, nil
}

func setPullMirrorAvailability(destinationGitlabInstance *GitlabInstance, gitlabMirrorArgs *mirrorcfg.ParserArgs) error {
	pullMirrorAvailable, err := destinationGitlabInstance.IsPullMirrorAvailable(gitlabMirrorArgs)
	switch {
	case err != nil:
		return err
	case pullMirrorAvailable:
		zap.L().Info("GitLab instance is compatible with the pull mirroring process", zap.String(RoleKey, destinationGitlabInstance.Role), zap.String(InstanceSizeKey, destinationGitlabInstance.InstanceSize))
	default:
		zap.L().Warn("Destination GitLab instance is not compatible with the pull mirroring process (requires a >= 17.6 ; >= Premium destination GitLab instance)", zap.String(RoleKey, destinationGitlabInstance.Role), zap.String(InstanceSizeKey, destinationGitlabInstance.InstanceSize))
		zap.L().Warn("Will use local pull / push mirroring instead (takes a lot longer)", zap.String(RoleKey, destinationGitlabInstance.Role), zap.String(InstanceSizeKey, destinationGitlabInstance.InstanceSize))
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
func setupGitCache(destinationGitlabInstance *GitlabInstance, gitlabMirrorArgs *mirrorcfg.ParserArgs) error {
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
	gitlabMirrorArgs *mirrorcfg.ParserArgs,
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
func MirrorGitlabs(gitlabMirrorArgs *mirrorcfg.ParserArgs) {
	zap.L().Info("Starting GitLab mirroring process", zap.String(RoleSource, gitlabMirrorArgs.SourceGitlabURL), zap.String(RoleDestination, gitlabMirrorArgs.DestinationGitlabURL))

	instances, err := createMirroringInstances(gitlabMirrorArgs)
	if err != nil {
		helpers.ReportBlocking(err)

		return
	}

	sourceGitlabInstance, destinationGitlabInstance := instances.source, instances.destination

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

	filters := processFilters(gitlabMirrorArgs.MirrorMapping)
	fetchInitialData(
		sourceGitlabInstance,
		destinationGitlabInstance,
		gitlabMirrorArgs,
		filters.sourceProjects,
		filters.sourceGroups,
		filters.destinationProjects,
		filters.destinationGroups,
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

// mirrorFilters holds the project and group paths to fetch on each side of the mirroring.
type mirrorFilters struct {
	sourceProjects      map[string]struct{}
	sourceGroups        map[string]struct{}
	destinationProjects map[string]struct{}
	destinationGroups   map[string]struct{}
}

// processFilters processes the filters for groups and projects.
// It returns the source and destination project and group filters.
func processFilters(filters *mirrorcfg.MirrorMapping) mirrorFilters {
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

	return mirrorFilters{
		sourceProjects:      sourceProjectFilters,
		sourceGroups:        sourceGroupFilters,
		destinationProjects: destinationProjectFilters,
		destinationGroups:   destinationGroupFilters,
	}
}

// ===========================================================================
//                       INSTANCE HEALTH MANAGEMENT                         //
// ===========================================================================
