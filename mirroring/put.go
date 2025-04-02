package mirroring

import (
	"fmt"
	"sync"
	"time"

	"gitlab-sync/utils"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func (g *GitlabInstance) enableProjectMirrorPull(sourceProject *gitlab.Project, destinationProject *gitlab.Project) error {
	utils.LogVerbosef("Enabling pull mirror for project %s", destinationProject.PathWithNamespace)
	_, _, err := g.Gitlab.Projects.ConfigureProjectPullMirror(destinationProject.ID, &gitlab.ConfigureProjectPullMirrorOptions{
		URL:                              &sourceProject.HTTPURLToRepo,
		OnlyMirrorProtectedBranches:      gitlab.Ptr(true),
		Enabled:                          gitlab.Ptr(true),
		MirrorOverwritesDivergedBranches: gitlab.Ptr(true),
		MirrorTriggerBuilds:              gitlab.Ptr(true),
	})
	return err
}

func (g *GitlabInstance) addProjectToCICDCatalog(project *gitlab.Project) error {
	utils.LogVerbosef("Adding project %s to CI/CD catalog", project.PathWithNamespace)
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
	utils.LogVerbosef("Copying project avatar for %s", destinationProject.PathWithNamespace)
	sourceProjectAvatar, _, err := g.Gitlab.Projects.DownloadAvatar(sourceProject.ID)
	if err != nil {
		return fmt.Errorf("failed to download avatar for project %s: %s", sourceProject.PathWithNamespace, err)
	}

	filename := fmt.Sprintf("avatar-%d.png", time.Now().Unix())
	_, _, err = destinationGitlabInstance.Gitlab.Projects.UploadAvatar(destinationProject.ID, sourceProjectAvatar, filename)
	if err != nil {
		return fmt.Errorf("failed to upload avatar for project %s: %s", destinationProject.PathWithNamespace, err)
	}

	return nil
}

func (g *GitlabInstance) copyGroupAvatar(destinationGitlabInstance *GitlabInstance, destinationGroup *gitlab.Group, sourceGroup *gitlab.Group) error {
	utils.LogVerbosef("Copying group avatar for %s", destinationGroup.FullPath)
	sourceGroupAvatar, _, err := g.Gitlab.Groups.DownloadAvatar(sourceGroup.ID)
	if err != nil {
		return fmt.Errorf("failed to download avatar for group %s: %s", sourceGroup.FullPath, err)
	}

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
	wg.Add(maxErrors)
	errorChan := make(chan error, maxErrors)

	go func() {
		defer wg.Done()
		utils.LogVerbosef("enabling project %s mirror pull", destinationProject.PathWithNamespace)
		err := g.enableProjectMirrorPull(sourceProject, destinationProject)
		if err != nil {
			errorChan <- fmt.Errorf("Failed to enable project mirror pull for %s: %s", destinationProject.PathWithNamespace, err)
		}
	}()
	go func() {
		defer wg.Done()
		utils.LogVerbosef("copying project %s avatar", destinationProject.PathWithNamespace)
		err := sourceGitlab.copyProjectAvatar(g, destinationProject, sourceProject)
		if err != nil {
			errorChan <- fmt.Errorf("Failed to copy project avatar for %s: %s", destinationProject.PathWithNamespace, err)
		}
	}()
	if copyOptions.CI_CD_Catalog {
		go func() {
			defer wg.Done()
			utils.LogVerbosef("adding project %s to CI/CD catalog", destinationProject.PathWithNamespace)
			err := g.addProjectToCICDCatalog(destinationProject)
			if err != nil {
				errorChan <- fmt.Errorf("Failed to add project %s to CI/CD catalog: %s", destinationProject.PathWithNamespace, err)
			}
		}()
	}
	wg.Wait()
	close(errorChan)
	return utils.MergeErrors(errorChan, 4)
}
