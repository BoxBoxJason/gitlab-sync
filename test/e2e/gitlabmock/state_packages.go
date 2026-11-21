package gitlabmock

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

// AddPackageFile publishes a file of a generic package of a project.
func (s *Server) AddPackageFile(fullPath, name, version, fileName string, content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	s.publishPackageFile(target, name, version, fileName, content)

	return nil
}

// publishPackageFile adds a file to a generic package, creating the package when
// needed. The caller holds the lock.
func (s *Server) publishPackageFile(target *project, name, version, fileName string, content []byte) {
	var record *packageRecord

	for _, candidate := range target.packages {
		if candidate.Name == name && candidate.Version == version {
			record = candidate
		}
	}

	if record == nil {
		record = &packageRecord{Package: gitlab.Package{ID: s.id(), Name: name, Version: version, PackageType: "generic"}}
		target.packages = append(target.packages, record)
	}

	sum := sha256.Sum256(content)
	record.files = append(record.files, &packageFile{
		PackageFile: gitlab.PackageFile{ID: s.id(), PackageID: record.ID, FileName: fileName, Size: int64(len(content)), FileSHA256: hex.EncodeToString(sum[:])},
		content:     content,
	})
}

// RemovePackageFile removes every copy of a generic package file of a project
// (the files of a package are kept even when none is left).
func (s *Server) RemovePackageFile(fullPath, name, version, fileName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	for _, record := range target.packages {
		if record.Name != name || record.Version != version {
			continue
		}

		kept := record.files[:0]

		for _, file := range record.files {
			if file.FileName != fileName {
				kept = append(kept, file)
			}
		}

		record.files = kept
	}

	return nil
}
