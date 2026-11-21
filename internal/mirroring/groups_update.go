package mirroring

import (
	"errors"
	"fmt"
	"sync"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.uber.org/zap"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

// updateGroupFromSource updates the destination group with settings from the source group.
// It copies the group avatar and updates the group attributes.
func (g *GitlabInstance) updateGroupFromSource(sourceGitlabInstance *GitlabInstance, sourceGroup, destinationGroup *gitlab.Group, copyOptions *mirrorcfg.MirroringOptions) {
	// Immediately capture pointers in local variables to avoid any late overrides
	srcGroup := sourceGroup
	dstGroup := destinationGroup
	cpOpts := copyOptions

	if srcGroup == nil || dstGroup == nil {
		helpers.Report(errors.New("source or destination group is nil"))

		return
	}

	waitGroup := sync.WaitGroup{}
	waitGroup.Add(groupUpdateTasks)

	go func(sg, dg *gitlab.Group, cp *mirrorcfg.MirroringOptions) {
		defer waitGroup.Done()

		helpers.Report(g.syncGroupAttributes(sg, dg, cp))
	}(srcGroup, dstGroup, cpOpts)

	go func(sg, dg *gitlab.Group) {
		defer waitGroup.Done()

		helpers.Report(sourceGitlabInstance.copyGroupAvatar(g, dg, sg))
	}(srcGroup, dstGroup)

	waitGroup.Wait()
}

// copyGroupAvatar copies the avatar from the source group to the destination group.
// It first checks if the destination group already has an avatar set. If not, it downloads the avatar from the source group
// and uploads it to the destination group.
// The avatar is saved with a unique filename based on the current timestamp.
// The function returns an error if any step fails, including downloading or uploading the avatar.
func (g *GitlabInstance) copyGroupAvatar(destinationGitlabInstance *GitlabInstance, destinationGroup, sourceGroup *gitlab.Group) error {
	zap.L().Debug("Checking if group avatar is already set", zap.String("group", destinationGroup.WebURL))

	// Check if the destination group already has an avatar
	if destinationGroup.AvatarURL != "" {
		zap.L().Debug("Group avatar already set", zap.String("group", destinationGroup.WebURL), zap.String("path", destinationGroup.AvatarURL))

		return nil
	}

	zap.L().Debug("Copying group avatar", zap.String(RoleSource, sourceGroup.WebURL), zap.String(RoleDestination, destinationGroup.WebURL))

	// Download the source group avatar
	sourceGroupAvatar, _, err := g.Gitlab.Groups.DownloadAvatar(sourceGroup.ID)
	if err != nil {
		return fmt.Errorf("failed to download avatar for group %s: %w", sourceGroup.WebURL, err)
	}

	// Upload the avatar to the destination group
	filename := fmt.Sprintf("avatar-%d.png", time.Now().Unix())

	_, _, err = destinationGitlabInstance.Gitlab.Groups.UploadAvatar(destinationGroup.ID, sourceGroupAvatar, filename)
	if err != nil {
		return fmt.Errorf("failed to upload avatar for group %s: %w", destinationGroup.WebURL, err)
	}

	return nil
}

// syncGroupAttributes updates the destination group with settings from the source group.
// It checks if any diverged group data exists and if so, it overwrites it.
func (g *GitlabInstance) syncGroupAttributes(sourceGroup, destinationGroup *gitlab.Group, copyOptions *mirrorcfg.MirroringOptions) error {
	zap.L().Debug("Checking if group requires attributes resync", zap.String(RoleSource, sourceGroup.FullPath), zap.String(RoleDestination, destinationGroup.FullPath))

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

	if helpers.Deref(copyOptions.Visibility, string(gitlab.PublicVisibility)) != string(destinationGroup.Visibility) {
		visibilityValue := mirrorcfg.ConvertVisibility(copyOptions.Visibility)
		gitlabEditOptions.Visibility = &visibilityValue
		missmatched = true
	}

	if missmatched {
		updatedGroup, _, err := g.Gitlab.Groups.UpdateGroup(destinationGroup.ID, gitlabEditOptions)
		if err != nil {
			return fmt.Errorf("failed to edit group %s: %w", destinationGroup.FullPath, err)
		}

		destinationGroup = updatedGroup

		zap.L().Debug("Group attributes resync completed", zap.String(RoleSource, sourceGroup.FullPath), zap.String(RoleDestination, destinationGroup.FullPath))
	} else {
		zap.L().Debug("Group attributes are already in sync, skipping", zap.String(RoleSource, sourceGroup.FullPath), zap.String(RoleDestination, destinationGroup.FullPath))
	}

	return nil
}

// ClaimOwnershipToGroup adds the authenticated user as an owner to the specified group.
// It uses the GitLab API to add the user as a group member with owner access level.
func (g *GitlabInstance) ClaimOwnershipToGroup(group *gitlab.Group) error {
	zap.L().Debug("Claiming ownership of group", zap.String("group", group.FullPath), zap.Int64("userID", g.UserID))

	_, _, err := g.Gitlab.GroupMembers.AddGroupMember(group.ID, &gitlab.AddGroupMemberOptions{
		UserID:      &g.UserID,
		AccessLevel: new(gitlab.AccessLevelValue(ownerAccessLevel)),
	})
	if err != nil {
		return fmt.Errorf("failed to add user as owner to group %s: %w", group.FullPath, err)
	}

	zap.L().Info("Successfully claimed ownership of group", zap.String("group", group.FullPath))

	return nil
}
