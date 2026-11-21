package mirroring

import (
	"fmt"
	"sync"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.uber.org/zap"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

// CreateProjects creates GitLab projects in the destination GitLab instance based on the mirror mapping.
// It retrieves the source project path for each destination project and creates the project in the destination instance.
func (g *GitlabInstance) CreateProjects(sourceGitlab *GitlabInstance, mirrorMapping *mirrorcfg.MirrorMapping) {
	zap.L().Info("Creating projects in GitLab Instance", zap.String(RoleKey, RoleDestination))

	// Create a wait group to wait for all goroutines to finish
	var creationWaitGroup sync.WaitGroup

	projectsSnapshot := mirrorMapping.ProjectsSnapshot()

	for sourceProjectPath, destinationProjectOptions := range projectsSnapshot {
		zap.L().Debug("Mirroring project", zap.String(RoleSource, sourceProjectPath), zap.String(RoleDestination, destinationProjectOptions.DestinationPath))
		// Retrieve the corresponding source project path
		sourceProject := sourceGitlab.GetProject(sourceProjectPath)
		if sourceProject == nil {
			helpers.ReportBlocking(fmt.Errorf("project %s not found in source GitLab instance (internal error, please review script)", sourceProjectPath))

			continue
		}

		creationWaitGroup.Add(1)
		// Create a goroutine to handle the project creation
		go func(sourcePath string, destinationCopyOptions *mirrorcfg.MirroringOptions) {
			defer creationWaitGroup.Done()

			g.CreateProject(sourcePath, destinationCopyOptions, sourceGitlab)
		}(sourceProjectPath, destinationProjectOptions)
	}

	// Wait for all goroutines to finish
	creationWaitGroup.Wait()
}

// CreateProject creates a GitLab project in the destination GitLab instance based on the source project and mirror mapping.
// It checks if the project already exists in the destination instance and creates it if not.
// The function also handles the copying of project avatars from the source to the destination instance.
func (g *GitlabInstance) CreateProject(sourceProjectPath string, projectCreationOptions *mirrorcfg.MirroringOptions, sourceGitlab *GitlabInstance) *gitlab.Project {
	destinationProjectPath := projectCreationOptions.DestinationPath
	// Check if the project already exists
	destinationProject := g.GetProject(destinationProjectPath)

	var err error

	sourceProject := sourceGitlab.GetProject(sourceProjectPath)
	if sourceProject == nil {
		helpers.ReportBlocking(fmt.Errorf("project %s not found in source GitLab instance (internal error, please review script)", sourceProjectPath))

		return nil
	}

	// Check if the project already exists in the destination GitLab instance
	// If it does not exist, create it
	if destinationProject == nil {
		destinationProject, err = g.CreateProjectFromSource(sourceProject, projectCreationOptions)
		if err != nil || destinationProject == nil {
			helpers.ReportBlocking(fmt.Errorf("failed to create project %s in destination GitLab instance: %w", destinationProjectPath, err))

			return nil
		}
	}

	// Reassert ownership on every run, not just when the project is first created.
	// GitLab's pull mirror endpoint requires the calling user to have Maintainer+ access to
	// reassign the mirror to itself, so re-claiming ownership here is what allows changing
	// the user running the script on an already-mirrored project.
	if helpers.Deref(projectCreationOptions.ClaimOwnership, false) {
		ownershipErr := g.ClaimOwnershipToProject(destinationProject)
		if ownershipErr != nil {
			zap.L().Warn("Failed to claim ownership of project", zap.String("project", destinationProject.PathWithNamespace), zap.Error(ownershipErr))
		}
	}

	// If the project already exists, update it with the source project details
	g.UpdateProjectFromSource(sourceGitlab, sourceProject, destinationProject, projectCreationOptions)

	zap.L().Info("Completed project mirroring", zap.String(RoleSource, sourceProjectPath), zap.String(RoleDestination, destinationProjectPath))

	return destinationProject
}

// CreateProjectFromSource creates a GitLab project in the destination GitLab instance based on the source project.
// It sets the project name, path, default branch, description, and visibility based on the source project.
// The function also handles the setting of the namespace ID for the project.
// It returns the created project or an error if the creation fails.
func (g *GitlabInstance) CreateProjectFromSource(sourceProject *gitlab.Project, copyOptions *mirrorcfg.MirroringOptions) (*gitlab.Project, error) {
	// Define the API call logic.
	//
	// Mirror is deliberately not set here: GitLab only accepts `mirror=true`
	// together with an import URL and a mirror user, neither of which exists yet at
	// creation time. Pull mirroring is configured right after by
	// EnableProjectMirrorPull, and only when the destination supports it.
	projectCreationArgs := &gitlab.CreateProjectOptions{
		Name:          &sourceProject.Name,
		Path:          &sourceProject.Path,
		DefaultBranch: &sourceProject.DefaultBranch,
		Description:   &sourceProject.Description,
		Visibility:    new(gitlab.VisibilityValue(helpers.Deref(copyOptions.Visibility, string(gitlab.PublicVisibility)))),
	}

	if g.PullMirrorAvailable {
		projectCreationArgs.MirrorTriggerBuilds = copyOptions.MirrorTriggerBuilds
	}

	zap.L().Debug("Retrieving project namespace ID", zap.String(RoleDestination, copyOptions.DestinationPath))

	// Get the parent namespace ID for the project
	// This is used to set the namespace ID for the project
	parentNamespaceID, err := g.EnsureParentNamespaceID(copyOptions.DestinationPath, *projectCreationArgs.Visibility)
	if err != nil {
		return nil, err
	}

	if parentNamespaceID >= 0 {
		projectCreationArgs.NamespaceID = &parentNamespaceID
	}

	// Create the project in the destination GitLab instance
	zap.L().Debug("Creating project in GitLab Instance", zap.String(RoleKey, RoleDestination), zap.String(RoleDestination, copyOptions.DestinationPath))

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
	copyOptions *mirrorcfg.MirroringOptions,
	waitGroup *sync.WaitGroup,
) {
	if helpers.Deref(copyOptions.CICDCatalog, false) {
		waitGroup.Add(1)

		go func(project *gitlab.Project) {
			defer waitGroup.Done()

			helpers.Report(destinationGitlabInstance.AddProjectToCICDCatalog(project))
		}(destinationProject)
	}

	if helpers.Deref(copyOptions.MirrorIssues, false) {
		waitGroup.Add(1)

		go func(sourceProj, destinationProj *gitlab.Project) {
			defer waitGroup.Done()

			destinationGitlabInstance.MirrorIssues(sourceGitlabInstance, sourceProj, destinationProj)
		}(sourceProject, destinationProject)
	}
}
