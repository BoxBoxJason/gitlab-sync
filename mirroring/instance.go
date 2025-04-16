package mirroring

import (
	"net/http"
	"sync"
	"time"

	"gitlab-sync/utils"

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

func newGitlabInstance(gitlabURL string, gitlabToken string, timeout time.Duration, maxRetries int) (*GitlabInstance, error) {
	// Create a custom HTTP client with a timeout
	httpClient := &http.Client{
		Timeout: timeout,
		Transport: &retryTransport{
			Base:       http.DefaultTransport,
			MaxRetries: maxRetries,
		},
	}

	// Initialize the GitLab client with the custom HTTP client
	gitlabClient, err := gitlab.NewClient(gitlabToken, gitlab.WithBaseURL(gitlabURL), gitlab.WithHTTPClient(httpClient))
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

// Add a project to the GitLabInstance
func (g *GitlabInstance) addProject(projectPath string, project *gitlab.Project) {
	g.muProjects.Lock()
	defer g.muProjects.Unlock()
	g.Projects[projectPath] = project
}

// Get a project from the GitLabInstance
func (g *GitlabInstance) getProject(projectPath string) *gitlab.Project {
	g.muProjects.RLock()
	defer g.muProjects.RUnlock()
	return g.Projects[projectPath]
}

// Add a group to the GitLabInstance
func (g *GitlabInstance) addGroup(groupPath string, group *gitlab.Group) {
	g.muGroups.Lock()
	defer g.muGroups.Unlock()
	g.Groups[groupPath] = group
}

// Get a group from the GitLabInstance
func (g *GitlabInstance) getGroup(groupPath string) *gitlab.Group {
	g.muGroups.RLock()
	defer g.muGroups.RUnlock()
	return g.Groups[groupPath]
}

// retryTransport wraps the default HTTP transport to add automatic retries
type retryTransport struct {
	Base       http.RoundTripper
	MaxRetries int
}

// RoundTrip implements the RoundTripper interface for retryTransport
func (rt *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	// Retry the request up to MaxRetries times
	for i := 0; i <= rt.MaxRetries; i++ {
		resp, err = rt.Base.RoundTrip(req)
		if err == nil && resp.StatusCode < http.StatusInternalServerError {
			// If the request succeeded or returned a non-server-error status, return the response
			return resp, nil
		}

		// Retry only on specific server errors or network issues
		time.Sleep(time.Duration(i) * time.Second) // Exponential backoff
	}

	return resp, err
}
