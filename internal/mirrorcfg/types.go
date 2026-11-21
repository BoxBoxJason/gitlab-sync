// Package mirrorcfg holds the mirroring configuration: command line arguments, the mirror mapping and its validation.
package mirrorcfg

import (
	"maps"
	"sync"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

const (
	// ProjectType is the entity type of a GitLab project.
	ProjectType = "project"
	// GroupType is the entity type of a GitLab group.
	GroupType = "group"
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
// - cache_max_age: how long an unused cached repository is kept
// - user_agent: the User-Agent sent with every GitLab API request
// - exit_success_on_non_blocking: whether a run that only hit non-blocking errors exits 0.
type ParserArgs struct {
	MirrorMapping            *MirrorMapping
	SourceGitlabURL          string
	SourceGitlabToken        string
	DestinationGitlabURL     string
	DestinationGitlabToken   string
	CacheDir                 string
	UserAgent                string
	CacheMaxAge              time.Duration
	Retry                    int
	ForcePremium             bool
	ForceNonPremium          bool
	DestinationGitlabIsBig   bool
	Verbose                  bool
	NoPrompt                 bool
	DryRun                   bool
	SourceGitlabIsBig        bool
	ExitSuccessOnNonBlocking bool
}

// MirroringOptions defines how a project or group should be mirrored
// to the destination GitLab instance
// - destination_url: the URL of the destination GitLab instance
// - ci_cd_catalog: whether to add the project to the CI/CD catalog. Requires GitLab 19.3+ on the destination instance.
// - issues: whether to mirror the issues.
// - mirror_release_assets: whether to mirror the release asset links (and the files
// they point to on the source instance). Only used when mirror_releases is enabled.
type MirroringOptions struct {
	CICDCatalog         *bool   `json:"ci_cd_catalog"`
	MirrorIssues        *bool   `json:"mirror_issues"`
	MirrorTriggerBuilds *bool   `json:"mirror_trigger_builds"`
	Visibility          *string `json:"visibility"`
	MirrorReleases      *bool   `json:"mirror_releases"`
	MirrorReleaseAssets *bool   `json:"mirror_release_assets"`
	ClaimOwnership      *bool   `json:"claim_ownership"`
	DestinationPath     string  `json:"destination_path"`
}

// TriggerBuilds reports whether mirroring must run CI on the destination
// (mirror_trigger_builds). An unset option, or no options at all, counts as disabled.
func (o *MirroringOptions) TriggerBuilds() bool {
	return o != nil && o.MirrorTriggerBuilds != nil && *o.MirrorTriggerBuilds
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

// ConvertVisibility converts a visibility *string to a gitlab.VisibilityValue
// It returns the corresponding gitlab.VisibilityValue or gitlab.PublicVisibility if the string is invalid.
func ConvertVisibility(visibility *string) gitlab.VisibilityValue {
	if visibility == nil {
		return gitlab.PublicVisibility
	}

	switch *visibility {
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
