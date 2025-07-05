package mirroring

import (
	"fmt"
	"sync"

	"gitlab-sync/internal/utils"
	"gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

// ============================================================ //
//                 GROUP CREATION FUNCTIONS                     //
// ============================================================ //

// createGroups creates GitLab groups in the destination GitLab instance based on the mirror mapping.
// It retrieves the source group path for each destination group and creates the group in the destination instance.
// The function also handles the copying of group avatars from the source to the destination instance.
func (destinationGitlab *GitlabInstance) createGroups(sourceGitlab *GitlabInstance, mirrorMapping *utils.MirrorMapping) []error {
	zap.L().Info("Creating groups in GitLab Instance", zap.String(ROLE, ROLE_DESTINATION))

	// Reverse the mirror mapping to get the source group path for each destination group
	reversedMirrorMap, destinationGroupPaths := sourceGitlab.reverseGroupMirrorMap(mirrorMapping)

	errorChan := make(chan []error, len(destinationGroupPaths))
	// Iterate over the groups in alphabetical order (little hack to ensure parent groups are created before children)
	for _, destinationGroupPath := range destinationGroupPaths {
		_, err := destinationGitlab.createGroup(destinationGroupPath, sourceGitlab, mirrorMapping, &reversedMirrorMap)
		if err != nil {
			errorChan <- err
		}
	}
	close(errorChan)
	return helpers.MergeErrors(errorChan)
}

// createGroup creates a GitLab group in the destination GitLab instance based on the source group and mirror mapping.
// It checks if the group already exists in the destination instance and creates it if not.
// The function also handles the copying of group avatars from the source to the destination instance.
func (destinationGitlab *GitlabInstance) createGroup(destinationGroupPath string, sourceGitlab *GitlabInstance, mirrorMapping *utils.MirrorMapping, reversedMirrorMap *map[string]string) (*gitlab.Group, []error) {
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
	destinationGroup := destinationGitlab.getGroup(destinationGroupPath)
	var err error
	if destinationGroup == nil {
		zap.L().Debug("Group not found, creating new group in GitLab Instance", zap.String("group", destinationGroupPath), zap.String(ROLE, ROLE_DESTINATION))
		destinationGroup, err = destinationGitlab.createGroupFromSource(sourceGroup, groupCreationOptions)
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

// createGroupFromSource creates a GitLab group in the destination GitLab instance based on the source group.
// It sets the group name, path, description, visibility, and default branch based on the source group.
// The function also handles the setting of the parent ID for the group.
// It returns the created group or an error if the creation fails.
func (g *GitlabInstance) createGroupFromSource(sourceGroup *gitlab.Group, copyOptions *utils.MirroringOptions) (*gitlab.Group, error) {
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
	parentGroupID, err := g.getParentNamespaceID(copyOptions.DestinationPath)
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
		g.addGroup(destinationGroup)
	}

	return destinationGroup, err
}

// ============================================================ //
//                 PROJECT CREATION FUNCTIONS                   //
// ============================================================ //

// createProjects creates GitLab projects in the destination GitLab instance based on the mirror mapping.
// It retrieves the source project path for each destination project and creates the project in the destination instance.
func (destinationGitlab *GitlabInstance) createProjects(sourceGitlab *GitlabInstance, mirrorMapping *utils.MirrorMapping) []error {
	zap.L().Info("Creating projects in GitLab Instance", zap.String(ROLE, ROLE_DESTINATION))

	// Create a wait group to wait for all goroutines to finish
	var wg sync.WaitGroup

	// Create a channel to collect errors
	errorChan := make(chan error, len(mirrorMapping.Projects))

	for sourceProjectPath, destinationProjectOptions := range mirrorMapping.Projects {
		zap.L().Debug("Mirroring project", zap.String(ROLE_SOURCE, sourceProjectPath), zap.String(ROLE_DESTINATION, destinationProjectOptions.DestinationPath))
		// Retrieve the corresponding source project path
		sourceProject := sourceGitlab.Projects[sourceProjectPath]
		if sourceProject == nil {
			errorChan <- fmt.Errorf("project %s not found in source GitLab instance (internal error, please review script)", sourceProjectPath)
			continue
		}
		wg.Add(1)
		// Create a goroutine to handle the project creation
		go func(sourcePath string, destinationCopyOptions *utils.MirroringOptions) {
			defer wg.Done()
			_, err := destinationGitlab.createProject(sourcePath, destinationCopyOptions, sourceGitlab)
			if err != nil {
				errorChan <- fmt.Errorf("failed to create project %s in destination GitLab instance: %v", destinationCopyOptions.DestinationPath, err)
			}
		}(sourceProjectPath, destinationProjectOptions)
	}

	// Wait for all goroutines to finish & close the error channel
	wg.Wait()
	close(errorChan)

	return helpers.MergeErrors(errorChan)
}

// createProject creates a GitLab project in the destination GitLab instance based on the source project and mirror mapping.
// It checks if the project already exists in the destination instance and creates it if not.
// The function also handles the copying of project avatars from the source to the destination instance.
func (destinationGitlab *GitlabInstance) createProject(sourceProjectPath string, projectCreationOptions *utils.MirroringOptions, sourceGitlab *GitlabInstance) (*gitlab.Project, []error) {
	destinationProjectPath := projectCreationOptions.DestinationPath
	// Check if the project already exists
	destinationProject := destinationGitlab.getProject(destinationProjectPath)
	var err error
	sourceProject := sourceGitlab.Projects[sourceProjectPath]
	if sourceProject == nil {
		return nil, []error{fmt.Errorf("project %s not found in source GitLab instance (internal error, please review script)", sourceProjectPath)}
	}

	// Check if the project already exists in the destination GitLab instance
	// If it does not exist, create it
	if destinationProject == nil {
		destinationProject, err = destinationGitlab.createProjectFromSource(sourceProject, projectCreationOptions)
		if err != nil || destinationProject == nil {
			return nil, []error{fmt.Errorf("failed to create project %s in destination GitLab instance: %s", destinationProjectPath, err)}
		}
	}

	// If the project already exists, update it with the source project details
	mergedError := destinationGitlab.updateProjectFromSource(sourceGitlab, sourceProject, destinationProject, projectCreationOptions)

	zap.L().Info("Completed project mirroring", zap.String(ROLE_SOURCE, sourceProjectPath), zap.String(ROLE_DESTINATION, destinationProjectPath))
	return destinationProject, mergedError
}

// createProjectFromSource creates a GitLab project in the destination GitLab instance based on the source project.
// It sets the project name, path, default branch, description, and visibility based on the source project.
// The function also handles the setting of the namespace ID for the project.
// It returns the created project or an error if the creation fails.
func (g *GitlabInstance) createProjectFromSource(sourceProject *gitlab.Project, copyOptions *utils.MirroringOptions) (*gitlab.Project, error) {
	// Define the API call logic
	projectCreationArgs := &gitlab.CreateProjectOptions{
		Name:                &sourceProject.Name,
		Path:                &sourceProject.Path,
		DefaultBranch:       &sourceProject.DefaultBranch,
		Description:         &sourceProject.Description,
		MirrorTriggerBuilds: &copyOptions.MirrorTriggerBuilds,
		Mirror:              gitlab.Ptr(true),
		Visibility:          gitlab.Ptr(gitlab.VisibilityValue(copyOptions.Visibility)),
	}

	zap.L().Debug("Retrieving project namespace ID", zap.String(ROLE_DESTINATION, copyOptions.DestinationPath))

	// Get the parent namespace ID for the project
	// This is used to set the namespace ID for the project
	parentNamespaceId, err := g.getParentNamespaceID(copyOptions.DestinationPath)
	if err != nil {
		return nil, err
	} else if parentNamespaceId >= 0 {
		projectCreationArgs.NamespaceID = &parentNamespaceId
	}

	// Create the project in the destination GitLab instance
	zap.L().Debug("Creating project in GitLab Instance", zap.String(ROLE, ROLE_DESTINATION), zap.String(ROLE_DESTINATION, copyOptions.DestinationPath))
	destinationProject, _, err := g.Gitlab.Projects.CreateProject(projectCreationArgs)
	if err == nil {
		zap.L().Info("Project created", zap.String("project", destinationProject.HTTPURLToRepo))
		g.addProject(destinationProject)
	}

	return destinationProject, err
}

// ============================================================ //
//               RELEASES CREATION FUNCTIONS                    //
// ============================================================ //

// mirrorReleases mirrors releases from the source project to the destination project.
// It fetches existing releases from the destination project and creates new releases for those that do not exist.
// The function handles the API calls concurrently using goroutines and a wait group.
// It returns an error if any of the API calls fail.
func (destinationGitlab *GitlabInstance) mirrorReleases(sourceGitlab *GitlabInstance, sourceProject *gitlab.Project, destinationProject *gitlab.Project) []error {
	zap.L().Info("Starting releases mirroring", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))
	// Fetch existing releases from the destination project
	existingReleases, _, err := destinationGitlab.Gitlab.Releases.ListReleases(destinationProject.ID, &gitlab.ListReleasesOptions{})
	if err != nil {
		return []error{fmt.Errorf("failed to fetch existing releases for destination project %s: %s", destinationProject.HTTPURLToRepo, err)}
	}

	// Create a map of existing release tags for quick lookup
	existingReleaseTags := make(map[string]struct{})
	for _, release := range existingReleases {
		if release != nil {
			existingReleaseTags[release.TagName] = struct{}{}
		}
	}

	// Fetch releases from the source project
	sourceReleases, _, err := sourceGitlab.Gitlab.Releases.ListReleases(sourceProject.ID, &gitlab.ListReleasesOptions{})
	if err != nil {
		return []error{fmt.Errorf("failed to fetch releases for source project %s: %s", sourceProject.HTTPURLToRepo, err)}
	}

	// Create a wait group and an error channel for handling API calls concurrently
	var wg sync.WaitGroup
	errorChan := make(chan error, len(sourceReleases))

	// Iterate over each source release
	for _, release := range sourceReleases {
		// Check if the release already exists in the destination project
		if _, exists := existingReleaseTags[release.TagName]; exists {
			zap.L().Debug("Release already exists", zap.String("release", release.TagName), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))
			continue
		}

		// Increment the wait group counter
		wg.Add(1)

		// Define the API call logic for creating a release
		go func(releaseToMirror *gitlab.Release) {
			defer wg.Done()
			zap.L().Debug("Creating release in destination project", zap.String("release", releaseToMirror.TagName), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))

			// Create the release in the destination project
			_, _, err := destinationGitlab.Gitlab.Releases.CreateRelease(destinationProject.ID, &gitlab.CreateReleaseOptions{
				Name:        &releaseToMirror.Name,
				TagName:     &releaseToMirror.TagName,
				Description: &releaseToMirror.Description,
				ReleasedAt:  releaseToMirror.ReleasedAt,
			})
			if err != nil {
				errorChan <- fmt.Errorf("failed to create release %s in project %s: %s", releaseToMirror.TagName, destinationProject.HTTPURLToRepo, err)
			}
		}(release)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(errorChan)

	zap.L().Info("Releases mirroring completed", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))
	return helpers.MergeErrors(errorChan)
}

// ============================================================ //
//                 CI/CD CATALOG FUNCTIONS                      //
// ============================================================ //

// addProjectToCICDCatalog adds a project to the CI/CD catalog in the destination GitLab instance.
// It uses a GraphQL mutation to create the catalog resource for the project.
func (g *GitlabInstance) addProjectToCICDCatalog(project *gitlab.Project) error {
	zap.L().Debug("Adding project to CI/CD catalog", zap.String("project", project.HTTPURLToRepo))
	mutation := `
    mutation {
        catalogResourcesCreate(input: { projectPath: "%s" }) {
            errors
        }
    }`
	query := fmt.Sprintf(mutation, project.PathWithNamespace)
	var response struct {
		Data struct {
			CatalogResourcesCreate struct {
				Errors []string `json:"errors"`
			} `json:"catalogResourcesCreate"`
		} `json:"data"`
	}

	_, err := g.Gitlab.GraphQL.Do(gitlab.GraphQLQuery{Query: query}, &response)
	return err
}
