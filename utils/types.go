/*
Utility types definitions
*/
package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
)

// ParserArgs defines the command line arguments
// - source_gitlab_url: the URL of the source GitLab instance
// - source_gitlab_token: the token for the source GitLab instance
// - destination_gitlab_url: the URL of the destination GitLab instance
// - destination_gitlab_token: the token for the destination GitLab instance
// - mirror_mapping: the path to the JSON file that contains the mapping
// - verbose: whether to enable verbose logging
// - no_prompt: whether to disable prompts
// - dry_run: whether to perform a dry run
// - version: whether to show the version
type ParserArgs struct {
	SourceGitlabURL        string
	SourceGitlabToken      string
	DestinationGitlabURL   string
	DestinationGitlabToken string
	MirrorMapping          *MirrorMapping
	Verbose                bool
	NoPrompt               bool
	DryRun                 bool
	Version                bool
}

// ProjectMirrorOptions defines how the project should be mirrored
// to the destination GitLab instance
// - destination_url: the URL of the destination GitLab instance
// - ci_cd_catalog: whether to add the project to the CI/CD catalog
// - issues: whether to mirror the issues
type ProjectMirroringOptions struct {
	DestinationPath string `json:"destination_path"`
	CI_CD_Catalog   bool   `json:"ci_cd_catalog"`
	Issues          bool   `json:"issues"`
}

// GroupMirrorOptions defines how the group should be mirrored
// to the destination GitLab instance
// - destination_url: the URL of the destination GitLab instance
// - ci_cd_catalog: whether to add the group to the CI/CD catalog
// - issues: whether to mirror the issues
type GroupMirroringOptions struct {
	DestinationPath string `json:"destination_path"`
	CI_CD_Catalog   bool   `json:"ci_cd_catalog"`
	Issues          bool   `json:"issues"`
}

// MirrorMapping defines the mapping of projects and groups
// to the destination GitLab instance
// It is used to parse the JSON file that contains the mapping
// - projects: a map of project names to their mirroring options
// - groups: a map of group names to their mirroring options
type MirrorMapping struct {
	Projects   map[string]*ProjectMirroringOptions `json:"projects"`
	Groups     map[string]*GroupMirroringOptions   `json:"groups"`
	muProjects sync.Mutex
	muGroups   sync.Mutex
}

func (m *MirrorMapping) AddProject(project string, options *ProjectMirroringOptions) {
	m.muProjects.Lock()
	defer m.muProjects.Unlock()
	m.Projects[project] = options
}

func (m *MirrorMapping) AddGroup(group string, options *GroupMirroringOptions) {
	m.muGroups.Lock()
	defer m.muGroups.Unlock()
	m.Groups[group] = options
}

// OpenMirrorMapping opens the JSON file that contains the mapping
// and parses it into a MirrorMapping struct
// It returns the mapping and an error if any
func OpenMirrorMapping(path string) (*MirrorMapping, error) {
	mapping := &MirrorMapping{
		Projects: make(map[string]*ProjectMirroringOptions),
		Groups:   make(map[string]*GroupMirroringOptions),
	}

	// Read the file
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Decode the JSON
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(mapping); err != nil {
		return nil, err
	}

	// Check if the mapping is valid
	err = mapping.check()
	if err != nil {
		return nil, err
	}

	return mapping, nil
}

func (m *MirrorMapping) check() error {
	errChan := make(chan error, 3*(len(m.Projects)+len(m.Groups)+1))
	// Check if the mapping is valid
	if len(m.Projects) == 0 && len(m.Groups) == 0 {
		errChan <- errors.New("no projects or groups defined in the mapping")
	}

	// Check if the projects are valid
	for project, options := range m.Projects {
		if project == "" || options.DestinationPath == "" {
			errChan <- fmt.Errorf("invalid (empty) string in project mapping: %s", project)
		}
		if strings.HasPrefix(project, "/") || strings.HasSuffix(project, "/") {
			errChan <- fmt.Errorf("invalid project mapping (must not start or end with /): %s", project)
		}
		if strings.HasPrefix(options.DestinationPath, "/") || strings.HasSuffix(options.DestinationPath, "/") {
			errChan <- fmt.Errorf("invalid destination path (must not start or end with /): %s", options.DestinationPath)
		} else if strings.Count(options.DestinationPath, "/") < 1 {
			errChan <- fmt.Errorf("invalid project destination path (must be in a namespace): %s", options.DestinationPath)
		}
	}

	// Check if the groups are valid
	for group, options := range m.Groups {
		if group == "" || options.DestinationPath == "" {
			errChan <- fmt.Errorf("invalid (empty) string in group mapping: %s", group)
		}
		if strings.HasPrefix(group, "/") || strings.HasSuffix(group, "/") {
			errChan <- fmt.Errorf("invalid group mapping (must not start or end with /): %s", group)
		}
		if strings.HasPrefix(options.DestinationPath, "/") || strings.HasSuffix(options.DestinationPath, "/") {
			errChan <- fmt.Errorf("invalid destination path (must not start or end with /): %s", options.DestinationPath)
		}
	}
	close(errChan)
	return MergeErrors(errChan, 2)
}

// GraphQLClient is a client for sending GraphQL requests to GitLab
type GraphQLClient struct {
	token string
	URL   string
}

// GraphQLRequest is a struct that represents a GraphQL request
// It contains the query and the variables
type GraphQLRequest struct {
	Query     string `json:"query"`
	Variables string `json:"variables,omitempty"`
}

// NewGitlabGraphQLClient creates a new GraphQL client for GitLab
// It takes the token and the GitLab URL as arguments
// It returns a pointer to the GraphQLClient struct
func NewGitlabGraphQLClient(token, gitlabUrl string) *GraphQLClient {
	return &GraphQLClient{
		token: token,
		URL:   strings.TrimSuffix(gitlabUrl, "/") + "/api/graphql",
	}
}

// SendRequest sends a GraphQL request to GitLab
// It takes a GraphQLRequest struct and the HTTP method as arguments
// It returns the response body as a string and an error if any
func (g *GraphQLClient) SendRequest(request *GraphQLRequest, method string) (string, error) {
	requestBody, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	LogVerbosef("Sending GraphQL request to %s with body: %s", g.URL, string(requestBody))
	req, err := http.NewRequestWithContext(context.Background(), method, g.URL, bytes.NewBuffer(requestBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GraphQL request failed with status: %s", resp.Status)
	}
	var responseBody bytes.Buffer
	if _, err := responseBody.ReadFrom(resp.Body); err != nil {
		return "", err
	}
	return responseBody.String(), nil
}
