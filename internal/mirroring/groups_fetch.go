package mirroring

import (
	"fmt"
	"path/filepath"
	"sync"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.uber.org/zap"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

// FetchAndProcessGroups retrieves all groups that match the filters from the GitLab instance and stores them in the instance cache.
// It also updates the mirror mapping with the corresponding group creation options.
//
// The function is run in a goroutine for each group, and a wait group is used to wait for all goroutines to finish.
func (g *GitlabInstance) FetchAndProcessGroups(groupFilters map[string]struct{}, mirrorMapping *mirrorcfg.MirrorMapping) {
	zap.L().Debug("Fetching and processing groups from GitLab instance", zap.String(RoleKey, g.Role), zap.Int("groups", len(groupFilters)))

	if !g.IsBig() {
		helpers.Report(g.FetchAndProcessGroupsSmallInstance(groupFilters, mirrorMapping))

		return
	}

	g.FetchAndProcessGroupsLargeInstance(groupFilters, mirrorMapping)
}

// StoreGroup stores the group in the Gitlab instance groups cache
// and updates the mirror mapping with the corresponding group creation options.
func (g *GitlabInstance) StoreGroup(group *gitlab.Group, parentGroupPath string, mirrorMapping *mirrorcfg.MirrorMapping) {
	if group == nil {
		zap.L().Error("Failed to store group in mirror mapping: nil group")

		return
	}

	// Add the group to the gitlab instance groups cache
	g.AddGroup(group)

	if g.Role == RoleSource {
		zap.L().Debug("Storing group in mirror mapping", zap.String("group", group.FullPath), zap.String("parentGroup", parentGroupPath))
		// Retrieve the corresponding group creation options from the mirror mapping
		groupCreationOptions, ok := mirrorMapping.GetGroup(parentGroupPath)
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
		mirrorMapping.AddGroup(group.FullPath, &mirrorcfg.MirroringOptions{
			DestinationPath:     filepath.Join(groupCreationOptions.DestinationPath, relativePath),
			CICDCatalog:         groupCreationOptions.CICDCatalog,
			MirrorIssues:        groupCreationOptions.MirrorIssues,
			MirrorTriggerBuilds: groupCreationOptions.MirrorTriggerBuilds,
			Visibility:          groupCreationOptions.Visibility,
			MirrorReleases:      groupCreationOptions.MirrorReleases,
			MirrorReleaseAssets: groupCreationOptions.MirrorReleaseAssets,
		})
	}
}

// FetchAndProcessGroupsSmallInstance retrieves all groups that match the filters from the GitLab instance and stores them in the instance cache.
// It also updates the mirror mapping with the corresponding group creation options.
func (g *GitlabInstance) FetchAndProcessGroupsSmallInstance(groupFilters map[string]struct{}, mirrorMapping *mirrorcfg.MirrorMapping) error {
	allGroups, err := g.FetchAllGroupsSmallInstance()
	if err != nil {
		return err
	}

	g.ProcessGroupsSmallInstance(allGroups, groupFilters, mirrorMapping)

	zap.L().Debug("Found matching groups in the GitLab instance", zap.String(RoleKey, g.Role), zap.Int("groups", g.GroupsLen()))

	return nil
}

// FetchAllGroupsSmallInstance retrieves all groups from the small GitLab instance.
func (g *GitlabInstance) FetchAllGroupsSmallInstance() ([]*gitlab.Group, error) {
	zap.L().Debug("Fetching all groups from GitLab instance", zap.String(RoleKey, g.Role))

	fetchOpts := &gitlab.ListGroupsOptions{
		AllAvailable: new(true),
		ListOptions: gitlab.ListOptions{
			PerPage: groupsPerPage,
			Page:    1,
		},
	}

	var allGroups []*gitlab.Group

	for {
		groups, resp, err := g.Gitlab.Groups.ListGroups(fetchOpts)
		if err != nil {
			return allGroups, fmt.Errorf("failed to list groups: %w", err)
		}

		allGroups = append(allGroups, groups...)

		if resp.CurrentPage >= resp.TotalPages {
			break
		}

		fetchOpts.Page = resp.NextPage
	}

	return allGroups, nil
}

// ProcessGroupsSmallInstance processes a list of groups from a small GitLab instance.
// It iterates over the provided allGroups slice, applies the groupFilters, and stores matching groups.
func (g *GitlabInstance) ProcessGroupsSmallInstance(allGroups []*gitlab.Group, groupFilters map[string]struct{}, mirrorMapping *mirrorcfg.MirrorMapping) {
	zap.L().Debug("Processing groups from GitLab instance", zap.String(InstanceSizeKey, g.InstanceSize), zap.String(RoleKey, g.Role), zap.Int("groups", len(allGroups)))

	// Create a wait group to wait for all goroutines to finish
	var waitGroup sync.WaitGroup

	for _, group := range allGroups {
		waitGroup.Add(1)

		go func(group *gitlab.Group) {
			defer waitGroup.Done()

			groupPath, matches := helpers.MatchPathAgainstFilters(group.FullPath, nil, groupFilters)
			if matches {
				g.StoreGroup(group, groupPath, mirrorMapping)
			}
		}(group)
	}

	waitGroup.Wait()
}

// FetchAndProcessGroupsLargeInstance retrieves all groups that match the filters from the GitLab instance and stores them in the instance cache.
// It also updates the mirror mapping with the corresponding group creation options.
// It uses goroutines to fetch groups and their projects concurrently.
func (g *GitlabInstance) FetchAndProcessGroupsLargeInstance(groupFilters map[string]struct{}, mirrorMapping *mirrorcfg.MirrorMapping) {
	var recursiveGroupWaitGroup sync.WaitGroup
	recursiveGroupWaitGroup.Add(len(groupFilters))

	for groupPath := range groupFilters {
		go g.FetchAndProcessGroupRecursive(groupPath, groupPath, mirrorMapping, &recursiveGroupWaitGroup)
	}

	// Wait for all goroutines to finish
	recursiveGroupWaitGroup.Wait()
}

// FetchAndProcessGroupRecursive fetches a group and its projects recursively
// It uses a wait group to ensure that all goroutines finish before returning
// It sends the fetched group to the allGroupsChannel and the projects to the allProjectsChannel
//
// gid can be either an int, a string or a *gitlab.Group.
func (g *GitlabInstance) FetchAndProcessGroupRecursive(gid any, fetchOriginPath string, mirrorMapping *mirrorcfg.MirrorMapping, recursiveGroupWaitGroup *sync.WaitGroup) {
	defer recursiveGroupWaitGroup.Done()

	var (
		group *gitlab.Group
		err   error
	)

	switch groupIdentifier := gid.(type) {
	case int, string:
		group, _, err = g.Gitlab.Groups.GetGroup(gid, &gitlab.GetGroupOptions{})
		if err != nil {
			helpers.Report(fmt.Errorf("failed to retrieve group %s: %w", gid, err))
		}
	case *gitlab.Group:
		group = groupIdentifier
	default:
		helpers.Report(fmt.Errorf("invalid group ID type %T (%v)", gid, gid))

		return
	}

	if group != nil {
		g.StoreGroup(group, fetchOriginPath, mirrorMapping)

		if g.IsSource() || g.IsBig() {
			recursiveGroupWaitGroup.Add(subgroupWorkerCount)
			// Fetch the projects of the group
			go g.FetchAndProcessGroupProjects(group, fetchOriginPath, mirrorMapping, recursiveGroupWaitGroup)
			// Fetch the subgroups of the group
			go g.FetchAndProcessGroupSubgroups(group, fetchOriginPath, mirrorMapping, recursiveGroupWaitGroup)
		}
	}
}

// FetchAndProcessGroupSubgroups retrieves all subgroups of a group
// and processes them to store in the instance cache.
func (g *GitlabInstance) FetchAndProcessGroupSubgroups(group *gitlab.Group, fetchOriginPath string, mirrorMapping *mirrorcfg.MirrorMapping, recursiveGroupWaitGroup *sync.WaitGroup) {
	defer recursiveGroupWaitGroup.Done()

	fetchOpts := &gitlab.ListSubGroupsOptions{
		AllAvailable: new(true),
		ListOptions: gitlab.ListOptions{
			PerPage: groupsPerPage,
			Page:    1,
		},
	}

	for {
		subgroups, resp, err := g.Gitlab.Groups.ListSubGroups(group.ID, fetchOpts)
		if err != nil {
			helpers.Report(fmt.Errorf("failed to retrieve subgroups for group %s: %w", group.FullPath, err))

			return
		}

		for _, subgroup := range subgroups {
			g.StoreGroup(subgroup, fetchOriginPath, mirrorMapping)

			if g.IsSource() || g.IsBig() {
				recursiveGroupWaitGroup.Add(1)

				go g.FetchAndProcessGroupRecursive(subgroup, fetchOriginPath, mirrorMapping, recursiveGroupWaitGroup)
			}
		}

		if resp.CurrentPage >= resp.TotalPages {
			break
		}

		fetchOpts.Page = resp.NextPage
	}
}
