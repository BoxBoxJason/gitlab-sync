package mirrorcfg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.uber.org/zap"

	"github.com/boxboxjason/gitlab-sync/pkg/helpers"
)

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
	return checkEntries(m.Projects, ProjectType)
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

	if pathType == ProjectType && strings.Count(destinationPath, "/") < 1 {
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
	return checkEntries(m.Groups, GroupType)
}

// checkEntries validates the project or group entries (according to entityType),
// logging every problem it finds. It returns the number of problems found.
func checkEntries(entries map[string]*MirroringOptions, entityType string) int {
	problems := 0

	duplicateDestinationFinder := make(map[string]struct{}, len(entries))
	for source, options := range entries {
		if options == nil {
			reportMappingProblem("missing options (null) for " + entityType + " mapping: " + source)

			problems++

			continue
		}

		// Check if the destination path is already used
		if _, ok := duplicateDestinationFinder[options.DestinationPath]; ok {
			reportMappingProblem("duplicate destination path found in " + entityType + " mapping: " + options.DestinationPath)

			problems++
		} else {
			duplicateDestinationFinder[options.DestinationPath] = struct{}{}
		}
		// Check the source / destination paths
		problems += checkCopyPaths(source, options.DestinationPath, entityType)

		// Check the visibility
		options.Visibility = new(strings.TrimSpace(helpers.Deref(options.Visibility, string(gitlab.PublicVisibility))))
		if options.Visibility != nil && !checkVisibility(*options.Visibility) {
			reportMappingProblem("invalid " + entityType + " visibility: " + *options.Visibility)

			problems++

			options.Visibility = new(string(gitlab.PublicVisibility))
		}
	}

	return problems
}

// checkVisibility checks if the visibility string is valid
// It checks if the visibility string is one of the valid GitLab visibility values.
func checkVisibility(visibility string) bool {
	switch visibility {
	case string(gitlab.PublicVisibility), string(gitlab.InternalVisibility), string(gitlab.PrivateVisibility):
		return true
	default:
		return false
	}
}
