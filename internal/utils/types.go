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
	"path/filepath"
	"strings"
	"sync"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

const (
	PROJECT = "project"
	GROUP   = "group"
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
// - timeout: the timeout for the GitLab API requests
// - retry: the number of retries for the GitLab API requests
type ParserArgs struct {
	SourceGitlabURL        string
	SourceGitlabToken      string
	DestinationGitlabURL   string
	DestinationGitlabToken string
	MirrorMapping          *MirrorMapping
	Verbose                bool
	NoPrompt               bool
	DryRun                 bool
	Timeout                time.Duration
	Retry                  int
}

// ProjectMirrorOptions defines how the project should be mirrored
// to the destination GitLab instance
// - destination_url: the URL of the destination GitLab instance
// - ci_cd_catalog: whether to add the project to the CI/CD catalog
// - issues: whether to mirror the issues
type MirroringOptions struct {
	DestinationPath     string `json:"destination_path"`
	CI_CD_Catalog       bool   `json:"ci_cd_catalog"`
	Issues              bool   `json:"issues"`
	MirrorTriggerBuilds bool   `json:"mirror_trigger_builds"`
	Visibility          string `json:"visibility"`
	MirrorReleases      bool   `json:"mirror_releases"`
}

// MirrorMapping defines the mapping of projects and groups
// to the destination GitLab instance
// It is used to parse the JSON file that contains the mapping
// - projects: a map of project names to their mirroring options
// - groups: a map of group names to their mirroring options
type MirrorMapping struct {
	Projects   map[string]*MirroringOptions `json:"projects"`
	Groups     map[string]*MirroringOptions `json:"groups"`
	muProjects sync.RWMutex
	muGroups   sync.RWMutex
}

func (m *MirrorMapping) AddProject(project string, options *MirroringOptions) {
	m.muProjects.Lock()
	defer m.muProjects.Unlock()
	m.Projects[project] = options
}

func (m *MirrorMapping) AddGroup(group string, options *MirroringOptions) {
	m.muGroups.Lock()
	defer m.muGroups.Unlock()
	m.Groups[group] = options
}

func (m *MirrorMapping) GetProject(project string) (*MirroringOptions, bool) {
	m.muProjects.RLock()
	defer m.muProjects.RUnlock()
	options, ok := m.Projects[project]
	return options, ok
}

func (m *MirrorMapping) GetGroup(group string) (*MirroringOptions, bool) {
	m.muGroups.RLock()
	defer m.muGroups.RUnlock()
	options, ok := m.Groups[group]
	return options, ok
}

// OpenMirrorMapping opens the JSON file that contains the mapping
// and parses it into a MirrorMapping struct
// It returns the mapping and an error if any
func OpenMirrorMapping(path string) (*MirrorMapping, error) {
	mapping := &MirrorMapping{
		Projects: make(map[string]*MirroringOptions),
		Groups:   make(map[string]*MirroringOptions),
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

// check checks if the mapping is valid
// It checks if the projects and groups are valid
// It returns an error if any of the projects or groups are invalid
func (m *MirrorMapping) check() error {
	errChan := make(chan error, 4*(len(m.Projects)+len(m.Groups))+1)
	// Check if the mapping is valid
	if len(m.Projects) == 0 && len(m.Groups) == 0 {
		errChan <- errors.New("no projects or groups defined in the mapping")
	}

	// Check if the projects are valid
	m.checkProjects(errChan)

	// Check if the groups are valid
	m.checkGroups(errChan)

	close(errChan)
	return MergeErrors(errChan, 2)
}

// checkProjects checks if the projects are valid
// It checks if the project names and destination paths are valid
// It returns an error if any of the projects are invalid
func (m *MirrorMapping) checkProjects(errChan chan error) {
	for project, options := range m.Projects {
		// Check the source / destination paths
		checkCopyPaths(project, options.DestinationPath, PROJECT, errChan)

		// Check the visibility
		visibilityString := strings.TrimSpace(string(options.Visibility))
		if visibilityString != "" && !checkVisibility(visibilityString) {
			errChan <- fmt.Errorf("invalid project visibility: %s", string(options.Visibility))
			options.Visibility = string(gitlab.PublicVisibility)
		}
	}
}

func checkCopyPaths(sourcePath string, destinationPath string, pathType string, errChan chan error) {
	// Ensure the source project path and destination path are not empty
	if sourcePath == "" || destinationPath == "" {
		errChan <- errors.New("invalid (empty) string in " + pathType + " mapping")
	} else {
		// Ensure the source project path and destination path do not start or end with a slash
		if strings.HasPrefix(sourcePath, "/") || strings.HasSuffix(sourcePath, "/") {
			errChan <- errors.New("invalid " + pathType + " mapping (must not start or end with /): " + sourcePath)
		}
		// Ensure the destination path does not start or end with a slash
		if strings.HasPrefix(destinationPath, "/") || strings.HasSuffix(destinationPath, "/") {
			errChan <- errors.New("invalid destination path (must not start or end with /): " + destinationPath)
		}
		if pathType == PROJECT {
			if strings.Count(destinationPath, "/") < 1 {
				errChan <- errors.New("invalid project destination path (must be in a namespace): " + destinationPath)
			}
		}
	}
	if filepath.Base(sourcePath) != filepath.Base(destinationPath) {
		errChan <- fmt.Errorf("source and destination paths must have the same base name (ending): %s != %s", sourcePath, destinationPath)
	}
}

func (m *MirrorMapping) checkGroups(errChan chan error) {
	for group, options := range m.Groups {
		// Check the source / destination paths
		checkCopyPaths(group, options.DestinationPath, GROUP, errChan)

		// Check the visibility
		visibilityString := strings.TrimSpace(string(options.Visibility))
		if visibilityString != "" && !checkVisibility(visibilityString) {
			errChan <- fmt.Errorf("invalid group visibility: %s", string(options.Visibility))
			options.Visibility = string(gitlab.PublicVisibility)
		}
	}
}

func checkVisibility(visibility string) bool {
	var valid bool
	switch visibility {
	case string(gitlab.PublicVisibility):
		valid = true
	case string(gitlab.InternalVisibility):
		valid = true
	case string(gitlab.PrivateVisibility):
		valid = true
	default:
		valid = false
	}
	return valid
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
