package mirroring

import (
	"fmt"
	"sync"
	"time"

	"gitlab-sync/internal/utils"
	"gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

// ===========================================================================
//                         PROJECTS PUT FUNCTIONS                         //
// ===========================================================================

// updateProjectFromSource updates the destination project with settings from the source project.
// It enables the project mirror pull, copies the project avatar, and optionally adds the project to the CI/CD catalog.
// It also mirrors releases if the option is set.
// The function uses goroutines to perform these tasks concurrently and waits for all of them to finish.
func (destinationGitlabInstance *GitlabInstance) updateProjectFromSource(sourceGitlabInstance *GitlabInstance, sourceProject *gitlab.Project, destinationProject *gitlab.Project, copyOptions *utils.MirroringOptions) []error {
	// Immediately capture pointers in local variables to avoid any late overrides
	srcProj := sourceProject
	dstProj := destinationProject
	if srcProj == nil || dstProj == nil {
		return []error{fmt.Errorf("source or destination project is nil")}
	}

	wg := sync.WaitGroup{}
	maxErrors := 3
	wg.Add(maxErrors)
	errorChan := make(chan error, maxErrors)

	go func(sp *gitlab.Project, dp *gitlab.Project) {
		defer wg.Done()
		errorChan <- destinationGitlabInstance.syncProjectAttributes(sp, dp, copyOptions)
	}(srcProj, dstProj)

	go func(sp *gitlab.Project, dp *gitlab.Project) {
		defer wg.Done()
		errorChan <- destinationGitlabInstance.mirrorProjectGit(sourceGitlabInstance, sp, dp, copyOptions)
	}(srcProj, dstProj)

	go func(sp *gitlab.Project, dp *gitlab.Project) {
		defer wg.Done()
		errorChan <- sourceGitlabInstance.copyProjectAvatar(destinationGitlabInstance, dp, sp)
	}(srcProj, dstProj)

	if copyOptions.CI_CD_Catalog {
		wg.Add(1)
		go func(dp *gitlab.Project) {
			defer wg.Done()
			errorChan <- destinationGitlabInstance.addProjectToCICDCatalog(dp)
		}(dstProj)
	}

	// Wait for git duplication to finish
	wg.Wait()

	allErrors := []error{}
	if copyOptions.MirrorReleases {
		wg.Add(1)
		go func(sp *gitlab.Project, dp *gitlab.Project) {
			defer wg.Done()
			allErrors = destinationGitlabInstance.mirrorReleases(sourceGitlabInstance, sp, dp)
		}(srcProj, dstProj)
	}

	wg.Wait()
	close(errorChan)

	for err := range errorChan {
		if err != nil {
			allErrors = append(allErrors, err)
		}
	}
	return allErrors
}

// syncProjectAttributes updates the destination project with settings from the source project.
// It checks if any diverged project data exists and if so, it overwrites it.
func (destinationGitlabInstance *GitlabInstance) syncProjectAttributes(sourceProject *gitlab.Project, destinationProject *gitlab.Project, copyOptions *utils.MirroringOptions) error {
	zap.L().Debug("Checking if project requires attributes resync", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))
	gitlabEditOptions := &gitlab.EditProjectOptions{}
	missmatched := false
	if sourceProject.Name != destinationProject.Name {
		gitlabEditOptions.Name = &sourceProject.Name
		missmatched = true
	}
	if sourceProject.Description != destinationProject.Description {
		gitlabEditOptions.Description = &sourceProject.Description
		missmatched = true
	}
	if sourceProject.DefaultBranch != destinationProject.DefaultBranch {
		gitlabEditOptions.DefaultBranch = &sourceProject.DefaultBranch
		missmatched = true
	}
	if !utils.StringArraysMatchValues(sourceProject.Topics, destinationProject.Topics) {
		gitlabEditOptions.Topics = &sourceProject.Topics
		missmatched = true
	}
	if copyOptions.MirrorTriggerBuilds != destinationProject.MirrorTriggerBuilds {
		gitlabEditOptions.MirrorTriggerBuilds = &copyOptions.MirrorTriggerBuilds
		missmatched = true
	}
	if !destinationProject.MirrorOverwritesDivergedBranches {
		gitlabEditOptions.MirrorOverwritesDivergedBranches = gitlab.Ptr(true)
		missmatched = true
	}
	if !destinationProject.Mirror {
		gitlabEditOptions.Mirror = gitlab.Ptr(true)
		missmatched = true
	}
	if copyOptions.Visibility != string(destinationProject.Visibility) {
		visibilityValue := utils.ConvertVisibility(copyOptions.Visibility)
		gitlabEditOptions.Visibility = &visibilityValue
		missmatched = true
	}

	if missmatched {
		_, _, err := destinationGitlabInstance.Gitlab.Projects.EditProject(destinationProject.ID, gitlabEditOptions)
		if err != nil {
			return fmt.Errorf("failed to edit project %s: %s", destinationProject.HTTPURLToRepo, err)
		}
		zap.L().Debug("Project attributes resync completed", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))
	} else {
		zap.L().Debug("Project attributes are already in sync, skipping", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))
	}
	return nil
}

func (destinationGitlabInstance *GitlabInstance) mirrorProjectGit(sourceGitlabInstance *GitlabInstance, sourceProject *gitlab.Project, destinationProject *gitlab.Project, mirrorOptions *utils.MirroringOptions) error {
	if destinationGitlabInstance.PullMirrorAvailable {
		return destinationGitlabInstance.enableProjectMirrorPull(sourceProject, destinationProject, mirrorOptions)
	}
	return helpers.MirrorRepo(sourceProject.HTTPURLToRepo, destinationProject.HTTPURLToRepo, sourceGitlabInstance.GitAuth, destinationGitlabInstance.GitAuth)
}

// enableProjectMirrorPull enables the pull mirror for a project in the destination GitLab instance.
// It sets the source project URL, enables mirroring, and configures other options like triggering builds and overwriting diverged branches.
func (g *GitlabInstance) enableProjectMirrorPull(sourceProject *gitlab.Project, destinationProject *gitlab.Project, mirrorOptions *utils.MirroringOptions) error {
	zap.L().Debug("Enabling project mirror pull", zap.String("sourceProject", sourceProject.HTTPURLToRepo), zap.String("destinationProject", destinationProject.HTTPURLToRepo))
	_, _, err := g.Gitlab.Projects.ConfigureProjectPullMirror(destinationProject.ID, &gitlab.ConfigureProjectPullMirrorOptions{
		URL:                              &sourceProject.HTTPURLToRepo,
		OnlyMirrorProtectedBranches:      gitlab.Ptr(true),
		Enabled:                          gitlab.Ptr(true),
		MirrorOverwritesDivergedBranches: gitlab.Ptr(true),
		MirrorTriggerBuilds:              gitlab.Ptr(mirrorOptions.MirrorTriggerBuilds),
	})
	return err
}

// copyProjectAvatar copies the avatar from the source project to the destination project.
// It first checks if the destination project already has an avatar set. If not, it downloads the avatar from the source project
// and uploads it to the destination project.
// The avatar is saved with a unique filename based on the current timestamp.
// The function returns an error if any step fails, including downloading or uploading the avatar.
func (sourceGitlabInstance *GitlabInstance) copyProjectAvatar(destinationGitlabInstance *GitlabInstance, destinationProject *gitlab.Project, sourceProject *gitlab.Project) error {
	zap.L().Debug("Checking if project avatar is already set", zap.String("project", destinationProject.HTTPURLToRepo))

	// Check if the destination project already has an avatar
	if destinationProject.AvatarURL != "" {
		zap.L().Debug("Project already has an avatar set, skipping.", zap.String("project", destinationProject.HTTPURLToRepo), zap.String("path", destinationProject.AvatarURL))
		return nil
	}

	zap.L().Debug("Copying project avatar", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))

	// Download the source project avatar
	sourceProjectAvatar, _, err := sourceGitlabInstance.Gitlab.Projects.DownloadAvatar(sourceProject.ID)
	if err != nil {
		return fmt.Errorf("failed to download avatar for project %s: %s", sourceProject.HTTPURLToRepo, err)
	}

	// Upload the avatar to the destination project
	_, _, err = destinationGitlabInstance.Gitlab.Projects.UploadAvatar(destinationProject.ID, sourceProjectAvatar, fmt.Sprintf("avatar-%d.png", time.Now().Unix()))
	if err != nil {
		return fmt.Errorf("failed to upload avatar for project %s: %s", destinationProject.HTTPURLToRepo, err)
	}

	return nil
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
