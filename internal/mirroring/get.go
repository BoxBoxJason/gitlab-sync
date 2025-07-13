package mirroring

import (
	"fmt"
	"gitlab-sync/internal/utils"
	"gitlab-sync/pkg/helpers"
	"path/filepath"
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

// IsVersionGreaterThanThreshold checks if the GitLab instance version is below the defined threshold.
// It retrieves the metadata from the GitLab instance and compares the version
// with the INSTANCE_SEMVER_THRESHOLD.
func (g *GitlabInstance) IsVersionGreaterThanThreshold() (bool, error) {
	metadata, _, err := g.Gitlab.Metadata.GetMetadata()
	if err != nil {
		return false, fmt.Errorf("failed to get GitLab version: %w", err)
	}
	zap.L().Debug("GitLab Instance version", zap.String(ROLE, g.Role), zap.String("version", metadata.Version))

	currentVer, err := semver.NewVersion(metadata.Version)
	if err != nil {
		return false, fmt.Errorf("failed to parse GitLab version: %w", err)
	}
	thresholdVer, err := semver.NewVersion(INSTANCE_SEMVER_THRESHOLD)
	if err != nil {
		return false, fmt.Errorf("failed to parse version threshold: %w", err)
	}

	return currentVer.GreaterThanEqual(thresholdVer), nil
}

// IsLicensePremium checks if the GitLab instance has a premium license.
// It retrieves the license information and checks the plan type.
func (g *GitlabInstance) IsLicensePremium() (bool, error) {
	license, _, err := g.Gitlab.License.GetLicense()
	if err != nil {
		return false, fmt.Errorf("failed to get GitLab license: %w", err)
	}
	zap.L().Info("GitLab Instance license", zap.String(ROLE, g.Role), zap.String("plan", license.Plan))
	if license.Plan != ULTIMATE_PLAN && license.Plan != PREMIUM_PLAN || license.Expired {
		return false, nil
	}
	return true, nil
}
