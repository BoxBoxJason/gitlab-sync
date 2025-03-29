package mirroring

import (
	"fmt"
	"log"
	"sort"
	"sync"

	"github.com/boxboxjason/gitlab-sync/utils"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func createGroups(sourceGitlab *GitlabInstance, destinationGitlab *GitlabInstance, mirrorMapping *utils.MirrorMapping) error {
	utils.LogVerbose("Creating groups in destination GitLab")
	// Reverse the mirror mapping to get the source group path for each destination group
	reversedMirrorMap := make(map[string]string, len(mirrorMapping.Groups))
	// Extract the keys (group paths) and sort them
	// This ensures that the parent groups are created before their children
	destinationGroupPaths := make([]string, 0, len(sourceGitlab.Groups))
	for sourceGroupPath, createOptions := range mirrorMapping.Groups {
		reversedMirrorMap[createOptions.DestinationURL] = sourceGroupPath
		destinationGroupPaths = append(destinationGroupPaths, createOptions.DestinationURL)
	}
	sort.Strings(destinationGroupPaths)

	errorChan := make(chan error, len(destinationGroupPaths))
	// Iterate over the groups in alphabetical order
	for _, destinationGroupPath := range destinationGroupPaths {
		// Retrieve the corresponding source group path
		sourceGroupPath := reversedMirrorMap[destinationGroupPath]
		utils.LogVerbosef("Mirroring group from source %s to destination %s", sourceGroupPath, destinationGroupPath)
		sourceGroup := sourceGitlab.Groups[sourceGroupPath]
		if sourceGroup == nil {
			errorChan <- fmt.Errorf("Group %s not found in destination GitLab instance (internal error, please review script)", sourceGroupPath)
			continue
		}

		// Retrieve the corresponding group creation options from the mirror mapping
		groupCreationOptions, ok := mirrorMapping.Groups[sourceGroupPath]
		if !ok {
			errorChan <- fmt.Errorf("Source Group %s not found in mirror mapping (internal error, please review script)", sourceGroupPath)
			continue
		}

		// Check if the group already exists in the destination GitLab instance
		destinationGroup := destinationGitlab.getGroup(destinationGroupPath)
		var err error
		if destinationGroup != nil {
			utils.LogVerbosef("Group %s already exists, skipping creation", destinationGroupPath)
		} else {
			utils.LogVerbosef("Creating group %s in destination GitLab instance", destinationGroupPath)
			destinationGroup, err = destinationGitlab.createGroupFromSource(sourceGroup, groupCreationOptions)
			if err != nil {
				errorChan <- fmt.Errorf("Failed to create group %s in destination GitLab instance: %s", destinationGroupPath, err)
				continue
			} else {
				err = sourceGitlab.copyGroupAvatar(destinationGitlab, destinationGroup, sourceGroup)
				if err != nil {
					errorChan <- fmt.Errorf("Failed to copy group avatar for %s: %s", destinationGroupPath, err)
				}
			}
		}

	}
	close(errorChan)
	return utils.MergeErrors(errorChan, 2)
}

func createProjects(sourceGitlab *GitlabInstance, destinationGitlab *GitlabInstance, mirrorMapping *utils.MirrorMapping) error {
	utils.LogVerbose("Creating projects in destination GitLab instance")
	// Reverse the mirror mapping to get the source project path for each destination project
	reversedMirrorMap := make(map[string]string, len(mirrorMapping.Projects))
	for sourceProjectPath, projectOptions := range mirrorMapping.Projects {
		reversedMirrorMap[projectOptions.DestinationURL] = sourceProjectPath
	}

	// Create a wait group to wait for all goroutines to finish
	var wg sync.WaitGroup

	// Create a channel to collect errors
	errorChan := make(chan error, len(reversedMirrorMap))

	// Create a channel to limit the number of concurrent goroutines
	concurrencyLimit := 10
	sem := make(chan struct{}, concurrencyLimit)

	for destinationProjectPath, sourceProjectPath := range reversedMirrorMap {
		utils.LogVerbosef("Mirroring project from source %s to destination %s", sourceProjectPath, destinationProjectPath)
		sourceProject := sourceGitlab.Projects[sourceProjectPath]
		if sourceProject == nil {
			errorChan <- fmt.Errorf("Project %s not found in source GitLab instance (internal error, please review script)", sourceProjectPath)
			continue
		}
		wg.Add(1)
		// Acquire a token from the semaphore
		sem <- struct{}{}

		go func(sourcePath string, destinationPath string) {
			defer wg.Done()
			// Release the token back to the semaphore
			defer func() { <-sem }()

			// Retrieve the corresponding project creation options from the mirror mapping
			projectCreationOptions, ok := mirrorMapping.Projects[sourcePath]
			if !ok {
				errorChan <- fmt.Errorf("Project %s not found in mirror mapping (internal error, please review script)", sourcePath)
				return
			}

			// Check if the project already exists
			destinationProject := destinationGitlab.getProject(destinationPath)
			var err error
			if destinationProject != nil {
				utils.LogVerbosef("Project %s already exists, skipping creation", destinationPath)
			} else {
				sourceProject := sourceGitlab.Projects[sourcePath]
				if sourceProject == nil {
					errorChan <- fmt.Errorf("Project %s not found in source GitLab instance (internal error, please review script)", sourcePath)
					return
				}
				destinationProject, err = destinationGitlab.createProjectFromSource(sourceProject, projectCreationOptions)
				if err != nil {
					errorChan <- fmt.Errorf("Failed to create project %s in destination GitLab instance: %s", destinationPath, err)
					return
				}
			}

			err = destinationGitlab.updateProjectFromSource(sourceGitlab, sourceProject, destinationProject, projectCreationOptions)
			if err != nil {
				errorChan <- fmt.Errorf("Failed to update project %s in destination GitLab instance: %s", destinationPath, err)
				return
			}

			log.Printf("Completed mirroring project to %s", destinationPath)
		}(sourceProjectPath, destinationProjectPath)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(errorChan)

	return utils.MergeErrors(errorChan, 2)
}

func (g *GitlabInstance) createProjectFromSource(sourceProject *gitlab.Project, copyOptions *utils.ProjectMirroringOptions) (*gitlab.Project, error) {
	projectCreationArgs := &gitlab.CreateProjectOptions{
		Name:                &sourceProject.Name,
		Path:                &sourceProject.Path,
		DefaultBranch:       &sourceProject.DefaultBranch,
		Description:         &sourceProject.Description,
		MirrorTriggerBuilds: gitlab.Ptr(true),
		Mirror:              gitlab.Ptr(true),
		Topics:              &sourceProject.Topics,
	}

	utils.LogVerbosef("Retrieving project namespace ID for %s", copyOptions.DestinationURL)
	parentNamespaceId, err := g.getParentNamespaceID(copyOptions.DestinationURL)
	if err != nil {
		return nil, err
	} else if parentNamespaceId >= 0 {
		projectCreationArgs.NamespaceID = &parentNamespaceId
	}

	utils.LogVerbosef("Creating project %s in destination GitLab instance", copyOptions.DestinationURL)
	destinationProject, _, err := g.Gitlab.Projects.CreateProject(projectCreationArgs)
	if err != nil {
		return nil, err
	}
	utils.LogVerbosef("Project %s created successfully", destinationProject.PathWithNamespace)
	g.addProject(copyOptions.DestinationURL, destinationProject)

	return destinationProject, nil
}

func (g *GitlabInstance) createGroupFromSource(sourceGroup *gitlab.Group, copyOptions *utils.GroupMirroringOptions) (*gitlab.Group, error) {
	groupCreationArgs := &gitlab.CreateGroupOptions{
		Name:          &sourceGroup.Name,
		Path:          &sourceGroup.Path,
		Description:   &sourceGroup.Description,
		Visibility:    &sourceGroup.Visibility,
		DefaultBranch: &sourceGroup.DefaultBranch,
	}

	parentGroupID, err := g.getParentNamespaceID(copyOptions.DestinationURL)
	if err != nil {
		return nil, err
	} else if parentGroupID >= 0 {
		groupCreationArgs.ParentID = &parentGroupID
	}

	destinationGroup, _, err := g.Gitlab.Groups.CreateGroup(groupCreationArgs)
	if err == nil {
		g.addGroup(copyOptions.DestinationURL, destinationGroup)
	}

	return destinationGroup, err
}
