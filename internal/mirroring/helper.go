package mirroring

import (
	"gitlab-sync/internal/utils"
	"sort"

	"go.uber.org/zap"
)

// reverseGroupMirrorMap reverses the mirror mapping to get the source group path for each destination group.
// It creates a map where the keys are the destination group paths and the values are the source group paths.
// The function also returns a sorted list of destination group paths.
func (g *GitlabInstance) reverseGroupMirrorMap(mirrorMapping *utils.MirrorMapping) (map[string]string, []string) {
	var reversedMirrorMap map[string]string
	destinationGroupPaths := make([]string, 0, len(g.Groups))
	if mirrorMapping != nil {
		// Reverse the mirror mapping to get the source group path for each destination group
		reversedMirrorMap = make(map[string]string, len(mirrorMapping.Groups))
		// Extract the keys (group paths) and sort them
		// This ensures that the parent groups are created before their children
		for sourceGroupPath, createOptions := range mirrorMapping.Groups {
			if _, ok := reversedMirrorMap[createOptions.DestinationPath]; ok {
				zap.L().Error("duplicate destination path found in mirror mapping", zap.String("destinationPath", createOptions.DestinationPath))
				continue
			}
			reversedMirrorMap[createOptions.DestinationPath] = sourceGroupPath
			destinationGroupPaths = append(destinationGroupPaths, createOptions.DestinationPath)
		}
		sort.Strings(destinationGroupPaths)
	}
	return reversedMirrorMap, destinationGroupPaths
}
