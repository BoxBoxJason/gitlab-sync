package mirroring

import (
	"fmt"
	"gitlab-sync/internal/utils"
	"gitlab-sync/pkg/helpers"
	"path/filepath"
	"sync"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

// ============================================================ //
//                 GROUP CREATION FUNCTIONS                     //
// ============================================================ //

// CreateGroups creates GitLab groups in the destination GitLab instance based on the mirror mapping.
// It retrieves the source group path for each destination group and creates the group in the destination instance.
// The function also handles the copying of group avatars from the source to the destination instance.
func (destinationGitlab *GitlabInstance) CreateGroups(sourceGitlab *GitlabInstance, mirrorMapping *utils.MirrorMapping) []error {
	zap.L().Info("Creating groups in GitLab Instance", zap.String(ROLE, ROLE_DESTINATION))

	// Reverse the mirror mapping to get the source group path for each destination group
	reversedMirrorMap, destinationGroupPaths := sourceGitlab.reverseGroupMirrorMap(mirrorMapping)

	errorChan := make(chan []error, len(destinationGroupPaths))
	// Iterate over the groups in alphabetical order (little hack to ensure parent groups are created before children)
	for _, destinationGroupPath := range destinationGroupPaths {
		_, err := destinationGitlab.CreateGroup(destinationGroupPath, sourceGitlab, mirrorMapping, &reversedMirrorMap)
		if err != nil {
			errorChan <- err
		}
	}
	close(errorChan)
	return helpers.MergeErrors(errorChan)
}

// CreateGroup creates a GitLab group in the destination GitLab instance based on the source group and mirror mapping.
// It checks if the group already exists in the destination instance and creates it if not.
// The function also handles the copying of group avatars from the source to the destination instance.
func (destinationGitlab *GitlabInstance) CreateGroup(destinationGroupPath string, sourceGitlab *GitlabInstance, mirrorMapping *utils.MirrorMapping, reversedMirrorMap *map[string]string) (*gitlab.Group, []error) {
	// Retrieve the corresponding source group path
	sourceGroupPath := (*reversedMirrorMap)[destinationGroupPath]
	zap.L().Debug("Mirroring group", zap.String(ROLE_SOURCE, sourceGroupPath), zap.String(ROLE_DESTINATION, destinationGroupPath))

	sourceGroup := sourceGitlab.Groups[sourceGroupPath]
	if sourceGroup == nil {
		return nil, []error{fmt.Errorf("group %s not found in destination GitLab instance (internal error, please review script)", sourceGroupPath)}
	}

	// Retrieve the corresponding group creation options from the mirror mapping
	groupCreationOptions, ok := mirrorMapping.GetGroup(sourceGroupPath)
	if !ok {
		return nil, []error{fmt.Errorf("source group %s not found in mirror mapping (internal error, please review script)", sourceGroupPath)}
	}

	// Check if the group already exists in the destination GitLab instance
	destinationGroup := destinationGitlab.GetGroup(destinationGroupPath)
	var err error
	if destinationGroup == nil {
		zap.L().Debug("Group not found, creating new group in GitLab Instance", zap.String("group", destinationGroupPath), zap.String(ROLE, ROLE_DESTINATION))
		destinationGroup, err = destinationGitlab.CreateGroupFromSource(sourceGroup, groupCreationOptions)
		if err != nil {
			return nil, []error{fmt.Errorf("failed to create group %s in destination GitLab instance: %s", destinationGroupPath, err)}
		} else {
			// Copy the group avatar from the source to the destination instance
			errArray := sourceGitlab.updateGroupFromSource(destinationGitlab, destinationGroup, sourceGroup, groupCreationOptions)
			if errArray != nil {
				return destinationGroup, errArray
			}
		}
	}
	zap.L().Debug("Group already exists, skipping creation", zap.String("group", destinationGroupPath))
	return destinationGroup, nil
}

// CreateGroupFromSource creates a GitLab group in the destination GitLab instance based on the source group.
// It sets the group name, path, description, visibility, and default branch based on the source group.
// The function also handles the setting of the parent ID for the group.
// It returns the created group or an error if the creation fails.
func (g *GitlabInstance) CreateGroupFromSource(sourceGroup *gitlab.Group, copyOptions *utils.MirroringOptions) (*gitlab.Group, error) {
	groupCreationArgs := &gitlab.CreateGroupOptions{
		Name:          &sourceGroup.Name,
		Path:          &sourceGroup.Path,
		Description:   &sourceGroup.Description,
		Visibility:    &sourceGroup.Visibility,
		DefaultBranch: &sourceGroup.DefaultBranch,
	}

	// Retrieve the parent namespace ID for the group
	// This is used to set the parent ID for the group
	zap.L().Debug("Retrieving group namespace ID", zap.String(ROLE, ROLE_DESTINATION), zap.String(ROLE_DESTINATION, copyOptions.DestinationPath))
	parentGroupID, err := g.GetParentNamespaceID(copyOptions.DestinationPath)
	if err != nil {
		return nil, err
	} else if parentGroupID >= 0 {
		groupCreationArgs.ParentID = &parentGroupID
	}

	// Create the group in the destination GitLab instance
	zap.L().Debug("Creating group in GitLab Instance", zap.String(ROLE, ROLE_DESTINATION), zap.String(ROLE_DESTINATION, copyOptions.DestinationPath))
	destinationGroup, _, err := g.Gitlab.Groups.CreateGroup(groupCreationArgs)
	if err == nil {
		zap.L().Info("Group created", zap.String("group", destinationGroup.WebURL))
		g.AddGroup(destinationGroup)

		// Claim ownership of the created group
		if err := g.ClaimOwnershipToGroup(destinationGroup); err != nil {
			zap.L().Warn("Failed to claim ownership of group", zap.String("group", destinationGroup.FullPath), zap.Error(err))
		}
	}

	return destinationGroup, err
}

// ============================================================ //
//                 GROUP RETRIEVAL FUNCTIONS                    //
// ============================================================ //

// FetchAndProcessGroups retrieves all groups that match the filters from the GitLab instance and stores them in the instance cache.
// It also updates the mirror mapping with the corresponding group creation options.
//
// The function is run in a goroutine for each group, and a wait group is used to wait for all goroutines to finish.
func (g *GitlabInstance) FetchAndProcessGroups(groupFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) []error {
	zap.L().Debug("Fetching and processing groups from GitLab instance", zap.String(ROLE, g.Role), zap.Int("groups", len(*groupFilters)))
	if !g.IsBig() {
		return []error{g.FetchAndProcessGroupsSmallInstance(groupFilters, mirrorMapping)}
	}
	return g.FetchAndProcessGroupsLargeInstance(groupFilters, mirrorMapping)
}

// StoreGroup stores the group in the Gitlab instance groups cache
// and updates the mirror mapping with the corresponding group creation options.
func (g *GitlabInstance) StoreGroup(group *gitlab.Group, parentGroupPath string, mirrorMapping *utils.MirrorMapping) {
	if group != nil {
		// Add the group to the gitlab instance groups cache
		g.AddGroup(group)

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
				MirrorIssues:        groupCreationOptions.MirrorIssues,
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

// FetchAndProcessGroupsSmallInstance retrieves all groups that match the filters from the GitLab instance and stores them in the instance cache.
// It also updates the mirror mapping with the corresponding group creation options.
func (g *GitlabInstance) FetchAndProcessGroupsSmallInstance(groupFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) error {
	allGroups, err := g.FetchAllGroupsSmallInstance()
	if err != nil {
		return err
	}

	g.ProcessGroupsSmallInstance(allGroups, groupFilters, mirrorMapping)

	zap.L().Debug("Found matching groups in the GitLab instance", zap.String(ROLE, g.Role), zap.Int("groups", len(g.Groups)))

	return nil
}

// FetchAllGroupsSmallInstance retrieves all groups from the small GitLab instance
func (g *GitlabInstance) FetchAllGroupsSmallInstance() ([]*gitlab.Group, error) {
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

// FetchAllGroupsSmallInstance retrieves all groups from the small GitLab instance
// It uses pagination to fetch all groups in batches of 100. Queries all groups of the instance
func (g *GitlabInstance) ProcessGroupsSmallInstance(allGroups []*gitlab.Group, groupFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) {
	zap.L().Debug("Processing groups from GitLab instance", zap.String(INSTANCE_SIZE, g.InstanceSize), zap.String(ROLE, g.Role), zap.Int("groups", len(allGroups)))

	// Create a wait group to wait for all goroutines to finish
	var wg sync.WaitGroup

	for _, group := range allGroups {
		wg.Add(1)

		go func(group *gitlab.Group) {
			defer wg.Done()

			groupPath, matches := helpers.MatchPathAgainstFilters(group.FullPath, nil, groupFilters)
			if matches {
				g.StoreGroup(group, groupPath, mirrorMapping)
			}
		}(group)
	}

	wg.Wait()
}

// ===========================================================================
//                         LARGE INSTANCE FUNCTIONS                         //
// ===========================================================================

// FetchAndProcessGroupsLargeInstance retrieves all groups that match the filters from the GitLab instance and stores them in the instance cache.
// It also updates the mirror mapping with the corresponding group creation options.
// It uses goroutines to fetch groups and their projects concurrently.
func (g *GitlabInstance) FetchAndProcessGroupsLargeInstance(groupFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) []error {
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
		go g.FetchAndProcessGroupRecursive(groupPath, groupPath, mirrorMapping, errChan, &wg)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(errChan)
	return helpers.MergeErrors(errs)
}

// FetchAndProcessGroupRecursive fetches a group and its projects recursively
// It uses a wait group to ensure that all goroutines finish befopidpre returning
// It sends the fetched group to the allGroupsChannel and the projects to the allProjectsChanel
//
// gid can be either an int, a string or a *gitlab.Group
func (g *GitlabInstance) FetchAndProcessGroupRecursive(gid any, fetchOriginPath string, mirrorMapping *utils.MirrorMapping, errChan chan error, wg *sync.WaitGroup) {
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
		g.StoreGroup(group, fetchOriginPath, mirrorMapping)
		if g.IsSource() || g.IsBig() {
			wg.Add(2)
			// Fetch the projects of the group
			go g.FetchAndProcessGroupProjects(group, fetchOriginPath, mirrorMapping, errChan, wg)
			// Fetch the subgroups of the group
			go g.FetchAndProcessGroupSubgroups(group, fetchOriginPath, mirrorMapping, errChan, wg)
		}
	}
}

// FetchAndProcessGroupSubgroups retrieves all subgroups of a group
// and processes them to store in the instance cache.
func (g *GitlabInstance) FetchAndProcessGroupSubgroups(group *gitlab.Group, fetchOriginPath string, mirrorMapping *utils.MirrorMapping, errChan chan error, wg *sync.WaitGroup) {
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
			g.StoreGroup(subgroup, fetchOriginPath, mirrorMapping)
			if g.IsSource() || g.IsBig() {
				wg.Add(1)
				go g.FetchAndProcessGroupRecursive(subgroup, fetchOriginPath, mirrorMapping, errChan, wg)
			}
		}
		if resp.CurrentPage >= resp.TotalPages {
			break
		}
		fetchOpts.Page = resp.NextPage
	}
}

// ===========================================================================
//                           GROUPS PUT FUNCTIONS                           //
// ===========================================================================

// updateGroupFromSource updates the destination group with settings from the source group.
// It copies the group avatar and updates the group attributes.
func (destinationGitlabInstance *GitlabInstance) updateGroupFromSource(sourceGitlabInstance *GitlabInstance, sourceGroup *gitlab.Group, destinationGroup *gitlab.Group, copyOptions *utils.MirroringOptions) []error {
	// Immediately capture pointers in local variables to avoid any late overrides
	srcGroup := sourceGroup
	dstGroup := destinationGroup
	cpOpts := copyOptions

	if srcGroup == nil || dstGroup == nil {
		return []error{fmt.Errorf("source or destination group is nil")}
	}

	wg := sync.WaitGroup{}
	maxErrors := 2
	wg.Add(maxErrors)
	errorChan := make(chan error, maxErrors)

	go func(sg *gitlab.Group, dg *gitlab.Group, cp *utils.MirroringOptions) {
		defer wg.Done()
		errorChan <- destinationGitlabInstance.syncGroupAttributes(sg, dg, cp)
	}(srcGroup, dstGroup, cpOpts)

	go func(sg *gitlab.Group, dg *gitlab.Group) {
		defer wg.Done()
		errorChan <- sourceGitlabInstance.copyGroupAvatar(destinationGitlabInstance, dg, sg)
	}(srcGroup, dstGroup)

	wg.Wait()
	close(errorChan)

	return helpers.MergeErrors(errorChan)
}

// copyGroupAvatar copies the avatar from the source group to the destination group.
// It first checks if the destination group already has an avatar set. If not, it downloads the avatar from the source group
// and uploads it to the destination group.
// The avatar is saved with a unique filename based on the current timestamp.
// The function returns an error if any step fails, including downloading or uploading the avatar.
func (sourceGitlabInstance *GitlabInstance) copyGroupAvatar(destinationGitlabInstance *GitlabInstance, destinationGroup *gitlab.Group, sourceGroup *gitlab.Group) error {
	zap.L().Debug("Checking if group avatar is already set", zap.String("group", destinationGroup.WebURL))

	// Check if the destination group already has an avatar
	if destinationGroup.AvatarURL != "" {
		zap.L().Debug("Group avatar already set", zap.String("group", destinationGroup.WebURL), zap.String("path", destinationGroup.AvatarURL))
		return nil
	}

	zap.L().Debug("Copying group avatar", zap.String(ROLE_SOURCE, sourceGroup.WebURL), zap.String(ROLE_DESTINATION, destinationGroup.WebURL))

	// Download the source group avatar
	sourceGroupAvatar, _, err := sourceGitlabInstance.Gitlab.Groups.DownloadAvatar(sourceGroup.ID)
	if err != nil {
		return fmt.Errorf("failed to download avatar for group %s: %s", sourceGroup.WebURL, err)
	}

	// Upload the avatar to the destination group
	filename := fmt.Sprintf("avatar-%d.png", time.Now().Unix())
	_, _, err = destinationGitlabInstance.Gitlab.Groups.UploadAvatar(destinationGroup.ID, sourceGroupAvatar, filename)
	if err != nil {
		return fmt.Errorf("failed to upload avatar for group %s: %s", destinationGroup.WebURL, err)
	}

	return nil
}

// syncGroupAttributes updates the destination group with settings from the source group.
// It checks if any diverged group data exists and if so, it overwrites it.
func (destinationGitlabInstance *GitlabInstance) syncGroupAttributes(sourceGroup *gitlab.Group, destinationGroup *gitlab.Group, copyOptions *utils.MirroringOptions) error {
	zap.L().Debug("Checking if group requires attributes resync", zap.String(ROLE_SOURCE, sourceGroup.FullPath), zap.String(ROLE_DESTINATION, destinationGroup.FullPath))
	gitlabEditOptions := &gitlab.UpdateGroupOptions{}
	missmatched := false
	if sourceGroup.Name != destinationGroup.Name {
		gitlabEditOptions.Name = &sourceGroup.Name
		missmatched = true
	}
	if sourceGroup.Description != destinationGroup.Description {
		gitlabEditOptions.Description = &sourceGroup.Description
		missmatched = true
	}
	if copyOptions.Visibility != string(destinationGroup.Visibility) {
		visibilityValue := utils.ConvertVisibility(copyOptions.Visibility)
		gitlabEditOptions.Visibility = &visibilityValue
		missmatched = true
	}

	if missmatched {
		destinationGroup, _, err := destinationGitlabInstance.Gitlab.Groups.UpdateGroup(destinationGroup.ID, gitlabEditOptions)
		if err != nil {
			return fmt.Errorf("failed to edit group %s: %s", destinationGroup.FullPath, err)
		}
		zap.L().Debug("Group attributes resync completed", zap.String(ROLE_SOURCE, sourceGroup.FullPath), zap.String(ROLE_DESTINATION, destinationGroup.FullPath))
	} else {
		zap.L().Debug("Group attributes are already in sync, skipping", zap.String(ROLE_SOURCE, sourceGroup.FullPath), zap.String(ROLE_DESTINATION, destinationGroup.FullPath))
	}
	return nil
}

// ClaimOwnershipToGroup adds the authenticated user as an owner to the specified group.
// It uses the GitLab API to add the user as a group member with owner access level.
func (g *GitlabInstance) ClaimOwnershipToGroup(group *gitlab.Group) error {
	zap.L().Debug("Claiming ownership of group", zap.String("group", group.FullPath), zap.Int("userID", g.UserID))

	_, _, err := g.Gitlab.GroupMembers.AddGroupMember(group.ID, &gitlab.AddGroupMemberOptions{
		UserID:      &g.UserID,
		AccessLevel: gitlab.Ptr(gitlab.AccessLevelValue(50)),
	})
	if err != nil {
		return fmt.Errorf("failed to add user as owner to group %s: %w", group.FullPath, err)
	}

	zap.L().Info("Successfully claimed ownership of group", zap.String("group", group.FullPath))
	return nil
}
