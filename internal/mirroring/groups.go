package mirroring

import (
	"fmt"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.uber.org/zap"
)

const (
	groupsPerPage       = 100
	subgroupWorkerCount = 2
	ownerAccessLevel    = 50
	groupUpdateTasks    = 2
)

// ============================================================ //
//                 GROUP CREATION FUNCTIONS                     //
// ============================================================ //

// CreateGroups creates GitLab groups in the destination GitLab instance based on the mirror mapping.
// It retrieves the source group path for each destination group and creates the group in the destination instance.
// The function also handles the copying of group avatars from the source to the destination instance.
func (g *GitlabInstance) CreateGroups(sourceGitlab *GitlabInstance, mirrorMapping *mirrorcfg.MirrorMapping) {
	zap.L().Info("Creating groups in GitLab Instance", zap.String(RoleKey, RoleDestination))

	// Reverse the mirror mapping to get the source group path for each destination group
	reversed := sourceGitlab.reverseGroupMirrorMap(mirrorMapping)

	// Iterate over the groups in alphabetical order (little hack to ensure parent groups are created before children)
	for _, destinationGroupPath := range reversed.destinationPaths {
		g.CreateGroup(destinationGroupPath, sourceGitlab, mirrorMapping, reversed.sourceByDestination)
	}
}

// CreateGroup creates a GitLab group in the destination GitLab instance based on the source group and mirror mapping.
// It checks if the group already exists in the destination instance and creates it if not.
// The function also handles the copying of group avatars from the source to the destination instance.
func (g *GitlabInstance) CreateGroup(destinationGroupPath string, sourceGitlab *GitlabInstance, mirrorMapping *mirrorcfg.MirrorMapping, reversedMirrorMap map[string]string) *gitlab.Group {
	// Retrieve the corresponding source group path
	sourceGroupPath := reversedMirrorMap[destinationGroupPath]
	zap.L().Debug("Mirroring group", zap.String(RoleSource, sourceGroupPath), zap.String(RoleDestination, destinationGroupPath))

	sourceGroup := sourceGitlab.GetGroup(sourceGroupPath)
	if sourceGroup == nil {
		helpers.ReportBlocking(fmt.Errorf("group %s not found in destination GitLab instance (internal error, please review script)", sourceGroupPath))

		return nil
	}

	// Retrieve the corresponding group creation options from the mirror mapping
	groupCreationOptions, ok := mirrorMapping.GetGroup(sourceGroupPath)
	if !ok {
		helpers.ReportBlocking(fmt.Errorf("source group %s not found in mirror mapping (internal error, please review script)", sourceGroupPath))

		return nil
	}

	// Check if the group already exists in the destination GitLab instance
	destinationGroup := g.GetGroup(destinationGroupPath)

	if destinationGroup == nil {
		zap.L().Debug("Group not found, creating new group in GitLab Instance", zap.String("group", destinationGroupPath), zap.String(RoleKey, RoleDestination))

		createdGroup, err := g.CreateGroupFromSource(sourceGroup, groupCreationOptions)
		if err != nil {
			helpers.ReportBlocking(fmt.Errorf("failed to create group %s in destination GitLab instance: %w", destinationGroupPath, err))

			return nil
		}

		destinationGroup = createdGroup

		// Copy the group avatar from the source to the destination instance
		sourceGitlab.updateGroupFromSource(g, destinationGroup, sourceGroup, groupCreationOptions)

		return destinationGroup
	}

	zap.L().Debug("Group already exists, skipping creation", zap.String("group", destinationGroupPath))

	return destinationGroup
}

// CreateGroupFromSource creates a GitLab group in the destination GitLab instance based on the source group.
// It sets the group name, path, description, visibility, and default branch based on the source group.
// The function also handles the setting of the parent ID for the group.
// It returns the created group or an error if the creation fails.
func (g *GitlabInstance) CreateGroupFromSource(sourceGroup *gitlab.Group, copyOptions *mirrorcfg.MirroringOptions) (*gitlab.Group, error) {
	groupCreationArgs := &gitlab.CreateGroupOptions{
		Name:          &sourceGroup.Name,
		Path:          &sourceGroup.Path,
		Description:   &sourceGroup.Description,
		Visibility:    &sourceGroup.Visibility,
		DefaultBranch: &sourceGroup.DefaultBranch,
	}

	// Retrieve the parent namespace ID for the group
	// This is used to set the parent ID for the group
	zap.L().Debug("Retrieving group namespace ID", zap.String(RoleKey, RoleDestination), zap.String(RoleDestination, copyOptions.DestinationPath))

	parentGroupID, err := g.EnsureParentNamespaceID(copyOptions.DestinationPath, sourceGroup.Visibility)
	if err != nil {
		return nil, err
	}

	if parentGroupID >= 0 {
		groupCreationArgs.ParentID = &parentGroupID
	}

	// Create the group in the destination GitLab instance
	zap.L().Debug("Creating group in GitLab Instance", zap.String(RoleKey, RoleDestination), zap.String(RoleDestination, copyOptions.DestinationPath))

	destinationGroup, _, err := g.Gitlab.Groups.CreateGroup(groupCreationArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to create group %s: %w", copyOptions.DestinationPath, err)
	}

	zap.L().Info("Group created", zap.String("group", destinationGroup.WebURL))
	g.AddGroup(destinationGroup)

	// Claim ownership of the created group
	if copyOptions.ClaimOwnership != nil && *copyOptions.ClaimOwnership {
		ownershipErr := g.ClaimOwnershipToGroup(destinationGroup)
		if ownershipErr != nil {
			zap.L().Warn("Failed to claim ownership of group", zap.String("group", destinationGroup.FullPath), zap.Error(ownershipErr))
		}
	}

	return destinationGroup, nil
}

// ============================================================ //
//                 GROUP RETRIEVAL FUNCTIONS                    //
// ============================================================ //

// ===========================================================================
//                         SMALL INSTANCE FUNCTIONS                         //
// ===========================================================================

// ===========================================================================
//                         LARGE INSTANCE FUNCTIONS                         //
// ===========================================================================

// ===========================================================================
//                           GROUPS PUT FUNCTIONS                           //
// ===========================================================================
