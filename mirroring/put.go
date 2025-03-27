package mirroring

import (
	"fmt"

	"github.com/boxboxjason/gitlab-sync/graphql"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func (g *GitlabInstance) enableProjectMirrorPull(sourceProject *gitlab.Project, destinationProject *gitlab.Project) error {
	_, _, err := g.Gitlab.Projects.ConfigureProjectPullMirror(destinationProject.ID, &gitlab.ConfigureProjectPullMirrorOptions{
		URL:                              &sourceProject.HTTPURLToRepo,
		OnlyMirrorProtectedBranches:      gitlab.Ptr(true),
		Enabled:                          gitlab.Ptr(true),
		MirrorOverwritesDivergedBranches: gitlab.Ptr(true),
		MirrorTriggerBuilds:              gitlab.Ptr(true),
	})
	return err
}

func (g *GitlabInstance) addProjectToCICDCatalog(project *gitlab.Project) error {
	_, err := g.GraphQLClient.SendRequest(&graphql.GraphQLRequest{
		Query: fmt.Sprintf(`mutation {
			catalogResourcesCreate(input: { projectPath: "gid://gitlab/Project/%d"})
		}`, project.ID),
	}, "POST")
	return err
}
