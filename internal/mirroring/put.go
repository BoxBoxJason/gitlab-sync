package mirroring

import (
	"fmt"
	"sync"
	"time"

	"gitlab-sync/internal/utils"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

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

// addProjectToCICDCatalog adds a project to the CI/CD catalog in the destination GitLab instance.
// It uses a GraphQL mutation to create the catalog resource for the project.
//
// NOTE: This function needs to be changed as soon as the gitlab SDK supports the GraphQL API.
func (g *GitlabInstance) addProjectToCICDCatalog(project *gitlab.Project) error {
	zap.L().Debug("Adding project to CI/CD catalog", zap.String("project", project.HTTPURLToRepo))
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

	zap.L().Debug("Copying project avatar", zap.String("source", sourceProject.HTTPURLToRepo), zap.String("destination", destinationProject.HTTPURLToRepo))

	// Download the source project avatar
	sourceProjectAvatar, _, err := sourceGitlabInstance.Gitlab.Projects.DownloadAvatar(sourceProject.ID)
	if err != nil {
		return fmt.Errorf("failed to download avatar for project %s: %s", sourceProject.HTTPURLToRepo, err)
	}

	// Upload the avatar to the destination project
	filename := fmt.Sprintf("avatar-%d.png", time.Now().Unix())
	_, _, err = destinationGitlabInstance.Gitlab.Projects.UploadAvatar(destinationProject.ID, sourceProjectAvatar, filename)
	if err != nil {
		return fmt.Errorf("failed to upload avatar for project %s: %s", destinationProject.HTTPURLToRepo, err)
	}

	return nil
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

	zap.L().Debug("Copying group avatar", zap.String("source", sourceGroup.WebURL), zap.String("destination", destinationGroup.WebURL))

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

// updateProjectFromSource updates the destination project with settings from the source project.
// It enables the project mirror pull, copies the project avatar, and optionally adds the project to the CI/CD catalog.
// It also mirrors releases if the option is set.
// The function uses goroutines to perform these tasks concurrently and waits for all of them to finish.
func (destinationGitlabInstance *GitlabInstance) updateProjectFromSource(sourceGitlabInstance *GitlabInstance, sourceProject *gitlab.Project, destinationProject *gitlab.Project, copyOptions *utils.MirroringOptions) error {
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

		zap.L().Debug("Enabling project mirror pull", zap.String("source", sourceProject.HTTPURLToRepo), zap.String("destination", destinationProject.HTTPURLToRepo))
		err := destinationGitlabInstance.enableProjectMirrorPull(sourceProject, destinationProject, copyOptions)
		if err != nil {
			errorChan <- fmt.Errorf("failed to enable project mirror pull for %s: %s", destinationProject.HTTPURLToRepo, err)
		}
	}()

	go func() {
		defer wg.Done()
		zap.L().Debug("Copying project avatar", zap.String("source", sourceProject.HTTPURLToRepo), zap.String("destination", destinationProject.HTTPURLToRepo))
		err := sourceGitlabInstance.copyProjectAvatar(destinationGitlabInstance, destinationProject, sourceProject)
		if err != nil {
			errorChan <- fmt.Errorf("failed to copy project avatar for %s: %s", destinationProject.HTTPURLToRepo, err)
		}
	}()

	if copyOptions.CI_CD_Catalog {
		go func() {
			defer wg.Done()
			err := destinationGitlabInstance.addProjectToCICDCatalog(destinationProject)
			if err != nil {
				errorChan <- fmt.Errorf("failed to add project %s to CI/CD catalog: %s", destinationProject.HTTPURLToRepo, err)
			}
		}()
	}

	if copyOptions.MirrorReleases {
		go func() {
			defer wg.Done()
			err := destinationGitlabInstance.mirrorReleases(sourceProject, destinationProject)
			if err != nil {
				errorChan <- fmt.Errorf("failed to copy project %s releases: %s", destinationProject.HTTPURLToRepo, err)
			}
		}()
	}

	wg.Wait()
	close(errorChan)
	return utils.MergeErrors(errorChan, 4)
}
