package mirroring

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"gitlab-sync/internal/utils"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

func createGroups(sourceGitlab *GitlabInstance, destinationGitlab *GitlabInstance, mirrorMapping *utils.MirrorMapping) error {
	zap.L().Debug("Creating groups in destination GitLab")
	// Reverse the mirror mapping to get the source group path for each destination group
	reversedMirrorMap := make(map[string]string, len(mirrorMapping.Groups))
	// Extract the keys (group paths) and sort them
	// This ensures that the parent groups are created before their children
	destinationGroupPaths := make([]string, 0, len(sourceGitlab.Groups))
	for sourceGroupPath, createOptions := range mirrorMapping.Groups {
		reversedMirrorMap[createOptions.DestinationPath] = sourceGroupPath
		destinationGroupPaths = append(destinationGroupPaths, createOptions.DestinationPath)
	}
	sort.Strings(destinationGroupPaths)

	errorChan := make(chan error, len(destinationGroupPaths))
	// Iterate over the groups in alphabetical order
	for _, destinationGroupPath := range destinationGroupPaths {
		// Retrieve the corresponding source group path
		sourceGroupPath := reversedMirrorMap[destinationGroupPath]
		zap.L().Sugar().Debugf("Mirroring group from source %s to destination %s", sourceGroupPath, destinationGroupPath)
		sourceGroup := sourceGitlab.Groups[sourceGroupPath]
		if sourceGroup == nil {
			errorChan <- fmt.Errorf("group %s not found in destination GitLab instance (internal error, please review script)", sourceGroupPath)
			continue
		}

		// Retrieve the corresponding group creation options from the mirror mapping
		groupCreationOptions, ok := mirrorMapping.Groups[sourceGroupPath]
		if !ok {
			errorChan <- fmt.Errorf("source group %s not found in mirror mapping (internal error, please review script)", sourceGroupPath)
			continue
		}

		// Check if the group already exists in the destination GitLab instance
		destinationGroup := destinationGitlab.getGroup(destinationGroupPath)
		var err error
		if destinationGroup != nil {
			zap.L().Sugar().Debugf("Group %s already exists, skipping creation", destinationGroupPath)
		} else {
			zap.L().Sugar().Debugf("Creating group %s in destination GitLab instance", destinationGroupPath)
			destinationGroup, err = destinationGitlab.createGroupFromSource(sourceGroup, groupCreationOptions)
			if err != nil {
				errorChan <- fmt.Errorf("failed to create group %s in destination GitLab instance: %s", destinationGroupPath, err)
				continue
			} else {
				err = sourceGitlab.copyGroupAvatar(destinationGitlab, destinationGroup, sourceGroup)
				if err != nil {
					errorChan <- fmt.Errorf("failed to copy group avatar for %s: %s", destinationGroupPath, err)
				}
			}
		}

	}
	close(errorChan)
	return utils.MergeErrors(errorChan, 2)
}

func createProjects(sourceGitlab *GitlabInstance, destinationGitlab *GitlabInstance, mirrorMapping *utils.MirrorMapping) error {
	zap.L().Debug("Creating projects in destination GitLab instance")

	// Reverse the mirror mapping to get the source project path for each destination project
	reversedMirrorMap := make(map[string]string, len(mirrorMapping.Projects))
	for sourceProjectPath, projectOptions := range mirrorMapping.Projects {
		reversedMirrorMap[projectOptions.DestinationPath] = sourceProjectPath
	}

	// Create a wait group to wait for all goroutines to finish
	var wg sync.WaitGroup

	// Create a channel to collect errors
	errorChan := make(chan error, len(reversedMirrorMap))

	for destinationProjectPath, sourceProjectPath := range reversedMirrorMap {
		zap.L().Sugar().Debugf("Mirroring project from source %s to destination %s", sourceProjectPath, destinationProjectPath)
		sourceProject := sourceGitlab.Projects[sourceProjectPath]
		if sourceProject == nil {
			errorChan <- fmt.Errorf("project %s not found in source GitLab instance (internal error, please review script)", sourceProjectPath)
			continue
		}
		wg.Add(1)

		go func(sourcePath string, destinationPath string) {
			defer wg.Done()

			// Retrieve the corresponding project creation options from the mirror mapping
			projectCreationOptions, ok := mirrorMapping.Projects[sourcePath]
			if !ok {
				errorChan <- fmt.Errorf("project %s not found in mirror mapping (internal error, please review script)", sourcePath)
				return
			}

			// Check if the project already exists
			destinationProject := destinationGitlab.getProject(destinationPath)
			var err error
			if destinationProject != nil {
				zap.L().Sugar().Debugf("project %s already exists, skipping creation", destinationPath)
			} else {
				sourceProject := sourceGitlab.Projects[sourcePath]
				if sourceProject == nil {
					errorChan <- fmt.Errorf("project %s not found in source GitLab instance (internal error, please review script)", sourcePath)
					return
				}
				destinationProject, err = destinationGitlab.createProjectFromSource(sourceProject, projectCreationOptions)
				if err != nil {
					errorChan <- fmt.Errorf("failed to create project %s in destination GitLab instance: %s", destinationPath, err)
					return
				}
			}

			err = destinationGitlab.updateProjectFromSource(sourceGitlab, sourceProject, destinationProject, projectCreationOptions)
			if err != nil {
				errorChan <- fmt.Errorf("failed to update project %s in destination GitLab instance: %s", destinationPath, err)
				return
			}

			zap.L().Info("Completed mirroring project to " + destinationPath)
		}(sourceProjectPath, destinationProjectPath)
	}

	wg.Wait()
	close(errorChan)

	return utils.MergeErrors(errorChan, 2)
}

func (g *GitlabInstance) createProjectFromSource(sourceProject *gitlab.Project, copyOptions *utils.MirroringOptions) (*gitlab.Project, error) {
	// Define the API call logic
	projectCreationArgs := &gitlab.CreateProjectOptions{
		Name:                &sourceProject.Name,
		Path:                &sourceProject.Path,
		DefaultBranch:       &sourceProject.DefaultBranch,
		Description:         &sourceProject.Description,
		MirrorTriggerBuilds: &copyOptions.MirrorTriggerBuilds,
		Mirror:              gitlab.Ptr(true),
		Topics:              &sourceProject.Topics,
		Visibility:          gitlab.Ptr(gitlab.VisibilityValue(copyOptions.Visibility)),
	}

	zap.L().Sugar().Debugf("Retrieving project namespace ID for %s", copyOptions.DestinationPath)
	parentNamespaceId, err := g.getParentNamespaceID(copyOptions.DestinationPath)
	if err != nil {
		return nil, err
	} else if parentNamespaceId >= 0 {
		projectCreationArgs.NamespaceID = &parentNamespaceId
	}

	zap.L().Sugar().Debugf("Creating project %s in destination GitLab instance", copyOptions.DestinationPath)
	destinationProject, _, err := g.Gitlab.Projects.CreateProject(projectCreationArgs)
	if err != nil {
		return nil, err
	}
	zap.L().Sugar().Debugf("Project %s created successfully", destinationProject.PathWithNamespace)
	g.addProject(copyOptions.DestinationPath, destinationProject)

	return destinationProject, nil
}

func (g *GitlabInstance) createGroupFromSource(sourceGroup *gitlab.Group, copyOptions *utils.MirroringOptions) (*gitlab.Group, error) {
	groupCreationArgs := &gitlab.CreateGroupOptions{
		Name:          &sourceGroup.Name,
		Path:          &sourceGroup.Path,
		Description:   &sourceGroup.Description,
		Visibility:    &sourceGroup.Visibility,
		DefaultBranch: &sourceGroup.DefaultBranch,
	}

	parentGroupID, err := g.getParentNamespaceID(copyOptions.DestinationPath)
	if err != nil {
		return nil, err
	} else if parentGroupID >= 0 {
		groupCreationArgs.ParentID = &parentGroupID
	}

	destinationGroup, _, err := g.Gitlab.Groups.CreateGroup(groupCreationArgs)
	if err == nil {
		g.addGroup(copyOptions.DestinationPath, destinationGroup)
	}

	return destinationGroup, err
}

func (g *GitlabInstance) mirrorReleases(sourceProject *gitlab.Project, destinationProject *gitlab.Project) error {
	zap.L().Sugar().Debugf("Starting releases mirroring for project %s", destinationProject.HTTPURLToRepo)
	// Fetch existing releases from the destination project
	existingReleases, _, err := g.Gitlab.Releases.ListReleases(destinationProject.ID, &gitlab.ListReleasesOptions{})
	if err != nil {
		return fmt.Errorf("failed to fetch existing releases for destination project %s: %s", destinationProject.PathWithNamespace, err)
	}

	// Create a map of existing release tags for quick lookup
	existingReleaseTags := make(map[string]bool)
	for _, release := range existingReleases {
		existingReleaseTags[release.TagName] = true
	}

	// Fetch releases from the source project
	sourceReleases, _, err := g.Gitlab.Releases.ListReleases(sourceProject.ID, &gitlab.ListReleasesOptions{})
	if err != nil {
		return fmt.Errorf("failed to fetch releases for source project %s: %s", sourceProject.PathWithNamespace, err)
	}

	// Create a wait group and an error channel for handling API calls concurrently
	var wg sync.WaitGroup
	errorChan := make(chan error, len(sourceReleases))

	// Iterate over each source release
	for _, release := range sourceReleases {
		// Check if the release already exists in the destination project
		if existingReleaseTags[release.TagName] {
			zap.L().Sugar().Debugf("Release %s already exists in destination project %s, skipping.", release.TagName, destinationProject.PathWithNamespace)
			continue
		}

		// Increment the wait group counter
		wg.Add(1)

		// Define the API call logic for creating a release
		releaseToMirror := release // Capture the current release in the loop
		go func() {
			defer wg.Done()
			zap.L().Sugar().Debugf("Mirroring release %s to project %s", releaseToMirror.TagName, destinationProject.PathWithNamespace)

			// Create the release in the destination project
			_, _, err := g.Gitlab.Releases.CreateRelease(destinationProject.ID, &gitlab.CreateReleaseOptions{
				Name:        &releaseToMirror.Name,
				TagName:     &releaseToMirror.TagName,
				Description: &releaseToMirror.Description,
				ReleasedAt:  releaseToMirror.ReleasedAt,
			})
			if err != nil {
				errMsg := fmt.Sprintf("Failed to create release %s in project %s: %s", releaseToMirror.TagName, destinationProject.PathWithNamespace, err)
				zap.L().Debug(errMsg)
				errorChan <- errors.New(errMsg)
			}
		}()
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(errorChan)

	// Check the error channel for any errors
	var combinedError error
	for err := range errorChan {
		if combinedError == nil {
			combinedError = err
		} else {
			combinedError = fmt.Errorf("%s; %s", combinedError, err)
		}
	}

	if combinedError != nil {
		return combinedError
	}

	zap.L().Sugar().Debugf("Releases mirroring completed for project %s", destinationProject.HTTPURLToRepo)
	return nil
}
