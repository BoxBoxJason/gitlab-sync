package mirroring

import (
	"fmt"
	"net/http"
	"path/filepath"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.uber.org/zap"
)

// EnsureParentNamespaceID returns the ID of the parent group of projectOrGroupPath,
// creating every missing group along that path first (top-down).
//
// It returns -1 when the path has no parent (top-level group or project).
//
// A parent that is missing from the instance cache is looked up on the instance
// before being created, since the initial fetch only caches groups matching the
// mirror mapping filters. A group created here inherits the visibility of its
// closest existing ancestor, so it is never more visible than that ancestor; a
// missing top-level group uses fallbackVisibility, the visibility of the resource
// being created.
func (g *GitlabInstance) EnsureParentNamespaceID(projectOrGroupPath string, fallbackVisibility gitlab.VisibilityValue) (int64, error) {
	parentPath := filepath.Dir(projectOrGroupPath)
	if parentPath == "." || parentPath == "/" {
		return -1, nil
	}

	// Fast path: parent already cached, no need to serialize.
	if parentGroup := g.GetGroup(parentPath); parentGroup != nil {
		return parentGroup.ID, nil
	}

	// Projects are created concurrently; serialize so sibling projects sharing a
	// missing parent create it once instead of racing on "has already been taken".
	g.muEnsureGroups.Lock()
	defer g.muEnsureGroups.Unlock()

	parentGroup, err := g.ensureGroup(parentPath, fallbackVisibility)
	if err != nil {
		return -1, fmt.Errorf("failed to ensure parent group %s exists: %w", parentPath, err)
	}

	return parentGroup.ID, nil
}

// ensureGroup returns the group at groupPath, looking it up on the instance or
// creating it (and its missing ancestors) when needed. Callers must hold muEnsureGroups.
func (g *GitlabInstance) ensureGroup(groupPath string, fallbackVisibility gitlab.VisibilityValue) (*gitlab.Group, error) {
	if group := g.GetGroup(groupPath); group != nil {
		return group, nil
	}

	group, resp, err := g.Gitlab.Groups.GetGroup(groupPath, &gitlab.GetGroupOptions{})
	if err == nil {
		g.AddGroup(group)

		return group, nil
	}

	if resp == nil || resp.StatusCode != http.StatusNotFound {
		return nil, fmt.Errorf("failed to look up group %s: %w", groupPath, err)
	}

	groupName := filepath.Base(groupPath)
	visibility := fallbackVisibility
	createOptions := &gitlab.CreateGroupOptions{
		Name: &groupName,
		Path: &groupName,
	}

	if parentPath := filepath.Dir(groupPath); parentPath != "." && parentPath != "/" {
		parentGroup, parentErr := g.ensureGroup(parentPath, fallbackVisibility)
		if parentErr != nil {
			return nil, parentErr
		}

		createOptions.ParentID = &parentGroup.ID

		if parentGroup.Visibility != "" {
			visibility = parentGroup.Visibility
		}
	}

	createOptions.Visibility = &visibility

	zap.L().Info("Creating missing parent group", zap.String(RoleKey, g.Role), zap.String("group", groupPath), zap.String("visibility", string(visibility)))

	createdGroup, _, err := g.Gitlab.Groups.CreateGroup(createOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create group %s: %w", groupPath, err)
	}

	g.AddGroup(createdGroup)

	return createdGroup, nil
}
