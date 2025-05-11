package mirroring

import (
	"sync"

	"gitlab-sync/internal/utils"

	"github.com/hashicorp/go-retryablehttp"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

type GitlabInstance struct {
	// Gitlab is the GitLab client used to interact with the GitLab API
	Gitlab *gitlab.Client
	// Projects is a map of project paths to GitLab project objects, it is used to
	// cache projects and avoid unnecessary API calls
	Projects map[string]*gitlab.Project
	// muProjects is a mutex used to synchronize access to the Projects map
	// It ensures that only one goroutine can read or write to the Projects map at a time
	muProjects sync.RWMutex
	// Groups is a map of group paths to GitLab group objects, it is used to
	// cache groups and avoid unnecessary API calls
	Groups map[string]*gitlab.Group
	// muGroups is a mutex used to synchronize access to the Groups map
	// It ensures that only one goroutine can read or write to the Groups map at a time
	muGroups sync.RWMutex
	// GraphQLClient is the GraphQL client used to interact with the GitLab GraphQL API
	// It is used to perform GraphQL queries and mutations
	// It is initialized with the GitLab token and URL
	GraphQLClient *utils.GraphQLClient
	// Role is the role of the GitLab instance, it can be either "source" or "destination"
	// It is used to determine the behavior of the mirroring process
	Role string
	// InstanceSize is the size of the GitLab instance, it can be either "small" or "big"
	// It is used to determine the behavior of the fetching process
	InstanceSize string
}

type GitlabInstanceOpts struct {
	// GitlabURL is the URL of the GitLab instance
	GitlabURL string
	// GitlabToken is the token used to authenticate with the GitLab API
	GitlabToken string
	// Role is the role of the GitLab instance, it can be either "source" or "destination"
	Role string
	// MaxRetries is the maximum number of retries for GitLab API requests
	MaxRetries int
	// InstanceSize is the size of the GitLab instance, it can be either "small" or "big"
	// It is used to determine the behavior of the fetching process
	InstanceSize string
}

// newGitlabInstance creates a new GitlabInstance with the provided parameters
// and initializes the GitLab client with a custom HTTP client.
func newGitlabInstance(initArgs *GitlabInstanceOpts) (*GitlabInstance, error) {
	// Initialize the GitLab client with the custom HTTP client
	gitlabClient, err := gitlab.NewClient(initArgs.GitlabToken, gitlab.WithBaseURL(initArgs.GitlabURL), gitlab.WithCustomRetryMax(initArgs.MaxRetries), gitlab.WithCustomBackoff(retryablehttp.DefaultBackoff))
	if err != nil {
		return nil, err
	}

	gitlabInstance := &GitlabInstance{
		Gitlab:        gitlabClient,
		Projects:      make(map[string]*gitlab.Project),
		Groups:        make(map[string]*gitlab.Group),
		GraphQLClient: utils.NewGitlabGraphQLClient(initArgs.GitlabToken, initArgs.GitlabURL),
		Role:          initArgs.Role,
		InstanceSize:  initArgs.InstanceSize,
	}

	return gitlabInstance, nil
}

// addProject adds a project to the GitLabInstance
// with the given projectPath and project object.
// It uses a mutex to ensure thread-safe access to the Projects map.
func (g *GitlabInstance) addProject(project *gitlab.Project) {
	g.muProjects.Lock()
	defer g.muProjects.Unlock()
	g.Projects[project.PathWithNamespace] = project
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
func (g *GitlabInstance) addGroup(group *gitlab.Group) {
	g.muGroups.Lock()
	defer g.muGroups.Unlock()
	g.Groups[group.FullPath] = group
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

// isBig checks if the GitLab instance is of size "big".
// It returns true if the InstanceSize is "big", otherwise false.
func (g *GitlabInstance) isBig() bool {
	return g.InstanceSize == INSTANCE_SIZE_BIG
}

func (g *GitlabInstance) isSource() bool {
	return g.Role == ROLE_SOURCE
}
