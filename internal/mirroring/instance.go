package mirroring

import (
	"fmt"
	"gitlab-sync/internal/utils"
	"gitlab-sync/pkg/helpers"
	"path/filepath"
	"sync"

	"github.com/Masterminds/semver/v3"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/hashicorp/go-retryablehttp"
	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

const (
	INSTANCE_SEMVER_THRESHOLD = "17.6"
	ULTIMATE_PLAN             = "ultimate"
	PREMIUM_PLAN              = "premium"
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
	// Role is the role of the GitLab instance, it can be either "source" or "destination"
	// It is used to determine the behavior of the mirroring process
	Role string
	// InstanceSize is the size of the GitLab instance, it can be either "small" or "big"
	// It is used to determine the behavior of the fetching process
	InstanceSize string
	// PullMirrorAvailable is a boolean indicating whether the GitLab instance supports pull mirroring
	PullMirrorAvailable bool
	// GitAuth is the HTTP authentication used for GitLab git over HTTP operations (only for non premium instances)
	GitAuth transport.AuthMethod
	// UserID is the ID of the authenticated user
	UserID int
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

// NewGitlabInstance creates a new GitlabInstance with the provided parameters
// and initializes the GitLab client with a custom HTTP client.
func NewGitlabInstance(initArgs *GitlabInstanceOpts) (*GitlabInstance, error) {
	// Initialize the GitLab client with the custom HTTP client
	gitlabClient, err := gitlab.NewClient(initArgs.GitlabToken, gitlab.WithBaseURL(initArgs.GitlabURL), gitlab.WithCustomRetryMax(initArgs.MaxRetries), gitlab.WithCustomBackoff(retryablehttp.DefaultBackoff))
	if err != nil {
		return nil, err
	}

	// Get the current user ID
	user, _, err := gitlabClient.Users.CurrentUser()
	if err != nil {
		return nil, fmt.Errorf("failed to get current user: %w", err)
	}

	gitlabInstance := &GitlabInstance{
		Gitlab:       gitlabClient,
		Projects:     make(map[string]*gitlab.Project),
		Groups:       make(map[string]*gitlab.Group),
		Role:         initArgs.Role,
		InstanceSize: initArgs.InstanceSize,
		GitAuth:      helpers.BuildHTTPAuth("", initArgs.GitlabToken),
		UserID:       user.ID,
	}

	return gitlabInstance, nil
}

// AddProject adds a project to the GitLabInstance
// with the given projectPath and project object.
// It uses a mutex to ensure thread-safe access to the Projects map.
func (g *GitlabInstance) AddProject(project *gitlab.Project) {
	g.muProjects.Lock()
	defer g.muProjects.Unlock()
	g.Projects[project.PathWithNamespace] = project
}

// GetProject retrieves a project from the GitLabInstance
// using the given projectPath.
// It uses a read lock to ensure thread-safe access to the Projects map.
// If the project is not found, it returns nil.
func (g *GitlabInstance) GetProject(projectPath string) *gitlab.Project {
	g.muProjects.RLock()
	defer g.muProjects.RUnlock()
	return g.Projects[projectPath]
}

// AddGroup adds a group to the GitLabInstance
// with the given groupPath and group object.
// It uses a mutex to ensure thread-safe access to the Groups map.
func (g *GitlabInstance) AddGroup(group *gitlab.Group) {
	g.muGroups.Lock()
	defer g.muGroups.Unlock()
	g.Groups[group.FullPath] = group
}

// GetGroup retrieves a group from the GitLabInstance
// using the given groupPath.
// It uses a read lock to ensure thread-safe access to the Groups map.
// If the group is not found, it returns nil.
func (g *GitlabInstance) GetGroup(groupPath string) *gitlab.Group {
	g.muGroups.RLock()
	defer g.muGroups.RUnlock()
	return g.Groups[groupPath]
}

// IsBig checks if the GitLab instance is of size "big".
// It returns true if the InstanceSize is "big", otherwise false.
func (g *GitlabInstance) IsBig() bool {
	return g.InstanceSize == INSTANCE_SIZE_BIG
}

// IsSource checks if the GitLab instance is a source instance.
func (g *GitlabInstance) IsSource() bool {
	return g.Role == ROLE_SOURCE
}

// IsVersionGreaterThanThreshold checks if the GitLab instance version is below the defined threshold.
// It retrieves the metadata from the GitLab instance and compares the version
// with the INSTANCE_SEMVER_THRESHOLD.
func (g *GitlabInstance) IsVersionGreaterThanThreshold() (bool, error) {
	metadata, _, err := g.Gitlab.Metadata.GetMetadata()
	if err != nil {
		return false, fmt.Errorf("failed to get GitLab version: %w", err)
	}
	zap.L().Debug("GitLab Instance version", zap.String(ROLE, g.Role), zap.String("version", metadata.Version))

	currentVer, err := semver.NewVersion(metadata.Version)
	if err != nil {
		return false, fmt.Errorf("failed to parse GitLab version: %w", err)
	}
	thresholdVer, err := semver.NewVersion(INSTANCE_SEMVER_THRESHOLD)
	if err != nil {
		return false, fmt.Errorf("failed to parse version threshold: %w", err)
	}

	return currentVer.GreaterThanEqual(thresholdVer), nil
}

// IsLicensePremium checks if the GitLab instance has a premium license.
// It retrieves the license information and checks the plan type.
func (g *GitlabInstance) IsLicensePremium() (bool, error) {
	license, _, err := g.Gitlab.License.GetLicense()
	if err != nil {
		return false, fmt.Errorf("failed to get GitLab license: %w", err)
	}
	zap.L().Info("GitLab Instance license", zap.String(ROLE, g.Role), zap.String("plan", license.Plan))
	if license.Plan != ULTIMATE_PLAN && license.Plan != PREMIUM_PLAN || license.Expired {
		return false, nil
	}
	return true, nil
}

// FetchAll retrieves all projects and groups from the GitLab instance
// that match the filters and stores them in the instance cache.
func (g *GitlabInstance) FetchAll(projectFilters map[string]struct{}, groupFilters map[string]struct{}, mirrorMapping *utils.MirrorMapping) []error {
	zap.L().Info("Fetching all projects and groups from GitLab instance", zap.String(ROLE, g.Role), zap.String(INSTANCE_SIZE, g.InstanceSize), zap.Int("projects", len(projectFilters)), zap.Int("groups", len(groupFilters)))
	wg := sync.WaitGroup{}
	errCh := make(chan []error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := g.FetchAndProcessGroups(&groupFilters, mirrorMapping); err != nil {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		if err := g.FetchAndProcessProjects(&projectFilters, &groupFilters, mirrorMapping); err != nil {
			errCh <- err
		}
	}()
	wg.Wait()
	close(errCh)

	return helpers.MergeErrors(errCh)
}

// GetParentNamespaceID retrieves the parent namespace ID for a given project or group path.
// It checks if the parent path is already in the instance groups cache.
//
// If not, it returns an error indicating that the parent group was not found.
func (g *GitlabInstance) GetParentNamespaceID(projectOrGroupPath string) (int, error) {
	parentGroupID := -1
	parentPath := filepath.Dir(projectOrGroupPath)
	var err error = nil
	if parentPath != "." && parentPath != "/" {
		// Check if parent path is already in the instance groups cache
		if parentGroup, ok := g.Groups[parentPath]; ok {
			parentGroupID = parentGroup.ID
		} else {
			err = fmt.Errorf("parent group not found for path: %s", parentPath)
		}
	}
	return parentGroupID, err
}
