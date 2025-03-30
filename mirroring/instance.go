package mirroring

import (
	"sync"

	"github.com/boxboxjason/gitlab-sync/utils"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

type GitlabInstance struct {
	Gitlab        *gitlab.Client
	Projects      map[string]*gitlab.Project
	muProjects    sync.RWMutex
	Groups        map[string]*gitlab.Group
	muGroups      sync.RWMutex
	GraphQLClient *utils.GraphQLClient
}

func newGitlabInstance(gitlabURL string, gitlabToken string) (*GitlabInstance, error) {
	gitlabClient, err := gitlab.NewClient(gitlabToken, gitlab.WithBaseURL(gitlabURL))
	if err != nil {
		return nil, err
	}

	gitlabInstance := &GitlabInstance{
		Gitlab:        gitlabClient,
		Projects:      make(map[string]*gitlab.Project),
		Groups:        make(map[string]*gitlab.Group),
		GraphQLClient: utils.NewGitlabGraphQLClient(gitlabToken, gitlabURL),
	}

	return gitlabInstance, nil
}

func (g *GitlabInstance) addProject(projectPath string, project *gitlab.Project) {
	g.muProjects.Lock()
	defer g.muProjects.Unlock()
	g.Projects[projectPath] = project
}

func (g *GitlabInstance) getProject(projectPath string) *gitlab.Project {
	g.muProjects.RLock()
	defer g.muProjects.RUnlock()
	var project *gitlab.Project
	project, exists := g.Projects[projectPath]
	if !exists {
		project = nil
	}
	return project
}

func (g *GitlabInstance) addGroup(groupPath string, group *gitlab.Group) {
	g.muGroups.Lock()
	defer g.muGroups.Unlock()
	g.Groups[groupPath] = group
}

func (g *GitlabInstance) getGroup(groupPath string) *gitlab.Group {
	g.muGroups.RLock()
	defer g.muGroups.RUnlock()
	return g.Groups[groupPath]
}
