package mirroring

import (
	"fmt"
	"path"
	"strings"

	"github.com/boxboxjason/gitlab-sync/utils"
)

func (g *GitlabInstance) fetchProjects(filters *utils.MirrorMapping) error {
	projects, _, err := g.Gitlab.Projects.ListProjects(nil)
	if err != nil {
		return err
	}

	for _, project := range projects {
		// Check if the project matches the filters:
		//   - either is in the projects map
		//   - or path starts with any of the groups in the groups map
		if _, ok := filters.Projects[project.PathWithNamespace]; ok {
			g.addProject(project.PathWithNamespace, project)
		} else {
			for group := range filters.Groups {
				if strings.HasPrefix(project.PathWithNamespace, group) {
					g.addProject(project.PathWithNamespace, project)
					break
				}
			}
		}
	}

	utils.LogVerbosef("Found %d projects to mirror in the source GitLab instance", len(g.Projects))

	return nil
}

func (g *GitlabInstance) fetchGroups(filters *utils.MirrorMapping) error {
	groups, _, err := g.Gitlab.Groups.ListGroups(nil)
	if err != nil {
		return err
	}

	for _, group := range groups {
		// Check if the group matches the filters:
		//   - either is in the groups map
		//   - or path starts with any of the groups in the groups map
		//   - or is a subgroup of any of the groups in the groups map
		if _, ok := filters.Groups[group.FullPath]; ok {
			g.addGroup(group.FullPath, group)
		} else {
			for groupPath := range filters.Groups {
				if strings.HasPrefix(group.FullPath, groupPath) {
					g.addGroup(group.FullPath, group)
					break
				}
			}
		}
	}

	utils.LogVerbosef("Found %d groups to mirror in the source GitLab instance", len(g.Groups))

	return nil
}

func (g *GitlabInstance) getParentGroupID(projectOrGroupPath string) (int, error) {
	parentGroupID := -1
	parentPath := path.Dir(projectOrGroupPath)
	var err error = nil
	if parentPath != "." {
		// Check if parent path is already in the instance groups cache
		if parentGroup, ok := g.Groups[parentPath]; ok {
			parentGroupID = parentGroup.ID
		} else {
			err = fmt.Errorf("parent group not found for path: %s", parentPath)
		}
	}
	return parentGroupID, err
}
