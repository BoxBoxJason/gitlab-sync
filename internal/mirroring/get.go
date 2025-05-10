package mirroring

import (
	"fmt"
	"gitlab-sync/internal/utils"
	"path/filepath"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// fetchAll retrieves all projects and groups from the GitLab instance
// that match the filters and stores them in the instance cache.
func (g *GitlabInstance) fetchAll(projectFilters map[string]struct{}, groupFilters map[string]struct{}, mirrorMapping *utils.MirrorMapping) error {
	wg := sync.WaitGroup{}
	errCh := make(chan error, 2)
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

	return utils.MergeErrors(errCh, 2)
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
