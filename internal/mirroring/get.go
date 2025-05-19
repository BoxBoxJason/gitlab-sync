package mirroring

import (
	"fmt"
	"gitlab-sync/internal/utils"
	"gitlab-sync/pkg/helpers"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Masterminds/semver/v3"
	"go.uber.org/zap"
)

const (
	INSTANCE_SEMVER_THRESHOLD = "17.6"
	ULTIMATE_PLAN             = "ultimate"
	PREMIUM_PLAN              = "premium"
)

// fetchAll retrieves all projects and groups from the GitLab instance
// that match the filters and stores them in the instance cache.
func (g *GitlabInstance) fetchAll(projectFilters map[string]struct{}, groupFilters map[string]struct{}, mirrorMapping *utils.MirrorMapping) []error {
	zap.L().Info("Fetching all projects and groups from GitLab instance", zap.String(ROLE, g.Role), zap.String(INSTANCE_SIZE, g.InstanceSize), zap.Int("projects", len(projectFilters)), zap.Int("groups", len(groupFilters)))
	wg := sync.WaitGroup{}
	errCh := make(chan []error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := g.fetchAndProcessGroups(&groupFilters, mirrorMapping); err != nil {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		if err := g.fetchAndProcessProjects(&projectFilters, &groupFilters, mirrorMapping); err != nil {
			errCh <- err
		}
	}()
	wg.Wait()
	close(errCh)

	return helpers.MergeErrors(errCh)
}

// getParentNamespaceID retrieves the parent namespace ID for a given project or group path.
// It checks if the parent path is already in the instance groups cache.
//
// If not, it returns an error indicating that the parent group was not found.
func (g *GitlabInstance) getParentNamespaceID(projectOrGroupPath string) (int, error) {
	parentGroupID := -1
	parentPath := filepath.Dir(projectOrGroupPath)
	var err error = nil
	if parentPath != "." && parentPath != "/" {
		// Check if parent path is already in the instance groups cache
		if parentGroup, ok := g.Groups[parentPath]; ok {
			parentGroupID = parentGroup.ID
		} else {
			err = fmt.Errorf("parent group not found for path: %s", parentPath)
		}
	}
	return parentGroupID, err
}

// checkPathMatchesFilters checks if the resources matches the filters
//   - either is in the projects map
//   - or path starts with any of the groups in the groups map
//
// In the case of a match with a group, it returns the group path
func checkPathMatchesFilters(resourcePath string, projectFilters *map[string]struct{}, groupFilters *map[string]struct{}) (string, bool) {
	zap.L().Debug("Checking if path matches filters", zap.String("path", resourcePath))
	if projectFilters != nil {
		if _, ok := (*projectFilters)[resourcePath]; ok {
			zap.L().Debug("Resource path matches project filter", zap.String("project", resourcePath))
			return "", true
		}
	}
	if groupFilters != nil {
		for groupPath := range *groupFilters {
			if strings.HasPrefix(resourcePath, groupPath) {
				zap.L().Debug("Resource path matches group filter", zap.String("resource", resourcePath), zap.String("group", groupPath))
				return groupPath, true
			}
		}
	}
	return "", false
}

func (g *GitlabInstance) CheckVersion() error {
	metadata, _, err := g.Gitlab.Metadata.GetMetadata()
	if err != nil {
		return fmt.Errorf("failed to get GitLab version: %w", err)
	}
	zap.L().Debug("GitLab Instance version", zap.String(ROLE, g.Role), zap.String("version", metadata.Version))

	currentVer, err := semver.NewVersion(metadata.Version)
	if err != nil {
		return fmt.Errorf("failed to parse GitLab version: %w", err)
	}
	thresholdVer, err := semver.NewVersion(INSTANCE_SEMVER_THRESHOLD)
	if err != nil {
		return fmt.Errorf("failed to parse version threshold: %w", err)
	}

	if currentVer.LessThan(thresholdVer) {
		return fmt.Errorf("GitLab version %s is below required threshold %s", currentVer, thresholdVer)
	}
	return nil
}

func (g *GitlabInstance) CheckLicense() error {
	license, _, err := g.Gitlab.License.GetLicense()
	if err != nil {
		return fmt.Errorf("failed to get GitLab license: %w", err)
	}
	if license.Plan != ULTIMATE_PLAN && license.Plan != PREMIUM_PLAN {
		return fmt.Errorf("GitLab license plan %s is not supported, only %s and %s are supported", license.Plan, ULTIMATE_PLAN, PREMIUM_PLAN)
	} else if license.Expired {
		return fmt.Errorf("GitLab license is expired")
	}

	zap.L().Debug("GitLab Instance license", zap.String(ROLE, g.Role), zap.String("plan", license.Plan))
	return nil
}
