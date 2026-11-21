package mirroring

import (
	"slices"

	"github.com/boxboxjason/gitlab-sync/internal/mirrorcfg"

	"go.uber.org/zap"
)

// reversedGroupMap is the reversed group mirror mapping: the source group path of each
// destination group, and the destination group paths sorted so parents come before children.
type reversedGroupMap struct {
	sourceByDestination map[string]string
	destinationPaths    []string
}

// reverseGroupMirrorMap reverses the mirror mapping to get the source group path for each destination group.
// It creates a map where the keys are the destination group paths and the values are the source group paths.
// The function also returns a sorted list of destination group paths.
func (g *GitlabInstance) reverseGroupMirrorMap(mirrorMapping *mirrorcfg.MirrorMapping) reversedGroupMap {
	var reversedMirrorMap map[string]string

	destinationGroupPaths := make([]string, 0, g.GroupsLen())

	if mirrorMapping != nil {
		// Reverse the mirror mapping to get the source group path for each destination group
		groupsSnapshot := mirrorMapping.GroupsSnapshot()
		reversedMirrorMap = make(map[string]string, len(groupsSnapshot))
		// Extract the keys (group paths) and sort them
		// This ensures that the parent groups are created before their children
		for sourceGroupPath, createOptions := range groupsSnapshot {
			if _, ok := reversedMirrorMap[createOptions.DestinationPath]; ok {
				zap.L().Error("duplicate destination path found in mirror mapping", zap.String("destinationPath", createOptions.DestinationPath))

				continue
			}

			reversedMirrorMap[createOptions.DestinationPath] = sourceGroupPath
			destinationGroupPaths = append(destinationGroupPaths, createOptions.DestinationPath)
		}

		slices.Sort(destinationGroupPaths)
	}

	return reversedGroupMap{sourceByDestination: reversedMirrorMap, destinationPaths: destinationGroupPaths}
}
