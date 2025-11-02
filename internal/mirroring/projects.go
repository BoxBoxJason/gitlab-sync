package mirroring

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"gitlab-sync/internal/utils"
	"gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

// ===========================================================================
//                          PROJECTS GET FUNCTIONS                          //
// ===========================================================================

// FetchAndProcessProjects retrieves all projects that match the filters from the GitLab instance and stores them in the instance cache.
// It also updates the mirror mapping with the corresponding group creation options.
//
// The function is run in a goroutine for each project, and a wait group is used to wait for all goroutines to finish.
func (g *GitlabInstance) FetchAndProcessProjects(projectFilters *map[string]struct{}, groupFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) []error {
	zap.L().Debug("Fetching and processing projects from GitLab instance", zap.String(ROLE, g.Role), zap.String(INSTANCE_SIZE, g.InstanceSize), zap.Int("projects", len(*projectFilters)), zap.Int("groups", len(*groupFilters)))
	if !g.IsBig() {
		return g.FetchAndProcessProjectsSmallInstance(projectFilters, groupFilters, mirrorMapping)
	}
	return g.FetchAndProcessProjectsBigInstance(projectFilters, mirrorMapping)
}

// storeProject stores the project in the Gitlab instance projects cache
// and updates the mirror mapping with the corresponding group creation options.
func (g *GitlabInstance) storeProject(project *gitlab.Project, parentGroupPath string, mirrorMapping *utils.MirrorMapping) {
	// Add the project to the gitlab instance projects cache
	g.AddProject(project)

	if g.Role == ROLE_SOURCE {
		zap.L().Debug("Storing project in mirror mapping", zap.String("project", project.HTTPURLToRepo), zap.String("group", parentGroupPath))
		// Retrieve the corresponding group creation options from the mirror mapping
		groupCreationOptions, ok := mirrorMapping.GetGroup(parentGroupPath)
		if !ok {
			zap.L().Error("Group not found in mirror mapping", zap.String("group", parentGroupPath))
			return
		}

		// Calculate the relative path between the project and the group
		relativePath, err := filepath.Rel(parentGroupPath, project.PathWithNamespace)
		if err != nil {
			zap.L().Error("Failed to calculate relative path for project", zap.String("project", project.HTTPURLToRepo), zap.String("group", parentGroupPath), zap.Error(err))
			return
		}

		// Add the project to the mirror mapping with the corresponding group creation options
		mirrorMapping.AddProject(project.PathWithNamespace, &utils.MirroringOptions{
			DestinationPath:     filepath.Join(groupCreationOptions.DestinationPath, relativePath),
			CI_CD_Catalog:       groupCreationOptions.CI_CD_Catalog,
			MirrorIssues:        groupCreationOptions.MirrorIssues,
			MirrorTriggerBuilds: groupCreationOptions.MirrorTriggerBuilds,
			Visibility:          groupCreationOptions.Visibility,
			MirrorReleases:      groupCreationOptions.MirrorReleases,
		})
	}
}

// ===========================================================================
//                         SMALL INSTANCE FUNCTIONS                         //
// ===========================================================================

// FetchAndProcessProjectsSmallInstance retrieves all projects from the small GitLab instance
// and processes them to store in the instance cache.
func (g *GitlabInstance) FetchAndProcessProjectsSmallInstance(projectFilters *map[string]struct{}, groupFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) []error {
	allProjects, err := g.FetchAllProjectsSmallInstance()
	if err != nil {
		if len(allProjects) == 0 {
			return []error{err}
		} else {
			zap.L().Warn("Failed to fetch all projects from GitLab instance", zap.String(ROLE, g.Role), zap.Error(err))
		}
	}
	g.processProjectsSmallInstance(allProjects, projectFilters, groupFilters, mirrorMapping)
	zap.L().Debug("Found matching projects in the GitLab instance", zap.String(ROLE, g.Role), zap.Int("projects", len(g.Projects)))
	return nil
}

// FetchAllProjectsSmallInstance retrieves all projects from the small GitLab instance
func (g *GitlabInstance) FetchAllProjectsSmallInstance() ([]*gitlab.Project, error) {
	zap.L().Debug("Fetching all projects from GitLab instance", zap.String(ROLE, g.Role))
	fetchOpts := &gitlab.ListProjectsOptions{
		Archived:             gitlab.Ptr(false),
		IncludeHidden:        gitlab.Ptr(false),
		IncludePendingDelete: gitlab.Ptr(false),
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
			Page:    1,
		},
	}
	var allProjects []*gitlab.Project

	for {
		projects, resp, err := g.Gitlab.Projects.ListProjects(fetchOpts)
		if err != nil {
			return allProjects, err
		}

		allProjects = append(allProjects, projects...)

		if resp.CurrentPage >= resp.TotalPages {
			break
		}

		fetchOpts.Page = resp.NextPage
	}

	return allProjects, nil
}

// processProjectsSmallInstance processes the projects from the small GitLab instance
// and stores those which match the filters in the instance cache.
//
// The function is run in a goroutine for each project.
// It returns an error if any of the goroutines fail.
func (g *GitlabInstance) processProjectsSmallInstance(allProjects []*gitlab.Project, projectFilters *map[string]struct{}, groupFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) {
	zap.L().Debug("Processing projects from GitLab instance", zap.String(INSTANCE_SIZE, g.InstanceSize), zap.String(ROLE, g.Role), zap.Int("projects", len(allProjects)))

	// Create a wait group to wait for all goroutines to finish
	var wg sync.WaitGroup

	for _, project := range allProjects {
		wg.Add(1)

		go func(project *gitlab.Project) {
			defer wg.Done()

			group, matches := helpers.MatchPathAgainstFilters(project.PathWithNamespace, projectFilters, groupFilters)
			if matches {
				g.storeProject(project, group, mirrorMapping)
			}

		}(project)
	}

	wg.Wait()
}

// ===========================================================================
//                         LARGE INSTANCE FUNCTIONS                         //
// ===========================================================================

// FetchAndProcessProjectsBigInstance retrieves all projects individually from the large GitLab instance
// and processes them to store in the instance cache.
//
// It uses goroutines to fetch each project in parallel and a wait group to wait for all goroutines to finish.
// It returns an error if any of the goroutines fail.
func (g *GitlabInstance) FetchAndProcessProjectsBigInstance(projectFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) []error {
	// Fetch each project in parallel
	var wg sync.WaitGroup
	projectsChan := make(chan *gitlab.Project, len(*projectFilters))
	errCh := make(chan error, len(*projectFilters))
	wg.Add(len(*projectFilters))
	for project := range *projectFilters {
		go func(projectPath string) {
			defer wg.Done()
			p, _, err := g.Gitlab.Projects.GetProject(projectPath, &gitlab.GetProjectOptions{})
			if err != nil {
				errCh <- fmt.Errorf("failed to retrieve project %s: %v", projectPath, err)
				return
			}
			projectsChan <- p
		}(project)
	}
	wg.Wait()
	close(errCh)
	close(projectsChan)

	for project := range projectsChan {
		g.storeProject(project, project.PathWithNamespace, mirrorMapping)
	}
	return helpers.MergeErrors(errCh)
}

// FetchAndProcessGroupProjects retrieves all projects from the group and processes them to store in the instance cache.
func (g *GitlabInstance) FetchAndProcessGroupProjects(group *gitlab.Group, fetchOriginPath string, mirrorMapping *utils.MirrorMapping, errChan chan error, wg *sync.WaitGroup) {
	defer wg.Done()
	if group != nil {
		// Retrieve all projects in the group
		opt := &gitlab.ListGroupProjectsOptions{
			Archived: gitlab.Ptr(false),
			ListOptions: gitlab.ListOptions{
				PerPage: 100,
				Page:    1,
			},
		}

		for {
			projects, resp, err := g.Gitlab.Groups.ListGroupProjects(group.ID, opt)
			if err != nil {
				errChan <- fmt.Errorf("failed to retrieve projects for group %s: %v", group.Name, err)
			}

			for _, project := range projects {
				g.storeProject(project, fetchOriginPath, mirrorMapping)
			}

			if resp.CurrentPage >= resp.TotalPages {
				break
			}

			opt.Page = resp.NextPage
		}
	}

}

// ============================================================ //
//                 PROJECT CREATION FUNCTIONS                   //
// ============================================================ //

// CreateProjects creates GitLab projects in the destination GitLab instance based on the mirror mapping.
// It retrieves the source project path for each destination project and creates the project in the destination instance.
func (destinationGitlab *GitlabInstance) CreateProjects(sourceGitlab *GitlabInstance, mirrorMapping *utils.MirrorMapping) []error {
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
			_, err := destinationGitlab.CreateProject(sourcePath, destinationCopyOptions, sourceGitlab)
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

// CreateProject creates a GitLab project in the destination GitLab instance based on the source project and mirror mapping.
// It checks if the project already exists in the destination instance and creates it if not.
// The function also handles the copying of project avatars from the source to the destination instance.
func (destinationGitlab *GitlabInstance) CreateProject(sourceProjectPath string, projectCreationOptions *utils.MirroringOptions, sourceGitlab *GitlabInstance) (*gitlab.Project, []error) {
	destinationProjectPath := projectCreationOptions.DestinationPath
	// Check if the project already exists
	destinationProject := destinationGitlab.GetProject(destinationProjectPath)
	var err error
	sourceProject := sourceGitlab.Projects[sourceProjectPath]
	if sourceProject == nil {
		return nil, []error{fmt.Errorf("project %s not found in source GitLab instance (internal error, please review script)", sourceProjectPath)}
	}

	// Check if the project already exists in the destination GitLab instance
	// If it does not exist, create it
	if destinationProject == nil {
		destinationProject, err = destinationGitlab.CreateProjectFromSource(sourceProject, projectCreationOptions)
		if err != nil || destinationProject == nil {
			return nil, []error{fmt.Errorf("failed to create project %s in destination GitLab instance: %s", destinationProjectPath, err)}
		}
	}

	// If the project already exists, update it with the source project details
	mergedError := destinationGitlab.UpdateProjectFromSource(sourceGitlab, sourceProject, destinationProject, projectCreationOptions)

	zap.L().Info("Completed project mirroring", zap.String(ROLE_SOURCE, sourceProjectPath), zap.String(ROLE_DESTINATION, destinationProjectPath))
	return destinationProject, mergedError
}

// CreateProjectFromSource creates a GitLab project in the destination GitLab instance based on the source project.
// It sets the project name, path, default branch, description, and visibility based on the source project.
// The function also handles the setting of the namespace ID for the project.
// It returns the created project or an error if the creation fails.
func (g *GitlabInstance) CreateProjectFromSource(sourceProject *gitlab.Project, copyOptions *utils.MirroringOptions) (*gitlab.Project, error) {
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
	parentNamespaceId, err := g.GetParentNamespaceID(copyOptions.DestinationPath)
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
		g.AddProject(destinationProject)

		// Claim ownership of the created project
		if err := g.ClaimOwnershipToProject(destinationProject); err != nil {
			zap.L().Warn("Failed to claim ownership of project", zap.String("project", destinationProject.PathWithNamespace), zap.Error(err))
		}
	}

	return destinationProject, err
}

// ===========================================================================
//                          PROJECTS PUT FUNCTIONS                          //
// ===========================================================================

// UpdateProjectFromSource updates the destination project with settings from the source project.
// It enables the project mirror pull, copies the project avatar, and optionally adds the project to the CI/CD catalog.
// It also mirrors releases if the option is set.
// The function uses goroutines to perform these tasks concurrently and waits for all of them to finish.
func (destinationGitlabInstance *GitlabInstance) UpdateProjectFromSource(sourceGitlabInstance *GitlabInstance, sourceProject *gitlab.Project, destinationProject *gitlab.Project, copyOptions *utils.MirroringOptions) []error {
	// Immediately capture pointers in local variables to avoid any late overrides
	srcProj := sourceProject
	dstProj := destinationProject
	if srcProj == nil || dstProj == nil {
		return []error{fmt.Errorf("source or destination project is nil")}
	}

	wg := sync.WaitGroup{}
	wg.Add(3)
	errorChan := make(chan error, 5)

	go func(sp *gitlab.Project, dp *gitlab.Project) {
		defer wg.Done()
		errorChan <- destinationGitlabInstance.SyncProjectAttributes(sp, dp, copyOptions)
	}(srcProj, dstProj)

	go func(sp *gitlab.Project, dp *gitlab.Project) {
		defer wg.Done()
		errorChan <- destinationGitlabInstance.MirrorProjectGit(sourceGitlabInstance, sp, dp, copyOptions)
	}(srcProj, dstProj)

	go func(sp *gitlab.Project, dp *gitlab.Project) {
		defer wg.Done()
		errorChan <- sourceGitlabInstance.CopyProjectAvatar(destinationGitlabInstance, dp, sp)
	}(srcProj, dstProj)

	if copyOptions.CI_CD_Catalog {
		wg.Add(1)
		go func(dp *gitlab.Project) {
			defer wg.Done()
			errorChan <- destinationGitlabInstance.AddProjectToCICDCatalog(dp)
		}(dstProj)
	}

	if copyOptions.MirrorIssues {
		wg.Add(1)
		go func(sp *gitlab.Project, dp *gitlab.Project) {
			defer wg.Done()
			allErrors := destinationGitlabInstance.MirrorIssues(sourceGitlabInstance, sp, dp)
			for _, err := range allErrors {
				if err != nil {
					errorChan <- fmt.Errorf("failed to mirror issues from %s to %s: %v", sp.HTTPURLToRepo, dp.HTTPURLToRepo, err)
				}
			}
		}(srcProj, dstProj)
	}

	// Wait for git duplication to finish
	wg.Wait()

	allErrors := []error{}
	if copyOptions.MirrorReleases {
		wg.Add(1)
		go func(sp *gitlab.Project, dp *gitlab.Project) {
			defer wg.Done()
			allErrors = destinationGitlabInstance.MirrorReleases(sourceGitlabInstance, sp, dp)
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

// SyncProjectAttributes updates the destination project with settings from the source project.
// It checks if any diverged project data exists and if so, it overwrites it.
func (destinationGitlabInstance *GitlabInstance) SyncProjectAttributes(sourceProject *gitlab.Project, destinationProject *gitlab.Project, copyOptions *utils.MirroringOptions) error {
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

func (destinationGitlabInstance *GitlabInstance) MirrorProjectGit(sourceGitlabInstance *GitlabInstance, sourceProject *gitlab.Project, destinationProject *gitlab.Project, mirrorOptions *utils.MirroringOptions) error {
	if destinationGitlabInstance.PullMirrorAvailable {
		return destinationGitlabInstance.EnableProjectMirrorPull(sourceProject, destinationProject, mirrorOptions)
	}
	return helpers.MirrorRepo(sourceProject.HTTPURLToRepo, destinationProject.HTTPURLToRepo, sourceGitlabInstance.GitAuth, destinationGitlabInstance.GitAuth)
}

// EnableProjectMirrorPull enables the pull mirror for a project in the destination GitLab instance.
// It sets the source project URL, enables mirroring, and configures other options like triggering builds and overwriting diverged branches.
func (g *GitlabInstance) EnableProjectMirrorPull(sourceProject *gitlab.Project, destinationProject *gitlab.Project, mirrorOptions *utils.MirroringOptions) error {
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

// CopyProjectAvatar copies the avatar from the source project to the destination project.
// It first checks if the destination project already has an avatar set. If not, it downloads the avatar from the source project
// and uploads it to the destination project.
// The avatar is saved with a unique filename based on the current timestamp.
// The function returns an error if any step fails, including downloading or uploading the avatar.
func (sourceGitlabInstance *GitlabInstance) CopyProjectAvatar(destinationGitlabInstance *GitlabInstance, destinationProject *gitlab.Project, sourceProject *gitlab.Project) error {
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

// AddProjectToCICDCatalog adds a project to the CI/CD catalog in the destination GitLab instance.
// It uses a GraphQL mutation to create the catalog resource for the project.
func (g *GitlabInstance) AddProjectToCICDCatalog(project *gitlab.Project) error {
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

// ClaimOwnershipToProject adds the authenticated user as an owner to the specified project.
// It uses the GitLab API to add the user as a project member with owner access level.
func (g *GitlabInstance) ClaimOwnershipToProject(project *gitlab.Project) error {
	zap.L().Debug("Claiming ownership of project", zap.String("project", project.PathWithNamespace), zap.Int("userID", g.UserID))

	_, _, err := g.Gitlab.ProjectMembers.AddProjectMember(project.ID, &gitlab.AddProjectMemberOptions{
		UserID:      &g.UserID,
		AccessLevel: gitlab.Ptr(gitlab.AccessLevelValue(50)),
	})
	if err != nil {
		return fmt.Errorf("failed to add user as owner to project %s: %w", project.PathWithNamespace, err)
	}

	zap.L().Info("Successfully claimed ownership of project", zap.String("project", project.PathWithNamespace))
	return nil
}
