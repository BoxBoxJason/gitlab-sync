package utils

import (
	"encoding/json"
	"fmt"
	"os"
)

type ProjectMirroringOptions struct {
	DestinationURL string `json:"destination_url"`
	CI_CD_Catalog  bool   `json:"ci_cd_catalog"`
	Issues         bool   `json:"issues"`
}

type GroupMirroringOptions struct {
	DestinationURL string `json:"destination_url"`
	CI_CD_Catalog  bool   `json:"ci_cd_catalog"`
	Issues         bool   `json:"issues"`
}

type MirrorMapping struct {
	Projects map[string]ProjectMirroringOptions `json:"projects"`
	Groups   map[string]GroupMirroringOptions   `json:"groups"`
}

func OpenMirrorMapping(path string) (*MirrorMapping, error) {
	mapping := &MirrorMapping{
		Projects: make(map[string]ProjectMirroringOptions),
		Groups:   make(map[string]GroupMirroringOptions),
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
	errors := make([]string, 0)
	// Check if the mapping is valid
	if len(m.Projects) == 0 && len(m.Groups) == 0 {
		errors = append(errors, "no projects or groups defined in the mapping")
	}

	// Check if the projects are valid
	for project, options := range m.Projects {
		if project == "" || options.DestinationURL == "" {
			errors = append(errors, fmt.Sprintf("  - invalid project mapping: %s", project))
		}
	}

	// Check if the groups are valid
	for group, options := range m.Groups {
		if group == "" || options.DestinationURL == "" {
			errors = append(errors, fmt.Sprintf("  - invalid group mapping: %s", group))
		}
	}

	// Aggregate errors
	var err error = nil
	if len(errors) > 0 {
		err = fmt.Errorf("invalid mapping: %s", errors)
	}

	return err
}
