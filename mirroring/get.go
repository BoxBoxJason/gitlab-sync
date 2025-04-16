package mirroring

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"

	"gitlab-sync/utils"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func (g *GitlabInstance) fetchProjects(projectFilters *map[string]bool, groupFilters *map[string]bool, mirrorMapping *utils.MirrorMapping, isSource bool) error {
	sourceString := "source"
	if !isSource {
		sourceString = "destination"
	}
	utils.LogVerbosef("Fetching all projects from %s GitLab instance", sourceString)
	projects, _, err := g.Gitlab.Projects.ListProjects(nil)
	if err != nil {
		return err
	}

	utils.LogVerbosef("Processing %d projects from %s GitLab instance", len(projects), sourceString)

	// Create a wait group to wait for all goroutines to finish
	var wg sync.WaitGroup

	// Create a channel to limit the number of concurrent goroutines
	concurrencyLimit := 10
	sem := make(chan struct{}, concurrencyLimit)

	for _, project := range projects {
		wg.Add(1)
		// Acquire a token from the semaphore
		sem <- struct{}{}

		go func(project *gitlab.Project) {
			defer wg.Done()
			// Release the token back to the semaphore
			defer func() { <-sem }()

			// Check if the project matches the filters:
			//   - either is in the projects map
			//   - or path starts with any of the groups in the groups map
			if _, ok := (*projectFilters)[project.PathWithNamespace]; ok {
				g.addProject(project.PathWithNamespace, project)
			} else {
				for group := range *groupFilters {
					if strings.HasPrefix(project.PathWithNamespace, group) {
						// Add the project to the gitlab instance projects cache
						g.addProject(project.PathWithNamespace, project)

						if isSource {
							// Retrieve the corresponding group creation options from the mirror mapping
							groupCreationOptions, ok := mirrorMapping.Groups[group]
							if !ok {
								log.Fatalf("Group %s not found in mirror mapping (internal error, please review script)", group)
							}

							// Calculate the relative path between the project and the group
							relativePath, err := filepath.Rel(group, project.PathWithNamespace)
							if err != nil {
								log.Fatalf("Failed to calculate relative path for project %s: %s", project.PathWithNamespace, err)
							}

							// Add the project to the mirror mapping
							mirrorMapping.AddProject(project.PathWithNamespace, &utils.MirroringOptions{
								DestinationPath:     filepath.Join(groupCreationOptions.DestinationPath, relativePath),
								CI_CD_Catalog:       groupCreationOptions.CI_CD_Catalog,
								Issues:              groupCreationOptions.Issues,
								MirrorTriggerBuilds: groupCreationOptions.MirrorTriggerBuilds,
								Visibility:          groupCreationOptions.Visibility,
								MirrorReleases:      groupCreationOptions.MirrorReleases,
							})
						}
						break
					}
				}
			}
		}(project)
	}

	wg.Wait()

	utils.LogVerbosef("Found %d projects to mirror in the %s GitLab instance", len(g.Projects), sourceString)

	return nil
}

func (g *GitlabInstance) fetchGroups(groupFilters *map[string]bool, mirrorMapping *utils.MirrorMapping, isSource bool) error {
	sourceString := "source"
	if !isSource {
		sourceString = "destination"
	}
	utils.LogVerbosef("Fetching all groups from %s GitLab instance", sourceString)
	groups, _, err := g.Gitlab.Groups.ListGroups(nil)
	if err != nil {
		return err
	}

	utils.LogVerbosef("Processing %d groups from %s GitLab instance", len(groups), sourceString)

	// Create a wait group to wait for all goroutines to finish
	var wg sync.WaitGroup

	// Create a channel to limit the number of concurrent goroutines
	concurrencyLimit := 10
	sem := make(chan struct{}, concurrencyLimit)

	for _, group := range groups {
		wg.Add(1)
		// Acquire a token from the semaphore
		sem <- struct{}{}

		go func(group *gitlab.Group) {
			defer wg.Done()
			// Release the token back to the semaphore
			defer func() { <-sem }()

			// Check if the group matches the filters:
			//   - either is in the groups map
			//   - or path starts with any of the groups in the groups map
			//   - or is a subgroup of any of the groups in the groups map
			if _, ok := (*groupFilters)[group.FullPath]; ok {
				g.addGroup(group.FullPath, group)
			} else {
				for groupPath := range *groupFilters {
					if strings.HasPrefix(group.FullPath, groupPath) {
						// Add the group to the gitlab instance groups cache
						g.addGroup(group.FullPath, group)

						if isSource {
							// Retrieve the corresponding group creation options from the mirror mapping
							groupCreationOptions, ok := mirrorMapping.Groups[groupPath]
							if !ok {
								log.Fatalf("Group %s not found in mirror mapping (internal error, please review script)", groupPath)
							}

							// Calculate the relative path between the group and the groupPath
							relativePath, err := filepath.Rel(groupPath, group.FullPath)
							if err != nil {
								log.Fatalf("Failed to calculate relative path for group %s: %s", group.FullPath, err)
							}

							// Add the group to the mirror mapping
							mirrorMapping.AddGroup(group.FullPath, &utils.MirroringOptions{
								DestinationPath:     filepath.Join(groupCreationOptions.DestinationPath, relativePath),
								CI_CD_Catalog:       groupCreationOptions.CI_CD_Catalog,
								Issues:              groupCreationOptions.Issues,
								MirrorTriggerBuilds: groupCreationOptions.MirrorTriggerBuilds,
								Visibility:          groupCreationOptions.Visibility,
								MirrorReleases:      groupCreationOptions.MirrorReleases,
							})
						}
						break
					}
				}
			}
		}(group)
	}

	wg.Wait()

	utils.LogVerbosef("Found %d matching groups in %s GitLab instance", len(g.Groups), sourceString)

	return nil
}

func fetchAll(gitlabInstance *GitlabInstance, projectFilters map[string]bool, groupFilters map[string]bool, mirrorMapping *utils.MirrorMapping, isSource bool) error {
	wg := sync.WaitGroup{}
	errCh := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := gitlabInstance.fetchGroups(&groupFilters, mirrorMapping, isSource); err != nil {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		if err := gitlabInstance.fetchProjects(&projectFilters, &groupFilters, mirrorMapping, isSource); err != nil {
			errCh <- err
		}
	}()
	wg.Wait()
	close(errCh)

	return utils.MergeErrors(errCh, 2)
}

func (g *GitlabInstance) getParentNamespaceID(projectOrGroupPath string) (int, error) {
	parentGroupID := -1
	parentPath := filepath.Dir(projectOrGroupPath)
	var err error = nil
	if parentPath != "." && parentPath != "/" {
		// Check if parent path is already in the instance groups cache
		if parentGroup, ok := g.Groups[parentPath]; ok {
			parentGroupID = parentGroup.ID
		} else {
			err = fmt.Errorf("parent group not found for path: %s", parentPath)
		}
	}
	return parentGroupID, err
}
