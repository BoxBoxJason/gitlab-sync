package mirroring

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"gitlab-sync/internal/utils"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

// fetchProjects retrieves all projects that match the filters from the GitLab instance and stores them in the instance cache.
// It also updates the mirror mapping with the corresponding group creation options.
//
// The function is run in a goroutine for each project, and a wait group is used to wait for all goroutines to finish.
func (g *GitlabInstance) fetchProjects(projectFilters *map[string]bool, groupFilters *map[string]bool, mirrorMapping *utils.MirrorMapping, isSource bool) error {
	sourceString := "source"
	if !isSource {
		sourceString = "destination"
	}
	zap.L().Debug("Fetching all projects from GitLab instance", zap.String("role", sourceString))
	projects, _, err := g.Gitlab.Projects.ListProjects(nil)
	if err != nil {
		return err
	}

	zap.L().Debug("Processing projects from GitLab instance", zap.String("role", sourceString), zap.Int("projects", len(projects)))

	// Create a wait group to wait for all goroutines to finish
	var wg sync.WaitGroup

	for _, project := range projects {
		wg.Add(1)

		go func(project *gitlab.Project) {
			defer wg.Done()

			group, matches := g.checkPathMatchesFilters(project.PathWithNamespace, projectFilters, groupFilters)
			if matches {
				g.storeProject(project, group, mirrorMapping, isSource)
			}

		}(project)
	}

	wg.Wait()

	zap.L().Debug("Found matching projects in the GitLab instance", zap.String("role", sourceString), zap.Int("projects", len(g.Projects)))
	return nil
}

// storeProject stores the project in the Gitlab instance projects cache
// and updates the mirror mapping with the corresponding group creation options.
func (g *GitlabInstance) storeProject(project *gitlab.Project, parentGroupPath string, mirrorMapping *utils.MirrorMapping, isSource bool) {
	// Add the project to the gitlab instance projects cache
	g.addProject(project.PathWithNamespace, project)

	if isSource {
		zap.L().Debug("Storing project in mirror mapping", zap.String("project", project.HTTPURLToRepo), zap.String("group", parentGroupPath))
		// Retrieve the corresponding group creation options from the mirror mapping
		groupCreationOptions, ok := mirrorMapping.GetGroup(parentGroupPath)
		if !ok {
			zap.L().Error("Group not found in mirror mapping", zap.String("group", parentGroupPath))
			return
		}

		// Calculate the relative path between the project and the group
		relativePath, err := filepath.Rel(parentGroupPath, project.PathWithNamespace)
		if err != nil {
			zap.L().Error("Failed to calculate relative path for project", zap.String("project", project.HTTPURLToRepo), zap.String("group", parentGroupPath), zap.Error(err))
			return
		}

		// Add the project to the mirror mapping with the corresponding group creation options
		mirrorMapping.AddProject(project.PathWithNamespace, &utils.MirroringOptions{
			DestinationPath:     filepath.Join(groupCreationOptions.DestinationPath, relativePath),
			CI_CD_Catalog:       groupCreationOptions.CI_CD_Catalog,
			Issues:              groupCreationOptions.Issues,
			MirrorTriggerBuilds: groupCreationOptions.MirrorTriggerBuilds,
			Visibility:          groupCreationOptions.Visibility,
			MirrorReleases:      groupCreationOptions.MirrorReleases,
		})
	}
}

// fetchGroups retrieves all groups that match the filters from the GitLab instance and stores them in the instance cache.
// It also updates the mirror mapping with the corresponding group creation options.
//
// The function is run in a goroutine for each group, and a wait group is used to wait for all goroutines to finish.
func (g *GitlabInstance) fetchGroups(groupFilters *map[string]bool, mirrorMapping *utils.MirrorMapping, isSource bool) error {
	sourceString := "source"
	if !isSource {
		sourceString = "destination"
	}
	zap.L().Debug("Fetching all groups from GitLab instance", zap.String("role", sourceString))
	groups, _, err := g.Gitlab.Groups.ListGroups(nil)
	if err != nil {
		return err
	}

	zap.L().Debug("Processing groups from GitLab instance", zap.String("role", sourceString), zap.Int("groups", len(groups)))

	// Create a wait group to wait for all goroutines to finish
	var wg sync.WaitGroup

	for _, group := range groups {
		wg.Add(1)

		go func(group *gitlab.Group) {
			defer wg.Done()

			groupPath, matches := g.checkPathMatchesFilters(group.FullPath, nil, groupFilters)
			if matches {
				g.storeGroup(group, groupPath, mirrorMapping, isSource)
			}
		}(group)
	}

	wg.Wait()

	zap.L().Debug("Found matching groups in the GitLab instance", zap.String("role", sourceString), zap.Int("groups", len(g.Groups)))

	return nil
}

// storeGroup stores the group in the Gitlab instance groups cache
// and updates the mirror mapping with the corresponding group creation options.
func (g *GitlabInstance) storeGroup(group *gitlab.Group, parentGroupPath string, mirrorMapping *utils.MirrorMapping, isSource bool) {
	if group != nil {
		// Add the group to the gitlab instance groups cache
		g.addGroup(group.FullPath, group)

		if isSource {
			zap.L().Debug("Storing group in mirror mapping", zap.String("group", group.FullPath), zap.String("parentGroup", parentGroupPath))
			// Retrieve the corresponding group creation options from the mirror mapping
			groupCreationOptions, ok := mirrorMapping.Groups[parentGroupPath]
			if !ok {
				zap.L().Error("Group not found in mirror mapping", zap.String("group", parentGroupPath))
				return
			}

			// Calculate the relative path between the group and the parent group
			relativePath, err := filepath.Rel(parentGroupPath, group.FullPath)
			if err != nil {
				zap.L().Error("Failed to calculate relative path for group", zap.String("group", group.FullPath), zap.String("parentGroup", parentGroupPath), zap.Error(err))
				return
			}

			// Add the group to the mirror mapping
			mirrorMapping.AddGroup(group.FullPath, &utils.MirroringOptions{
				DestinationPath:     filepath.Join(groupCreationOptions.DestinationPath, relativePath),
				CI_CD_Catalog:       groupCreationOptions.CI_CD_Catalog,
				Issues:              groupCreationOptions.Issues,
				MirrorTriggerBuilds: groupCreationOptions.MirrorTriggerBuilds,
				Visibility:          groupCreationOptions.Visibility,
				MirrorReleases:      groupCreationOptions.MirrorReleases,
			})
		}
	} else {
		zap.L().Error("Failed to store group in mirror mapping: nil group")
	}
}

// checkPathMatchesFilters checks if the resources matches the filters
//   - either is in the projects map
//   - or path starts with any of the groups in the groups map
//
// In the case of a match with a group, it returns the group path
func (g *GitlabInstance) checkPathMatchesFilters(resourcePath string, projectFilters *map[string]bool, groupFilters *map[string]bool) (string, bool) {
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

// fetchAll retrieves all projects and groups from the GitLab instance
// that match the filters and stores them in the instance cache.
func fetchAll(gitlabInstance *GitlabInstance, projectFilters map[string]bool, groupFilters map[string]bool, mirrorMapping *utils.MirrorMapping, isSource bool) error {
	wg := sync.WaitGroup{}
	errCh := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := gitlabInstance.fetchGroups(&groupFilters, mirrorMapping, isSource); err != nil {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		if err := gitlabInstance.fetchProjects(&projectFilters, &groupFilters, mirrorMapping, isSource); err != nil {
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
