package mirroring

import (
	"fmt"
	"sync"
	"time"

	"gitlab-sync/internal/utils"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

func (g *GitlabInstance) enableProjectMirrorPull(sourceProject *gitlab.Project, destinationProject *gitlab.Project, mirrorOptions *utils.MirroringOptions) error {
	zap.L().Sugar().Debugf("Enabling pull mirror for project %s", destinationProject.PathWithNamespace)
	_, _, err := g.Gitlab.Projects.ConfigureProjectPullMirror(destinationProject.ID, &gitlab.ConfigureProjectPullMirrorOptions{
		URL:                              &sourceProject.HTTPURLToRepo,
		OnlyMirrorProtectedBranches:      gitlab.Ptr(true),
		Enabled:                          gitlab.Ptr(true),
		MirrorOverwritesDivergedBranches: gitlab.Ptr(true),
		MirrorTriggerBuilds:              gitlab.Ptr(mirrorOptions.MirrorTriggerBuilds),
	})
	return err
}

func (g *GitlabInstance) addProjectToCICDCatalog(project *gitlab.Project) error {
	zap.L().Sugar().Debugf("Adding project %s to CI/CD catalog", project.PathWithNamespace)
	mutation := `
    mutation {
        catalogResourcesCreate(input: { projectPath: "%s" }) {
            errors
        }
    }`
	query := fmt.Sprintf(mutation, project.PathWithNamespace)
	_, err := g.GraphQLClient.SendRequest(&utils.GraphQLRequest{
		Query: query,
	}, "POST")
	return err
}

func (g *GitlabInstance) copyProjectAvatar(destinationGitlabInstance *GitlabInstance, destinationProject *gitlab.Project, sourceProject *gitlab.Project) error {
	zap.L().Sugar().Debugf("Checking if project avatar is already set for %s", destinationProject.PathWithNamespace)

	// Check if the destination project already has an avatar
	if destinationProject.AvatarURL != "" {
		zap.L().Sugar().Debugf("Project %s already has an avatar set, skipping.", destinationProject.PathWithNamespace)
		return nil
	}

	zap.L().Sugar().Debugf("Copying project avatar for %s", destinationProject.PathWithNamespace)

	// Download the source project avatar
	sourceProjectAvatar, _, err := g.Gitlab.Projects.DownloadAvatar(sourceProject.ID)
	if err != nil {
		return fmt.Errorf("failed to download avatar for project %s: %s", sourceProject.PathWithNamespace, err)
	}

	// Upload the avatar to the destination project
	filename := fmt.Sprintf("avatar-%d.png", time.Now().Unix())
	_, _, err = destinationGitlabInstance.Gitlab.Projects.UploadAvatar(destinationProject.ID, sourceProjectAvatar, filename)
	if err != nil {
		return fmt.Errorf("failed to upload avatar for project %s: %s", destinationProject.PathWithNamespace, err)
	}

	return nil
}

func (g *GitlabInstance) copyGroupAvatar(destinationGitlabInstance *GitlabInstance, destinationGroup *gitlab.Group, sourceGroup *gitlab.Group) error {
	zap.L().Sugar().Debugf("Checking if group avatar is already set for %s", destinationGroup.FullPath)

	// Check if the destination group already has an avatar
	if destinationGroup.AvatarURL != "" {
		zap.L().Sugar().Debugf("Group %s already has an avatar set, skipping.", destinationGroup.FullPath)
		return nil
	}

	zap.L().Sugar().Debugf("Copying group avatar for %s", destinationGroup.FullPath)

	// Download the source group avatar
	sourceGroupAvatar, _, err := g.Gitlab.Groups.DownloadAvatar(sourceGroup.ID)
	if err != nil {
		return fmt.Errorf("failed to download avatar for group %s: %s", sourceGroup.FullPath, err)
	}

	// Upload the avatar to the destination group
	filename := fmt.Sprintf("avatar-%d.png", time.Now().Unix())
	_, _, err = destinationGitlabInstance.Gitlab.Groups.UploadAvatar(destinationGroup.ID, sourceGroupAvatar, filename)
	if err != nil {
		return fmt.Errorf("failed to upload avatar for group %s: %s", destinationGroup.FullPath, err)
	}

	return nil
}

func (g *GitlabInstance) updateProjectFromSource(sourceGitlab *GitlabInstance, sourceProject *gitlab.Project, destinationProject *gitlab.Project, copyOptions *utils.MirroringOptions) error {
	wg := sync.WaitGroup{}
	maxErrors := 2
	if copyOptions.CI_CD_Catalog {
		maxErrors++
	}
	if copyOptions.MirrorReleases {
		maxErrors++
	}
	wg.Add(maxErrors)
	errorChan := make(chan error, maxErrors)

	go func() {
		defer wg.Done()

		zap.L().Sugar().Debugf("Enabling project %s mirror pull", destinationProject.PathWithNamespace)
		err := g.enableProjectMirrorPull(sourceProject, destinationProject, copyOptions)
		if err != nil {
			errorChan <- fmt.Errorf("Failed to enable project mirror pull for %s: %s", destinationProject.PathWithNamespace, err)
		}
	}()

	go func() {
		defer wg.Done()
		zap.L().Sugar().Debugf("Copying project %s avatar", destinationProject.PathWithNamespace)
		err := sourceGitlab.copyProjectAvatar(g, destinationProject, sourceProject)
		if err != nil {
			errorChan <- fmt.Errorf("Failed to copy project avatar for %s: %s", destinationProject.PathWithNamespace, err)
		}
	}()

	if copyOptions.CI_CD_Catalog {
		go func() {
			defer wg.Done()
			zap.L().Sugar().Debugf("Adding project %s to CI/CD catalog", destinationProject.PathWithNamespace)
			err := g.addProjectToCICDCatalog(destinationProject)
			if err != nil {
				errorChan <- fmt.Errorf("Failed to add project %s to CI/CD catalog: %s", destinationProject.PathWithNamespace, err)
			}
		}()
	}

	if copyOptions.MirrorReleases {
		go func() {
			defer wg.Done()
			zap.L().Sugar().Debugf("Copying project %s releases", destinationProject.PathWithNamespace)
			err := g.mirrorReleases(sourceProject, destinationProject)
			if err != nil {
				errorChan <- fmt.Errorf("Failed to copy project %s releases: %s", destinationProject.PathWithNamespace, err)
			}
		}()
	}

	wg.Wait()
	close(errorChan)
	return utils.MergeErrors(errorChan, 4)
}
