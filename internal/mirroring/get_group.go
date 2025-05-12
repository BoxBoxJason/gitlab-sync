package mirroring

import (
	"fmt"
	"gitlab-sync/internal/utils"
	"path/filepath"
	"sync"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

// fetchAndProcessGroups retrieves all groups that match the filters from the GitLab instance and stores them in the instance cache.
// It also updates the mirror mapping with the corresponding group creation options.
//
// The function is run in a goroutine for each group, and a wait group is used to wait for all goroutines to finish.
func (g *GitlabInstance) fetchAndProcessGroups(groupFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) error {
	zap.L().Debug("Fetching and processing groups from GitLab instance", zap.String(ROLE, g.Role), zap.Int("groups", len(*groupFilters)))
	if !g.isBig() {
		return g.fetchAndProcessGroupsSmallInstance(groupFilters, mirrorMapping)
	}
	return g.fetchAndProcessGroupsLargeInstance(groupFilters, mirrorMapping)
}

// storeGroup stores the group in the Gitlab instance groups cache
// and updates the mirror mapping with the corresponding group creation options.
func (g *GitlabInstance) storeGroup(group *gitlab.Group, parentGroupPath string, mirrorMapping *utils.MirrorMapping) {
	if group != nil {
		// Add the group to the gitlab instance groups cache
		g.addGroup(group)

		if g.Role == ROLE_SOURCE {
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

// ===========================================================================
//                         SMALL INSTANCE FUNCTIONS                         //
// ===========================================================================

// fetchAndProcessGroupsSmallInstance retrieves all groups that match the filters from the GitLab instance and stores them in the instance cache.
// It also updates the mirror mapping with the corresponding group creation options.
func (g *GitlabInstance) fetchAndProcessGroupsSmallInstance(groupFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) error {
	allGroups, err := g.fetchAllGroupsSmallInstance()
	if err != nil {
		return err
	}

	g.processGroupsSmallInstance(allGroups, groupFilters, mirrorMapping)

	zap.L().Debug("Found matching groups in the GitLab instance", zap.String(ROLE, g.Role), zap.Int("groups", len(g.Groups)))

	return nil
}

// fetchAllGroupsSmallInstance retrieves all groups from the small GitLab instance
func (g *GitlabInstance) fetchAllGroupsSmallInstance() ([]*gitlab.Group, error) {
	zap.L().Debug("Fetching all groups from GitLab instance", zap.String(ROLE, g.Role))
	fetchOpts := &gitlab.ListGroupsOptions{
		AllAvailable: gitlab.Ptr(true),
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
			Page:    1,
		},
	}

	var allGroups []*gitlab.Group

	for {
		groups, resp, err := g.Gitlab.Groups.ListGroups(fetchOpts)
		if err != nil {
			return allGroups, err
		}
		allGroups = append(allGroups, groups...)
		if resp.CurrentPage >= resp.TotalPages {
			break
		}
		fetchOpts.Page = resp.NextPage
	}
	return allGroups, nil
}

// fetchAllGroupsSmallInstance retrieves all groups from the small GitLab instance
// It uses pagination to fetch all groups in batches of 100. Queries all groups of the instance
func (g *GitlabInstance) processGroupsSmallInstance(allGroups []*gitlab.Group, groupFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) {
	zap.L().Debug("Processing groups from GitLab instance", zap.String(INSTANCE_SIZE, g.InstanceSize), zap.String(ROLE, g.Role), zap.Int("groups", len(allGroups)))

	// Create a wait group to wait for all goroutines to finish
	var wg sync.WaitGroup

	for _, group := range allGroups {
		wg.Add(1)

		go func(group *gitlab.Group) {
			defer wg.Done()

			groupPath, matches := checkPathMatchesFilters(group.FullPath, nil, groupFilters)
			if matches {
				g.storeGroup(group, groupPath, mirrorMapping)
			}
		}(group)
	}

	wg.Wait()
}

// ===========================================================================
//                         LARGE INSTANCE FUNCTIONS                         //
// ===========================================================================

// fetchAndProcessGroupsLargeInstance retrieves all groups that match the filters from the GitLab instance and stores them in the instance cache.
// It also updates the mirror mapping with the corresponding group creation options.
// It uses goroutines to fetch groups and their projects concurrently.
func (g *GitlabInstance) fetchAndProcessGroupsLargeInstance(groupFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) error {
	errChan := make(chan error)
	var wg sync.WaitGroup
	wg.Add(len(*groupFilters))

	var (
		errs  []error
		errMu sync.Mutex
	)

	// Start an error collector goroutine.
	go func() {
		// This goroutine will run until errChan is closed.
		for err := range errChan {
			if err != nil {
				errMu.Lock()
				errs = append(errs, err)
				errMu.Unlock()
			}
		}
	}()

	for groupPath := range *groupFilters {
		go g.fetchAndProcessGroupRecursive(groupPath, groupPath, mirrorMapping, errChan, &wg)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(errChan)
	return utils.MergeErrors(errs, 2)
}

// fetchAndProcessGroupRecursive fetches a group and its projects recursively
// It uses a wait group to ensure that all goroutines finish befopidpre returning
// It sends the fetched group to the allGroupsChannel and the projects to the allProjectsChanel
//
// gid can be either an int, a string or a *gitlab.Group
func (g *GitlabInstance) fetchAndProcessGroupRecursive(gid any, fetchOriginPath string, mirrorMapping *utils.MirrorMapping, errChan chan error, wg *sync.WaitGroup) {
	defer wg.Done()

	var group *gitlab.Group
	var err error

	switch v := gid.(type) {
	case int, string:
		group, _, err = g.Gitlab.Groups.GetGroup(gid, &gitlab.GetGroupOptions{WithProjects: gitlab.Ptr(false)})
		if err != nil {
			errChan <- fmt.Errorf("failed to retrieve group %s: %v", gid, err)
		}
	case *gitlab.Group:
		group = v
	default:
		errChan <- fmt.Errorf("invalid group ID type %T (%v)", gid, gid)
		return
	}
	if group != nil {
		g.storeGroup(group, fetchOriginPath, mirrorMapping)
		if g.isSource() {
			wg.Add(2)
			// Fetch the projects of the group
			go g.fetchAndProcessGroupProjects(group, fetchOriginPath, mirrorMapping, errChan, wg)
			// Fetch the subgroups of the group
			go g.fetchAndProcessGroupSubgroups(group, fetchOriginPath, mirrorMapping, errChan, wg)
		}
	}
}

// fetchAndProcessGroupSubgroups retrieves all subgroups of a group
// and processes them to store in the instance cache.
func (g *GitlabInstance) fetchAndProcessGroupSubgroups(group *gitlab.Group, fetchOriginPath string, mirrorMapping *utils.MirrorMapping, errChan chan error, wg *sync.WaitGroup) {
	defer wg.Done()
	fetchOpts := &gitlab.ListSubGroupsOptions{
		AllAvailable: gitlab.Ptr(true),
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
			Page:    1,
		},
	}

	for {
		subgroups, resp, err := g.Gitlab.Groups.ListSubGroups(group.ID, fetchOpts)
		if err != nil {
			errChan <- fmt.Errorf("failed to retrieve subgroups for group %s: %v", group.FullPath, err)
			return
		}
		for _, subgroup := range subgroups {
			g.storeGroup(subgroup, fetchOriginPath, mirrorMapping)
			if g.isSource() {
				wg.Add(1)
				go g.fetchAndProcessGroupRecursive(subgroup, fetchOriginPath, mirrorMapping, errChan, wg)
			}
		}
		if resp.CurrentPage >= resp.TotalPages {
			break
		}
		fetchOpts.Page = resp.NextPage
	}
}
