package mirroring

import (
	"path/filepath"
	"sync"

	"gitlab-sync/internal/utils"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

// fetchAndProcessProjects retrieves all projects that match the filters from the GitLab instance and stores them in the instance cache.
// It also updates the mirror mapping with the corresponding group creation options.
//
// The function is run in a goroutine for each project, and a wait group is used to wait for all goroutines to finish.
func (g *GitlabInstance) fetchAndProcessProjects(projectFilters *map[string]struct{}, groupFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) error {
	if !g.isBig() {
		return g.fetchAndProcessProjectsSmallInstance(projectFilters, groupFilters, mirrorMapping)
	}
	return g.fetchAndProcessProjectsBigInstance(projectFilters, mirrorMapping)
}

// storeProject stores the project in the Gitlab instance projects cache
// and updates the mirror mapping with the corresponding group creation options.
func (g *GitlabInstance) storeProject(project *gitlab.Project, parentGroupPath string, mirrorMapping *utils.MirrorMapping) {
	// Add the project to the gitlab instance projects cache
	g.addProject(project)

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
			Issues:              groupCreationOptions.Issues,
			MirrorTriggerBuilds: groupCreationOptions.MirrorTriggerBuilds,
			Visibility:          groupCreationOptions.Visibility,
			MirrorReleases:      groupCreationOptions.MirrorReleases,
		})
	}
}

// ===========================================================================
//                         SMALL INSTANCE FUNCTIONS                         //
// ===========================================================================

// fetchAndProcessProjectsSmallInstance retrieves all projects from the small GitLab instance
// and processes them to store in the instance cache.
func (g *GitlabInstance) fetchAndProcessProjectsSmallInstance(projectFilters *map[string]struct{}, groupFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) error {
	allProjects, err := g.fetchAllProjectsSmallInstance()
	if err != nil {
		if len(allProjects) == 0 {
			return err
		} else {
			zap.L().Warn("Failed to fetch all projects from GitLab instance", zap.String(ROLE, g.Role), zap.Error(err))
		}
	}
	g.processProjectsSmallInstance(allProjects, projectFilters, groupFilters, mirrorMapping)
	zap.L().Debug("Found matching projects in the GitLab instance", zap.String(ROLE, g.Role), zap.Int("projects", len(g.Projects)))
	return nil
}

// fetchAllProjectsSmallInstance retrieves all projects from the small GitLab instance
func (g *GitlabInstance) fetchAllProjectsSmallInstance() ([]*gitlab.Project, error) {
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

			group, matches := g.checkPathMatchesFilters(project.PathWithNamespace, projectFilters, groupFilters)
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

// fetchAndProcessProjectsBigInstance retrieves all projects individually from the large GitLab instance
// and processes them to store in the instance cache.
//
// It uses goroutines to fetch each project in parallel and a wait group to wait for all goroutines to finish.
// It returns an error if any of the goroutines fail.
func (g *GitlabInstance) fetchAndProcessProjectsBigInstance(projectFilters *map[string]struct{}, mirrorMapping *utils.MirrorMapping) error {
	// Fetch each project in parallel
	var wg sync.WaitGroup
	projectsChan := make(chan *gitlab.Project, len(*projectFilters))
	errCh := make(chan error)
	wg.Add(len(*projectFilters))
	for project := range *projectFilters {
		go func(projectPath string) {
			defer wg.Done()
			p, _, err := g.Gitlab.Projects.GetProject(projectPath, &gitlab.GetProjectOptions{})
			if err != nil {
				errCh <- err
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
	return utils.MergeErrors(errCh, 2)
}

// fetchAndProcessGroupProjects retrieves all projects from the group and processes them to store in the instance cache.
func (g *GitlabInstance) fetchAndProcessGroupProjects(group *gitlab.Group, fetchOriginPath string, mirrorMapping *utils.MirrorMapping, errChan chan error, wg *sync.WaitGroup) {
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
				errChan <- err
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
