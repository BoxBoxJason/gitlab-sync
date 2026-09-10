// Package utils contains core types and helpers shared across the application.
package utils

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
	"go.uber.org/zap"
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
// - retry: the number of retries for the GitLab API requests
// - cache_dir: where to keep the bare git clones between runs (empty disables caching)
// - cache_max_age: how long an unused cached repository is kept.
type ParserArgs struct {
	MirrorMapping          *MirrorMapping
	SourceGitlabURL        string
	SourceGitlabToken      string
	DestinationGitlabURL   string
	DestinationGitlabToken string
	CacheDir               string
	CacheMaxAge            time.Duration
	Retry                  int
	ForcePremium           bool
	ForceNonPremium        bool
	DestinationGitlabIsBig bool
	Verbose                bool
	NoPrompt               bool
	DryRun                 bool
	SourceGitlabIsBig      bool
}

// MirroringOptions defines how a project or group should be mirrored
// to the destination GitLab instance
// - destination_url: the URL of the destination GitLab instance
// - ci_cd_catalog: whether to add the project to the CI/CD catalog. Requires GitLab 19.3+ on the destination instance.
// - issues: whether to mirror the issues.
type MirroringOptions struct {
	CI_CD_Catalog       *bool   `json:"ci_cd_catalog"`
	MirrorIssues        *bool   `json:"mirror_issues"`
	MirrorTriggerBuilds *bool   `json:"mirror_trigger_builds"`
	Visibility          *string `json:"visibility"`
	MirrorReleases      *bool   `json:"mirror_releases"`
	ClaimOwnership      *bool   `json:"claim_ownership"`
	DestinationPath     string  `json:"destination_path"`
}

// MirrorMapping defines the mapping of projects and groups
// to the destination GitLab instance
// It is used to parse the JSON file that contains the mapping
// - projects: a map of project names to their mirroring options
// - groups: a map of group names to their mirroring options.
type MirrorMapping struct {
	Projects   map[string]*MirroringOptions `json:"projects"`
	Groups     map[string]*MirroringOptions `json:"groups"`
	muProjects sync.RWMutex
	muGroups   sync.RWMutex
}

// AddProject adds a project to the mapping
// It takes the project name and the mirroring options as parameters
// It locks the projects mutex to ensure thread safety.
func (m *MirrorMapping) AddProject(project string, options *MirroringOptions) {
	m.muProjects.Lock()
	defer m.muProjects.Unlock()

	m.Projects[project] = options
}

// AddGroup adds a group to the mapping
// It takes the group name and the mirroring options as parameters
// It locks the groups mutex to ensure thread safety.
func (m *MirrorMapping) AddGroup(group string, options *MirroringOptions) {
	m.muGroups.Lock()
	defer m.muGroups.Unlock()

	m.Groups[group] = options
}

// GetProject retrieves the mirroring options for a project.
func (m *MirrorMapping) GetProject(project string) (*MirroringOptions, bool) {
	m.muProjects.RLock()
	defer m.muProjects.RUnlock()

	options, ok := m.Projects[project]

	return options, ok
}

// GetGroup retrieves the mirroring options for a group.
func (m *MirrorMapping) GetGroup(group string) (*MirroringOptions, bool) {
	m.muGroups.RLock()
	defer m.muGroups.RUnlock()

	options, ok := m.Groups[group]

	return options, ok
}

// ProjectsSnapshot returns a shallow copy of the projects map.
// It locks the projects mutex to ensure thread-safe access, allowing callers
// to iterate over the result without holding the lock or racing with concurrent writers.
func (m *MirrorMapping) ProjectsSnapshot() map[string]*MirroringOptions {
	m.muProjects.RLock()
	defer m.muProjects.RUnlock()

	snapshot := make(map[string]*MirroringOptions, len(m.Projects))
	maps.Copy(snapshot, m.Projects)

	return snapshot
}

// GroupsSnapshot returns a shallow copy of the groups map.
// It locks the groups mutex to ensure thread-safe access, allowing callers
// to iterate over the result without holding the lock or racing with concurrent writers.
func (m *MirrorMapping) GroupsSnapshot() map[string]*MirroringOptions {
	m.muGroups.RLock()
	defer m.muGroups.RUnlock()

	snapshot := make(map[string]*MirroringOptions, len(m.Groups))
	maps.Copy(snapshot, m.Groups)

	return snapshot
}

// OpenMirrorMapping opens the JSON file that contains the mapping
// and parses it into a MirrorMapping struct.
// Any validation problem is logged as it is found; the returned error is
// non-nil when the file could not be read or is invalid.
func OpenMirrorMapping(path string) (*MirrorMapping, error) {
	mapping := &MirrorMapping{
		Projects: make(map[string]*MirroringOptions),
		Groups:   make(map[string]*MirroringOptions),
	}

	// Read the file
	cleanPath := filepath.Clean(path)

	file, err := os.Open(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open mirror mapping file: %w", err)
	}

	defer file.Close() //nolint:errcheck // We do not need to check if file closing returns an error

	// Decode the JSON
	decoder := json.NewDecoder(file)

	err = decoder.Decode(mapping)
	if err != nil {
		return nil, fmt.Errorf("failed to decode mirror mapping file: %w", err)
	}

	if problems := mapping.check(); problems > 0 {
		return nil, fmt.Errorf("mirror mapping file is invalid (%d problem(s), see logs above)", problems)
	}

	return mapping, nil
}

// check validates the mapping, logging every problem it finds as it finds it.
// It returns the number of problems found.
func (m *MirrorMapping) check() int {
	problems := 0

	// Check if the mapping is valid
	if len(m.Projects) == 0 && len(m.Groups) == 0 {
		zap.L().Error("invalid mirror mapping: no projects or groups defined in the mapping")

		problems++
	}

	// Check if the projects are valid
	problems += m.checkProjects()

	// Check if the groups are valid
	problems += m.checkGroups()

	return problems
}

// reportMappingProblem logs a single mapping validation problem.
func reportMappingProblem(msg string) {
	zap.L().Error("invalid mirror mapping: " + msg)
}

// checkProjects validates the projects, logging every problem it finds.
// It returns the number of problems found.
func (m *MirrorMapping) checkProjects() int {
	problems := 0

	duplicateDestinationFinder := make(map[string]struct{}, len(m.Projects))
	for project, options := range m.Projects {
		// Check if the destination path is already used
		if _, ok := duplicateDestinationFinder[options.DestinationPath]; ok {
			reportMappingProblem("duplicate destination path found in project mapping: " + options.DestinationPath)

			problems++
		} else {
			duplicateDestinationFinder[options.DestinationPath] = struct{}{}
		}
		// Check the source / destination paths
		problems += checkCopyPaths(project, options.DestinationPath, PROJECT)

		// Check the visibility
		options.Visibility = new(strings.TrimSpace(helpers.Deref(options.Visibility, string(gitlab.PublicVisibility))))
		if options.Visibility != nil && !checkVisibility(*options.Visibility) {
			reportMappingProblem("invalid project visibility: " + *options.Visibility)

			problems++

			options.Visibility = new(string(gitlab.PublicVisibility))
		}
	}

	return problems
}

// checkCopyPaths validates a single source/destination path pair, logging every
// problem it finds. It returns the number of problems found.
func checkCopyPaths(sourcePath, destinationPath, pathType string) int {
	// Ensure the source project path and destination path are not empty
	if sourcePath == "" || destinationPath == "" {
		reportMappingProblem("invalid (empty) string in " + pathType + " mapping")

		return 1
	}

	problems := 0

	// Ensure the source project path and destination path do not start or end with a slash
	if strings.HasPrefix(sourcePath, "/") || strings.HasSuffix(sourcePath, "/") {
		reportMappingProblem("invalid " + pathType + " mapping (must not start or end with /): " + sourcePath)

		problems++
	}
	// Ensure the destination path does not start or end with a slash
	if strings.HasPrefix(destinationPath, "/") || strings.HasSuffix(destinationPath, "/") {
		reportMappingProblem("invalid destination path (must not start or end with /): " + destinationPath)

		problems++
	}

	if pathType == PROJECT && strings.Count(destinationPath, "/") < 1 {
		reportMappingProblem("invalid project destination path (must be in a namespace): " + destinationPath)

		problems++
	}

	if filepath.Base(sourcePath) != filepath.Base(destinationPath) {
		reportMappingProblem("source and destination paths must have the same base name (ending): " + sourcePath + " != " + destinationPath)

		problems++
	}

	return problems
}

// checkGroups validates the groups, logging every problem it finds.
// It returns the number of problems found.
func (m *MirrorMapping) checkGroups() int {
	problems := 0

	duplicateDestinationFinder := make(map[string]struct{}, len(m.Groups))
	for group, options := range m.Groups {
		// Check if the destination path is already used
		if _, ok := duplicateDestinationFinder[options.DestinationPath]; ok {
			reportMappingProblem("duplicate destination path found in group mapping: " + options.DestinationPath)

			problems++
		} else {
			duplicateDestinationFinder[options.DestinationPath] = struct{}{}
		}
		// Check the source / destination paths
		problems += checkCopyPaths(group, options.DestinationPath, GROUP)

		// Check the visibility
		options.Visibility = new(strings.TrimSpace(helpers.Deref(options.Visibility, string(gitlab.PublicVisibility))))
		if options.Visibility != nil && !checkVisibility(*options.Visibility) {
			reportMappingProblem("invalid group visibility: " + *options.Visibility)

			problems++

			options.Visibility = new(string(gitlab.PublicVisibility))
		}
	}

	return problems
}

// checkVisibility checks if the visibility string is valid
// It checks if the visibility string is one of the valid GitLab visibility values.
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

// ConvertVisibility converts a visibility *string to a gitlab.VisibilityValue
// It returns the corresponding gitlab.VisibilityValue or gitlab.PublicVisibility if the string is invalid.
func ConvertVisibility(visibility *string) gitlab.VisibilityValue {
	if visibility == nil {
		return gitlab.PublicVisibility
	}

	switch *visibility {
	case string(gitlab.PublicVisibility):
		return gitlab.PublicVisibility
	case string(gitlab.InternalVisibility):
		return gitlab.InternalVisibility
	case string(gitlab.PrivateVisibility):
		return gitlab.PrivateVisibility
	default:
		return gitlab.PublicVisibility
	}
}

// StringArraysMatchValues checks if two string arrays match in values
// It returns true if both arrays have the same values, regardless of order.
func StringArraysMatchValues(array1, array2 []string) bool {
	if len(array1) != len(array2) {
		return false
	}

	matchMap := make(map[string]struct{}, len(array1))
	for _, value := range array1 {
		matchMap[value] = struct{}{}
	}

	for _, value := range array2 {
		if _, ok := matchMap[value]; !ok {
			return false
		}
	}

	return true
}
