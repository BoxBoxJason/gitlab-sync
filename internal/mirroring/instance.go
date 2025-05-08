package mirroring

import (
	"sync"
	"time"

	"gitlab-sync/internal/utils"

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

// newGitlabInstance creates a new GitlabInstance with the provided parameters
// and initializes the GitLab client with a custom HTTP client.
func newGitlabInstance(gitlabURL string, gitlabToken string, timeout time.Duration, maxRetries int) (*GitlabInstance, error) {
	// Initialize the GitLab client with the custom HTTP client
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

// addProject adds a project to the GitLabInstance
// with the given projectPath and project object.
// It uses a mutex to ensure thread-safe access to the Projects map.
func (g *GitlabInstance) addProject(projectPath string, project *gitlab.Project) {
	g.muProjects.Lock()
	defer g.muProjects.Unlock()
	g.Projects[projectPath] = project
}

// getProject retrieves a project from the GitLabInstance
// using the given projectPath.
// It uses a read lock to ensure thread-safe access to the Projects map.
// If the project is not found, it returns nil.
func (g *GitlabInstance) getProject(projectPath string) *gitlab.Project {
	g.muProjects.RLock()
	defer g.muProjects.RUnlock()
	return g.Projects[projectPath]
}

// addGroup adds a group to the GitLabInstance
// with the given groupPath and group object.
// It uses a mutex to ensure thread-safe access to the Groups map.
func (g *GitlabInstance) addGroup(groupPath string, group *gitlab.Group) {
	g.muGroups.Lock()
	defer g.muGroups.Unlock()
	g.Groups[groupPath] = group
}

// getGroup retrieves a group from the GitLabInstance
// using the given groupPath.
// It uses a read lock to ensure thread-safe access to the Groups map.
// If the group is not found, it returns nil.
func (g *GitlabInstance) getGroup(groupPath string) *gitlab.Group {
	g.muGroups.RLock()
	defer g.muGroups.RUnlock()
	return g.Groups[groupPath]
}
