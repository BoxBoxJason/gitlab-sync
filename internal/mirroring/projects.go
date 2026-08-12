package mirroring

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/boxboxjason/gitlab-sync/internal/utils"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
	"go.uber.org/zap"
)

const (
	projectsPerPage         = 100
	updateProjectBaseTasks  = 3
	updateProjectErrorLimit = 5
	projectOwnerAccessLevel = 50
)

// ===========================================================================
//                          PROJECTS GET FUNCTIONS                          //
// ===========================================================================

// FetchAndProcessProjects retrieves all projects that match the filters from the GitLab instance and stores them in the instance cache.
// It also updates the mirror mapping with the corresponding group creation options.
//
// The function is run in a goroutine for each project, and a wait group is used to wait for all goroutines to finish.
func (g *GitlabInstance) FetchAndProcessProjects(projectFilters, groupFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) []error {
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
		// If the project is directly listed in the mirror mapping config,
		// its options are already set from the JSON; no group lookup needed.
		if _, ok := mirrorMapping.GetProject(project.PathWithNamespace); ok {
			return
		}

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
			ClaimOwnership:      groupCreationOptions.ClaimOwnership,
		})
	}
}

// ===========================================================================
//                         SMALL INSTANCE FUNCTIONS                         //
// ===========================================================================

// FetchAndProcessProjectsSmallInstance retrieves all projects from the small GitLab instance
// and processes them to store in the instance cache.
func (g *GitlabInstance) FetchAndProcessProjectsSmallInstance(projectFilters, groupFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) []error {
	allProjects, err := g.FetchAllProjectsSmallInstance()
	if err != nil {
		if len(allProjects) == 0 {
			return []error{err}
		} else {
			zap.L().Warn("Failed to fetch all projects from GitLab instance", zap.String(ROLE, g.Role), zap.Error(err))
		}
	}

	g.processProjectsSmallInstance(allProjects, projectFilters, groupFilters, mirrorMapping)
	zap.L().Debug("Found matching projects in the GitLab instance", zap.String(ROLE, g.Role), zap.Int("projects", g.ProjectsLen()))

	return nil
}

// FetchAllProjectsSmallInstance retrieves all projects from the small GitLab instance.
func (g *GitlabInstance) FetchAllProjectsSmallInstance() ([]*gitlab.Project, error) {
	zap.L().Debug("Fetching all projects from GitLab instance", zap.String(ROLE, g.Role))

	fetchOpts := &gitlab.ListProjectsOptions{
		Archived:             new(false),
		IncludeHidden:        new(false),
		IncludePendingDelete: new(false),
		ListOptions: gitlab.ListOptions{
			PerPage: projectsPerPage,
			Page:    1,
		},
	}

	var allProjects []*gitlab.Project

	for {
		projects, resp, err := g.Gitlab.Projects.ListProjects(fetchOpts)
		if err != nil {
			return allProjects, fmt.Errorf("failed to list projects: %w", err)
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
func (g *GitlabInstance) processProjectsSmallInstance(allProjects []*gitlab.Project, projectFilters, groupFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) {
	zap.L().Debug("Processing projects from GitLab instance", zap.String(INSTANCE_SIZE, g.InstanceSize), zap.String(ROLE, g.Role), zap.Int("projects", len(allProjects)))

	// Create a wait group to wait for all goroutines to finish
	var waitGroup sync.WaitGroup

	for _, project := range allProjects {
		waitGroup.Add(1)

		go func(project *gitlab.Project) {
			defer waitGroup.Done()

			group, matches := helpers.MatchPathAgainstFilters(project.PathWithNamespace, projectFilters, groupFilters)
			if matches {
				g.storeProject(project, group, mirrorMapping)
			}
		}(project)
	}

	waitGroup.Wait()
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
	var waitGroup sync.WaitGroup

	projectsChan := make(chan *gitlab.Project, len(*projectFilters))
	errCh := make(chan error, len(*projectFilters))
	waitGroup.Add(len(*projectFilters))

	for project := range *projectFilters {
		go func(projectPath string) {
			defer waitGroup.Done()

			projectDetails, _, err := g.Gitlab.Projects.GetProject(projectPath, &gitlab.GetProjectOptions{})
			if err != nil {
				errCh <- fmt.Errorf("failed to retrieve project %s: %w", projectPath, err)

				return
			}

			projectsChan <- projectDetails
		}(project)
	}

	waitGroup.Wait()
	close(errCh)
	close(projectsChan)

	for project := range projectsChan {
		g.storeProject(project, filepath.Dir(project.PathWithNamespace), mirrorMapping)
	}

	return helpers.MergeErrors(errCh)
}

// FetchAndProcessGroupProjects retrieves all projects from the group and processes them to store in the instance cache.
func (g *GitlabInstance) FetchAndProcessGroupProjects(group *gitlab.Group, fetchOriginPath string, mirrorMapping *utils.MirrorMapping, errChan chan error, wg *sync.WaitGroup) {
	defer wg.Done()

	if group != nil {
		// Retrieve all projects in the group
		opt := &gitlab.ListGroupProjectsOptions{
			Archived: new(false),
			ListOptions: gitlab.ListOptions{
				PerPage: projectsPerPage,
				Page:    1,
			},
		}

		for {
			projects, resp, err := g.Gitlab.Groups.ListGroupProjects(group.ID, opt)
			if err != nil {
				errChan <- fmt.Errorf("failed to retrieve projects for group %s: %w", group.Name, err)
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
	var creationWaitGroup sync.WaitGroup

	// Create a channel to collect errors
	projectsSnapshot := mirrorMapping.ProjectsSnapshot()
	errorChan := make(chan error, len(projectsSnapshot))

	for sourceProjectPath, destinationProjectOptions := range projectsSnapshot {
		zap.L().Debug("Mirroring project", zap.String(ROLE_SOURCE, sourceProjectPath), zap.String(ROLE_DESTINATION, destinationProjectOptions.DestinationPath))
		// Retrieve the corresponding source project path
		sourceProject := sourceGitlab.GetProject(sourceProjectPath)
		if sourceProject == nil {
			errorChan <- fmt.Errorf("project %s not found in source GitLab instance (internal error, please review script)", sourceProjectPath)

			continue
		}

		creationWaitGroup.Add(1)
		// Create a goroutine to handle the project creation
		go func(sourcePath string, destinationCopyOptions *utils.MirroringOptions) {
			defer creationWaitGroup.Done()

			_, err := destinationGitlab.CreateProject(sourcePath, destinationCopyOptions, sourceGitlab)
			if err != nil {
				errorChan <- fmt.Errorf("failed to create project %s in destination GitLab instance: %v", destinationCopyOptions.DestinationPath, err)
			}
		}(sourceProjectPath, destinationProjectOptions)
	}

	// Wait for all goroutines to finish & close the error channel
	creationWaitGroup.Wait()
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

	sourceProject := sourceGitlab.GetProject(sourceProjectPath)
	if sourceProject == nil {
		return nil, []error{fmt.Errorf("project %s not found in source GitLab instance (internal error, please review script)", sourceProjectPath)}
	}

	// Check if the project already exists in the destination GitLab instance
	// If it does not exist, create it
	if destinationProject == nil {
		destinationProject, err = destinationGitlab.CreateProjectFromSource(sourceProject, projectCreationOptions)
		if err != nil || destinationProject == nil {
			return nil, []error{fmt.Errorf("failed to create project %s in destination GitLab instance: %w", destinationProjectPath, err)}
		}
	}

	// Reassert ownership on every run, not just when the project is first created.
	// GitLab's pull mirror endpoint requires the calling user to have Maintainer+ access to
	// reassign the mirror to itself, so re-claiming ownership here is what allows changing
	// the user running the script on an already-mirrored project.
	if helpers.Deref(projectCreationOptions.ClaimOwnership, false) {
		if ownershipErr := destinationGitlab.ClaimOwnershipToProject(destinationProject); ownershipErr != nil {
			zap.L().Warn("Failed to claim ownership of project", zap.String("project", destinationProject.PathWithNamespace), zap.Error(ownershipErr))
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
		MirrorTriggerBuilds: copyOptions.MirrorTriggerBuilds,
		Mirror:              new(true),
		Visibility:          new(gitlab.VisibilityValue(helpers.Deref(copyOptions.Visibility, string(gitlab.PublicVisibility)))),
	}

	zap.L().Debug("Retrieving project namespace ID", zap.String(ROLE_DESTINATION, copyOptions.DestinationPath))

	// Get the parent namespace ID for the project
	// This is used to set the namespace ID for the project
	parentNamespaceId, err := g.GetParentNamespaceID(copyOptions.DestinationPath)
	if err != nil {
		return nil, err
	}

	if parentNamespaceId >= 0 {
		projectCreationArgs.NamespaceID = &parentNamespaceId
	}

	// Create the project in the destination GitLab instance
	zap.L().Debug("Creating project in GitLab Instance", zap.String(ROLE, ROLE_DESTINATION), zap.String(ROLE_DESTINATION, copyOptions.DestinationPath))

	destinationProject, _, err := g.Gitlab.Projects.CreateProject(projectCreationArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to create project %s: %w", copyOptions.DestinationPath, err)
	}

	zap.L().Info("Project created", zap.String("project", destinationProject.HTTPURLToRepo))
	g.AddProject(destinationProject)

	return destinationProject, nil
}

// enqueueOptionalProjectTasks enqueues optional tasks related to project creation, such as adding the project to the CI/CD catalog and mirroring issues.
// It uses goroutines to perform these tasks concurrently and a wait group to wait for their completion.
func enqueueOptionalProjectTasks(
	destinationGitlabInstance *GitlabInstance,
	sourceGitlabInstance *GitlabInstance,
	sourceProject *gitlab.Project,
	destinationProject *gitlab.Project,
	copyOptions *utils.MirroringOptions,
	errorChannel chan error,
	waitGroup *sync.WaitGroup,
) {
	if helpers.Deref(copyOptions.CI_CD_Catalog, false) {
		waitGroup.Add(1)

		go func(project *gitlab.Project) {
			defer waitGroup.Done()

			errorChannel <- destinationGitlabInstance.AddProjectToCICDCatalog(project)
		}(destinationProject)
	}

	if helpers.Deref(copyOptions.MirrorIssues, false) {
		waitGroup.Add(1)

		go func(sourceProj, destinationProj *gitlab.Project) {
			defer waitGroup.Done()

			allErrors := destinationGitlabInstance.MirrorIssues(sourceGitlabInstance, sourceProj, destinationProj)
			nonNilErrors := make([]error, 0, len(allErrors))

			for _, currentErr := range allErrors {
				if currentErr != nil {
					nonNilErrors = append(nonNilErrors, currentErr)
				}
			}

			if len(nonNilErrors) == 0 {
				return
			}

			errorChannel <- fmt.Errorf(
				"failed to mirror issues from %s to %s: %w",
				sourceProj.HTTPURLToRepo,
				destinationProj.HTTPURLToRepo,
				errors.Join(nonNilErrors...),
			)
		}(sourceProject, destinationProject)
	}
}

// ===========================================================================
//                          PROJECTS PUT FUNCTIONS                          //
// ===========================================================================

// UpdateProjectFromSource updates the destination project with settings from the source project.
// It enables the project mirror pull, copies the project avatar, and optionally adds the project to the CI/CD catalog.
// It also mirrors releases if the option is set.
// The function uses goroutines to perform these tasks concurrently and waits for all of them to finish.
func (destinationGitlabInstance *GitlabInstance) UpdateProjectFromSource(sourceGitlabInstance *GitlabInstance, sourceProject, destinationProject *gitlab.Project, copyOptions *utils.MirroringOptions) []error {
	// Immediately capture pointers in local variables to avoid any late overrides
	srcProj := sourceProject

	dstProj := destinationProject
	if srcProj == nil || dstProj == nil {
		return []error{errors.New("source or destination project is nil")}
	}

	waitGroup := sync.WaitGroup{}
	waitGroup.Add(updateProjectBaseTasks)

	errorChannel := make(chan error, updateProjectErrorLimit)

	go func(sourceProj, destinationProj *gitlab.Project) {
		defer waitGroup.Done()

		errorChannel <- destinationGitlabInstance.SyncProjectAttributes(sourceProj, destinationProj, copyOptions)
	}(srcProj, dstProj)

	go func(sourceProj, destinationProj *gitlab.Project) {
		defer waitGroup.Done()

		errorChannel <- destinationGitlabInstance.MirrorProjectGit(sourceGitlabInstance, sourceProj, destinationProj, copyOptions)
	}(srcProj, dstProj)

	go func(sourceProj, destinationProj *gitlab.Project) {
		defer waitGroup.Done()

		errorChannel <- sourceGitlabInstance.CopyProjectAvatar(destinationGitlabInstance, destinationProj, sourceProj)
	}(srcProj, dstProj)

	enqueueOptionalProjectTasks(destinationGitlabInstance, sourceGitlabInstance, srcProj, dstProj, copyOptions, errorChannel, &waitGroup)

	// Wait for git duplication to finish
	waitGroup.Wait()

	allErrors := []error{}

	if helpers.Deref(copyOptions.MirrorReleases, false) {
		waitGroup.Add(1)

		go func(sourceProj, destinationProj *gitlab.Project) {
			defer waitGroup.Done()

			allErrors = destinationGitlabInstance.MirrorReleases(sourceGitlabInstance, sourceProj, destinationProj)
		}(srcProj, dstProj)
	}

	waitGroup.Wait()
	close(errorChannel)

	for currentErr := range errorChannel {
		if currentErr != nil {
			allErrors = append(allErrors, currentErr)
		}
	}

	return allErrors
}

// SyncProjectAttributes updates the destination project with settings from the source project.
// It checks if any diverged project data exists and if so, it overwrites it.
func syncStandardProjectAttributes(sourceProject, destinationProject *gitlab.Project, gitlabEditOptions *gitlab.EditProjectOptions) bool {
	mismatch := false

	if sourceProject.Name != destinationProject.Name {
		gitlabEditOptions.Name = &sourceProject.Name
		mismatch = true
	}

	if sourceProject.Description != destinationProject.Description {
		gitlabEditOptions.Description = &sourceProject.Description
		mismatch = true
	}

	if sourceProject.DefaultBranch != destinationProject.DefaultBranch {
		gitlabEditOptions.DefaultBranch = &sourceProject.DefaultBranch
		mismatch = true
	}

	if !utils.StringArraysMatchValues(sourceProject.Topics, destinationProject.Topics) {
		gitlabEditOptions.Topics = &sourceProject.Topics
		mismatch = true
	}

	return mismatch
}

func syncMirrorProjectAttributes(destinationProject *gitlab.Project, copyOptions *utils.MirroringOptions, gitlabEditOptions *gitlab.EditProjectOptions) bool {
	mismatch := false

	desiredMirrorTriggerBuilds := helpers.Deref(copyOptions.MirrorTriggerBuilds, false)
	if desiredMirrorTriggerBuilds != destinationProject.MirrorTriggerBuilds {
		gitlabEditOptions.MirrorTriggerBuilds = new(desiredMirrorTriggerBuilds)
		mismatch = true
	}

	if !destinationProject.MirrorOverwritesDivergedBranches {
		gitlabEditOptions.MirrorOverwritesDivergedBranches = new(true)
		mismatch = true
	}

	if !destinationProject.Mirror {
		gitlabEditOptions.Mirror = new(true)
		mismatch = true
	}

	return mismatch
}

func (destinationGitlabInstance *GitlabInstance) SyncProjectAttributes(sourceProject, destinationProject *gitlab.Project, copyOptions *utils.MirroringOptions) error {
	zap.L().Debug("Checking if project requires attributes resync", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))

	gitlabEditOptions := &gitlab.EditProjectOptions{}

	missmatched := syncStandardProjectAttributes(sourceProject, destinationProject, gitlabEditOptions)
	if syncMirrorProjectAttributes(destinationProject, copyOptions, gitlabEditOptions) {
		missmatched = true
	}

	if copyOptions.Visibility != new(string(destinationProject.Visibility)) {
		visibilityValue := utils.ConvertVisibility(copyOptions.Visibility)
		gitlabEditOptions.Visibility = &visibilityValue
		missmatched = true
	}

	if missmatched {
		_, _, err := destinationGitlabInstance.Gitlab.Projects.EditProject(destinationProject.ID, gitlabEditOptions)
		if err != nil {
			return fmt.Errorf("failed to edit project %s: %w", destinationProject.HTTPURLToRepo, err)
		}

		zap.L().Debug("Project attributes resync completed", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))
	} else {
		zap.L().Debug("Project attributes are already in sync, skipping", zap.String(ROLE_SOURCE, sourceProject.HTTPURLToRepo), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))
	}

	return nil
}

func (destinationGitlabInstance *GitlabInstance) MirrorProjectGit(sourceGitlabInstance *GitlabInstance, sourceProject, destinationProject *gitlab.Project, mirrorOptions *utils.MirroringOptions) error {
	if destinationGitlabInstance.PullMirrorAvailable {
		return destinationGitlabInstance.EnableProjectMirrorPull(sourceProject, destinationProject, mirrorOptions)
	}

	err := helpers.MirrorRepo(sourceProject.HTTPURLToRepo, destinationProject.HTTPURLToRepo, sourceGitlabInstance.GitAuth, destinationGitlabInstance.GitAuth)
	if err != nil {
		return fmt.Errorf("failed to mirror repository from %s to %s: %w", sourceProject.PathWithNamespace, destinationProject.PathWithNamespace, err)
	}

	return nil
}

// EnableProjectMirrorPull (re)configures the pull mirror for a project in the destination GitLab instance.
// It sets the source project URL, enables mirroring, and configures other options like triggering builds and overwriting diverged branches.
//
// This is called on every run, for every project (not just newly created ones): GitLab's pull
// mirror update endpoint unconditionally reassigns the mirror's "running user" to whichever user
// makes this call, so calling it every time is what allows changing the user running the script.
// That reassignment only takes effect if the calling user has Maintainer+ access on the
// destination project, which is why ClaimOwnershipToProject also needs to run on every run.
func (g *GitlabInstance) EnableProjectMirrorPull(sourceProject, destinationProject *gitlab.Project, mirrorOptions *utils.MirroringOptions) error {
	zap.L().Debug("Reapplying project mirror pull", zap.String("sourceProject", sourceProject.HTTPURLToRepo), zap.String("destinationProject", destinationProject.HTTPURLToRepo))

	desiredMirrorTriggerBuilds := helpers.Deref(mirrorOptions.MirrorTriggerBuilds, false)

	_, _, err := g.Gitlab.Projects.ConfigureProjectPullMirror(destinationProject.ID, &gitlab.ConfigureProjectPullMirrorOptions{
		URL:                              &sourceProject.HTTPURLToRepo,
		OnlyMirrorProtectedBranches:      new(true),
		Enabled:                          new(true),
		MirrorOverwritesDivergedBranches: new(true),
		MirrorTriggerBuilds:              new(desiredMirrorTriggerBuilds),
	})
	if err != nil {
		return fmt.Errorf("failed to configure pull mirror for project %s: %w", destinationProject.PathWithNamespace, err)
	}

	return nil
}

// CopyProjectAvatar copies the avatar from the source project to the destination project.
// It first checks if the destination project already has an avatar set. If not, it downloads the avatar from the source project
// and uploads it to the destination project.
// The avatar is saved with a unique filename based on the current timestamp.
// The function returns an error if any step fails, including downloading or uploading the avatar.
func (sourceGitlabInstance *GitlabInstance) CopyProjectAvatar(destinationGitlabInstance *GitlabInstance, destinationProject, sourceProject *gitlab.Project) error {
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
		return fmt.Errorf("failed to download avatar for project %s: %w", sourceProject.HTTPURLToRepo, err)
	}

	// Upload the avatar to the destination project
	_, _, err = destinationGitlabInstance.Gitlab.Projects.UploadAvatar(destinationProject.ID, sourceProjectAvatar, fmt.Sprintf("avatar-%d.png", time.Now().Unix()))
	if err != nil {
		return fmt.Errorf("failed to upload avatar for project %s: %w", destinationProject.HTTPURLToRepo, err)
	}

	return nil
}

// AddProjectToCICDCatalog enables the CI/CD catalog resource for the project in the destination GitLab instance.
// It skips the API call if the project is already registered as a CI/CD catalog resource.
// Requires GitLab 19.3+ on the destination instance, since it relies on the "cicd_catalog_enabled" project API field introduced in that version.
func (g *GitlabInstance) AddProjectToCICDCatalog(project *gitlab.Project) error {
	if project.CICDCatalogEnabled {
		zap.L().Debug("Project is already part of the CI/CD catalog, skipping", zap.String("project", project.HTTPURLToRepo))

		return nil
	}

	zap.L().Debug("Adding project to CI/CD catalog", zap.String("project", project.HTTPURLToRepo))

	_, _, err := g.Gitlab.Projects.EditProject(project.ID, &gitlab.EditProjectOptions{CICDCatalogEnabled: new(true)})
	if err != nil {
		return fmt.Errorf("failed to add project %s to CI/CD catalog: %w", project.PathWithNamespace, err)
	}

	return nil
}

// ClaimOwnershipToProject ensures the authenticated user is a direct owner of the specified project.
//
// It is called on every run, so it must actively reassert ownership rather than silently
// no-op: AddProjectMember does not change the access level of a user who is already a
// member, so a user added at a lower level (or by a previous run under a different account)
// would otherwise never be promoted to Owner. It tries EditProjectMember first to upgrade an
// existing direct membership, falling back to AddProjectMember when the user isn't a direct
// member yet.
func (g *GitlabInstance) ClaimOwnershipToProject(project *gitlab.Project) error {
	zap.L().Debug("Claiming ownership of project", zap.String("project", project.PathWithNamespace), zap.Int64("userID", g.UserID))

	ownerAccessLevel := new(gitlab.AccessLevelValue(projectOwnerAccessLevel))

	_, _, editErr := g.Gitlab.ProjectMembers.EditProjectMember(project.ID, g.UserID, &gitlab.EditProjectMemberOptions{
		AccessLevel: ownerAccessLevel,
	})
	if editErr != nil {
		_, _, addErr := g.Gitlab.ProjectMembers.AddProjectMember(project.ID, &gitlab.AddProjectMemberOptions{
			UserID:      &g.UserID,
			AccessLevel: ownerAccessLevel,
		})
		if addErr != nil {
			return fmt.Errorf("failed to add or promote user as owner of project %s: %w", project.PathWithNamespace, errors.Join(editErr, addErr))
		}
	}

	zap.L().Info("Successfully claimed ownership of project", zap.String("project", project.PathWithNamespace))

	return nil
}
