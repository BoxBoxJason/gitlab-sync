package mirroring

import (
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func (g *GitlabInstance) createProjectFromSource(sourceProject *gitlab.Project, destinationPath string) error {
	projectCreationArgs := &gitlab.CreateProjectOptions{
		Name:                &sourceProject.Name,
		Path:                &sourceProject.Path,
		DefaultBranch:       &sourceProject.DefaultBranch,
		Description:         &sourceProject.Description,
		MirrorTriggerBuilds: gitlab.Ptr(true),
		Mirror:              gitlab.Ptr(true),
		Topics:              &sourceProject.Topics,
		Visibility:          &sourceProject.Visibility,
	}

	destinationProject, _, err := g.Gitlab.Projects.CreateProject(projectCreationArgs)
	if err != nil {
		return err
	}
	g.addProject(destinationPath, destinationProject)

	err = g.enableProjectMirrorPull(sourceProject, destinationProject)

	return err
}

func (g *GitlabInstance) createGroupFromSource(sourceGroup *gitlab.Group, destinationPath string) error {
	groupCreationArgs := &gitlab.CreateGroupOptions{
		Name:          &sourceGroup.Name,
		Path:          &sourceGroup.Path,
		Description:   &sourceGroup.Description,
		Visibility:    &sourceGroup.Visibility,
		DefaultBranch: &sourceGroup.DefaultBranch,
	}

	parentGroupID, err := g.getParentGroupID(destinationPath)
	if err != nil {
		return err
	} else if parentGroupID >= 0 {
		groupCreationArgs.ParentID = &parentGroupID
	}

	destinationGroup, _, err := g.Gitlab.Groups.CreateGroup(groupCreationArgs)
	if err == nil {
		g.addGroup(destinationPath, destinationGroup)
	}
	return err
}
