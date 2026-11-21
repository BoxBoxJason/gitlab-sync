package mirroring

import (
	"fmt"
	"path/filepath"
	"sync"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.uber.org/zap"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

// FetchAndProcessProjects retrieves all projects that match the filters from the GitLab instance and stores them in the instance cache.
// It also updates the mirror mapping with the corresponding group creation options.
//
// The function is run in a goroutine for each project, and a wait group is used to wait for all goroutines to finish.
func (g *GitlabInstance) FetchAndProcessProjects(projectFilters, groupFilters map[string]struct{}, mirrorMapping *mirrorcfg.MirrorMapping) {
	zap.L().Debug("Fetching and processing projects from GitLab instance", zap.String(RoleKey, g.Role), zap.String(InstanceSizeKey, g.InstanceSize), zap.Int("projects", len(projectFilters)), zap.Int("groups", len(groupFilters)))

	if !g.IsBig() {
		g.FetchAndProcessProjectsSmallInstance(projectFilters, groupFilters, mirrorMapping)

		return
	}

	g.FetchAndProcessProjectsBigInstance(projectFilters, mirrorMapping)
}

// storeProject stores the project in the Gitlab instance projects cache
// and updates the mirror mapping with the corresponding group creation options.
func (g *GitlabInstance) storeProject(project *gitlab.Project, parentGroupPath string, mirrorMapping *mirrorcfg.MirrorMapping) {
	// Add the project to the gitlab instance projects cache
	g.AddProject(project)

	if g.Role == RoleSource {
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
		mirrorMapping.AddProject(project.PathWithNamespace, &mirrorcfg.MirroringOptions{
			DestinationPath:     filepath.Join(groupCreationOptions.DestinationPath, relativePath),
			CICDCatalog:         groupCreationOptions.CICDCatalog,
			MirrorIssues:        groupCreationOptions.MirrorIssues,
			MirrorTriggerBuilds: groupCreationOptions.MirrorTriggerBuilds,
			Visibility:          groupCreationOptions.Visibility,
			MirrorReleases:      groupCreationOptions.MirrorReleases,
			MirrorReleaseAssets: groupCreationOptions.MirrorReleaseAssets,
			ClaimOwnership:      groupCreationOptions.ClaimOwnership,
		})
	}
}

// FetchAndProcessProjectsSmallInstance retrieves all projects from the small GitLab instance
// and processes them to store in the instance cache.
func (g *GitlabInstance) FetchAndProcessProjectsSmallInstance(projectFilters, groupFilters map[string]struct{}, mirrorMapping *mirrorcfg.MirrorMapping) {
	allProjects, err := g.FetchAllProjectsSmallInstance()
	if err != nil {
		if len(allProjects) == 0 {
			helpers.Report(err)

			return
		}

		zap.L().Warn("Failed to fetch all projects from GitLab instance", zap.String(RoleKey, g.Role), zap.Error(err))
	}

	g.processProjectsSmallInstance(allProjects, projectFilters, groupFilters, mirrorMapping)
	zap.L().Debug("Found matching projects in the GitLab instance", zap.String(RoleKey, g.Role), zap.Int("projects", g.ProjectsLen()))
}

// FetchAllProjectsSmallInstance retrieves all projects from the small GitLab instance.
func (g *GitlabInstance) FetchAllProjectsSmallInstance() ([]*gitlab.Project, error) {
	zap.L().Debug("Fetching all projects from GitLab instance", zap.String(RoleKey, g.Role))

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
func (g *GitlabInstance) processProjectsSmallInstance(allProjects []*gitlab.Project, projectFilters, groupFilters map[string]struct{}, mirrorMapping *mirrorcfg.MirrorMapping) {
	zap.L().Debug("Processing projects from GitLab instance", zap.String(InstanceSizeKey, g.InstanceSize), zap.String(RoleKey, g.Role), zap.Int("projects", len(allProjects)))

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

// FetchAndProcessProjectsBigInstance retrieves all projects individually from the large GitLab instance
// and processes them to store in the instance cache.
//
// It uses goroutines to fetch each project in parallel and a wait group to wait for all goroutines to finish.
// It returns an error if any of the goroutines fail.
func (g *GitlabInstance) FetchAndProcessProjectsBigInstance(projectFilters map[string]struct{}, mirrorMapping *mirrorcfg.MirrorMapping) {
	// Fetch each project in parallel
	var waitGroup sync.WaitGroup

	projectsChan := make(chan *gitlab.Project, len(projectFilters))
	waitGroup.Add(len(projectFilters))

	for project := range projectFilters {
		go func(projectPath string) {
			defer waitGroup.Done()

			projectDetails, _, err := g.Gitlab.Projects.GetProject(projectPath, &gitlab.GetProjectOptions{})
			if err != nil {
				helpers.Report(fmt.Errorf("failed to retrieve project %s: %w", projectPath, err))

				return
			}

			projectsChan <- projectDetails
		}(project)
	}

	waitGroup.Wait()
	close(projectsChan)

	for project := range projectsChan {
		g.storeProject(project, filepath.Dir(project.PathWithNamespace), mirrorMapping)
	}
}

// FetchAndProcessGroupProjects retrieves all projects from the group and processes them to store in the instance cache.
func (g *GitlabInstance) FetchAndProcessGroupProjects(group *gitlab.Group, fetchOriginPath string, mirrorMapping *mirrorcfg.MirrorMapping, wg *sync.WaitGroup) {
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
				helpers.Report(fmt.Errorf("failed to retrieve projects for group %s: %w", group.Name, err))
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
