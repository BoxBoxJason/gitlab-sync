package mirroring

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.uber.org/zap"
)

// packageFileRef is a file of a package of the package registry.
type packageFileRef struct {
	File      *gitlab.PackageFile
	PackageID int64
}

// listGenericPackageFiles returns every file named like packageFile in the generic
// package packageFile.Name / packageFile.Version of project pid. A package can hold
// several files with the same name: GitLab serves the newest one (see newestPackageFile).
func (g *GitlabInstance) listGenericPackageFiles(pid any, packageFile genericPackageFile) ([]packageFileRef, error) {
	listOptions := &gitlab.ListProjectPackagesOptions{
		ListOptions:    gitlab.ListOptions{PerPage: packagesPerPage, Page: 1},
		PackageType:    new(genericPackageType),
		PackageName:    &packageFile.Name,
		PackageVersion: &packageFile.Version,
	}

	var files []packageFileRef

	for {
		packages, resp, err := g.Gitlab.Packages.ListProjectPackages(pid, listOptions)
		if err != nil {
			return nil, fmt.Errorf("failed to list generic packages %s/%s: %w", packageFile.Name, packageFile.Version, err)
		}

		for _, pkg := range packages {
			// The package_name filter is a partial match: keep the exact package only.
			if pkg == nil || pkg.Name != packageFile.Name || pkg.Version != packageFile.Version {
				continue
			}

			packageFiles, listErr := g.listPackageFilesNamed(pid, pkg.ID, packageFile.FileName)
			if listErr != nil {
				return nil, listErr
			}

			files = append(files, packageFiles...)
		}

		if resp == nil || resp.NextPage == 0 {
			return files, nil
		}

		listOptions.Page = resp.NextPage
	}
}

// listPackageFilesNamed returns the files named fileName of package packageID of project pid.
func (g *GitlabInstance) listPackageFilesNamed(pid any, packageID int64, fileName string) ([]packageFileRef, error) {
	listOptions := &gitlab.ListPackageFilesOptions{
		ListOptions: gitlab.ListOptions{PerPage: packagesPerPage, Page: 1},
	}

	var files []packageFileRef

	for {
		packageFiles, resp, err := g.Gitlab.Packages.ListPackageFiles(pid, packageID, listOptions)
		if err != nil {
			return nil, fmt.Errorf("failed to list the files of package %d: %w", packageID, err)
		}

		for _, file := range packageFiles {
			if file != nil && file.FileName == fileName {
				files = append(files, packageFileRef{File: file, PackageID: packageID})
			}
		}

		if resp == nil || resp.NextPage == 0 {
			return files, nil
		}

		listOptions.Page = resp.NextPage
	}
}

// newestPackageFile returns the most recently uploaded file (the one with the highest ID), or nil when files is empty.
func newestPackageFile(files []packageFileRef) *gitlab.PackageFile {
	var newest *gitlab.PackageFile

	for _, file := range files {
		if newest == nil || file.File.ID > newest.ID {
			newest = file.File
		}
	}

	return newest
}

// packageFilesMatch tells whether two package files have the same content, using
// the checksums GitLab computed when they were uploaded (no download involved).
// The strongest checksum known on both sides is compared; files without any
// common checksum are considered different, so that they get copied.
func packageFilesMatch(source, destination *gitlab.PackageFile) bool {
	if source == nil || destination == nil || source.Size != destination.Size {
		return false
	}

	checksums := [][2]string{
		{source.FileSHA256, destination.FileSHA256},
		{source.FileSHA1, destination.FileSHA1},
		{source.FileMD5, destination.FileMD5},
	}
	for _, checksum := range checksums {
		if checksum[0] != "" && checksum[1] != "" {
			return strings.EqualFold(checksum[0], checksum[1])
		}
	}

	return false
}

// deletePackageFiles deletes files from the package registry of project.
func (g *GitlabInstance) deletePackageFiles(project *gitlab.Project, files []packageFileRef) error {
	var errs []error

	for _, file := range files {
		zap.L().Info("Deleting package file", zap.String("file", file.File.FileName), zap.Int64("package", file.PackageID), zap.String(RoleDestination, project.HTTPURLToRepo))

		_, err := g.Gitlab.Packages.DeletePackageFile(project.ID, file.PackageID, file.File.ID)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to delete file %s of package %d: %w", file.File.FileName, file.PackageID, err))
		}
	}

	return errors.Join(errs...)
}

// destinationPackageFileKey identifies, among the release assets referencedFiles,
// the destination package file the asset of plan is copied to. The project is left
// out: every copy lives in the destination project.
func destinationPackageFileKey(plan *releaseAssetPlan) (genericPackageFile, bool) {
	if plan.Kind == releaseAssetExternal {
		return genericPackageFile{}, false
	}

	return genericPackageFile{Name: plan.Package.Name, Version: plan.Package.Version, FileName: plan.Package.FileName}, true
}

// referencedPackageFiles returns the destination package files the links of
// releases are copied to. A file in this set must never be deleted, even when a
// destination link pointing to it is.
func referencedPackageFiles(sourceRoot *url.URL, releases []*gitlab.Release) map[genericPackageFile]struct{} {
	referenced := make(map[genericPackageFile]struct{})

	for _, release := range releases {
		if release == nil {
			continue
		}

		for _, plan := range planReleaseAssets(sourceRoot, release) {
			if key, ok := destinationPackageFileKey(plan); ok {
				referenced[key] = struct{}{}
			}
		}
	}

	return referenced
}

// ownGenericPackageFile tells whether linkURL points to a generic package file of
// project on this instance, and returns it (without project, like destinationPackageFileKey).
func (g *GitlabInstance) ownGenericPackageFile(project *gitlab.Project, linkURL string) (genericPackageFile, bool) {
	parsedURL, err := url.Parse(linkURL)
	if err != nil {
		return genericPackageFile{}, false
	}

	relativePath, onInstance := instanceRelativePath(instanceRootURL(g.Gitlab), parsedURL)
	if !onInstance {
		return genericPackageFile{}, false
	}

	packageFile, ok := parseGenericPackagePath(relativePath)
	if !ok {
		return genericPackageFile{}, false
	}

	if packageFile.ProjectID != strconv.FormatInt(project.ID, 10) && !strings.EqualFold(packageFile.ProjectID, project.PathWithNamespace) {
		return genericPackageFile{}, false
	}

	packageFile.ProjectID = ""

	return packageFile, true
}
