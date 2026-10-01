package mirroring

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.uber.org/zap"
)

const (
	// releaseAssetsPackageName is the destination generic package that stores the
	// release asset files that were not a generic package on the source instance.
	// Each release gets its own package version (the sanitized tag name).
	releaseAssetsPackageName = "release-assets"
	// releaseAssetTransferWorkers bounds the number of asset files a destination
	// instance downloads / uploads at the same time. Each file is fully held in
	// memory while it is transferred, so this also bounds the memory usage.
	releaseAssetTransferWorkers = 4
	// packagesPerPage is the page size used when listing packages and package files.
	packagesPerPage = 100
	// genericPackageType is the package_type of generic packages.
	genericPackageType = "generic"
	// defaultReleaseAssetFileName is used when no file name can be derived from a link.
	defaultReleaseAssetFileName = "asset"
	// defaultReleaseAssetVersion is used when a tag name sanitizes to nothing.
	defaultReleaseAssetVersion = "0"

	// apiGenericPackagePathSegments is the minimum number of segments of a generic
	// package file path: api/v4/projects/:id/packages/generic/:name/:version/:file.
	apiGenericPackagePathSegments = 9
	// releaseDownloadsMarker precedes the direct asset path in a release link direct_asset_url.
	releaseDownloadsMarker = "/downloads/"
	// releasesPathMarker precedes the tag name in a release link direct_asset_url.
	releasesPathMarker = "/-/releases/"
	// signInPathSuffix is where GitLab redirects unauthenticated requests to web routes.
	signInPathSuffix = "/users/sign_in"
)

// errSourceAssetMissing is returned when the file a source release link points to
// does not exist on the source instance (anymore).
var errSourceAssetMissing = errors.New("the file does not exist on the source instance")

// releaseAssetKind tells how a source release link is mirrored.
type releaseAssetKind int

const (
	// releaseAssetExternal links point outside of the source instance: they are copied unchanged.
	releaseAssetExternal releaseAssetKind = iota
	// releaseAssetGenericPackage links point to a generic package file of the source instance:
	// the file is republished under the same package name, version and file name.
	releaseAssetGenericPackage
	// releaseAssetInstanceFile links point to any other file of the source instance:
	// the file is stored in the releaseAssetsPackageName generic package.
	releaseAssetInstanceFile
)

// genericPackageFile identifies a file of a generic package.
type genericPackageFile struct {
	ProjectID string
	Name      string
	Version   string
	FileName  string
}

// releaseAssetPlan describes how a single source release link is mirrored.
type releaseAssetPlan struct {
	Link            *gitlab.ReleaseLink
	SourceURL       *url.URL
	Package         genericPackageFile
	DirectAssetPath string
	Kind            releaseAssetKind
}

// String describes what mirroring the asset does, for the dry run output.
func (p *releaseAssetPlan) String() string {
	switch p.Kind {
	case releaseAssetGenericPackage:
		return fmt.Sprintf("generic package file %s/%s/%s copied to the destination package registry (if it is missing there or its checksum differs; its copies are deleted if it no longer exists on the source)", p.Package.Name, p.Package.Version, p.Package.FileName)
	case releaseAssetInstanceFile:
		return fmt.Sprintf("file copied to the destination package registry as %s/%s/%s (if the link does not already exist)", p.Package.Name, p.Package.Version, p.Package.FileName)
	case releaseAssetExternal:
		return "external link copied unchanged (if the link does not already exist)"
	default:
		return "unknown asset kind"
	}
}

// ===========================================================================
//                       RELEASE ASSETS PLANNING                            //
// ===========================================================================

// instanceRootURL returns the web root of the instance behind client (its API base
// URL without the trailing api/v4/), always ending with a slash.
func instanceRootURL(client *gitlab.Client) *url.URL {
	root := client.BaseURL()
	root.Path = strings.TrimSuffix(strings.TrimSuffix(root.Path, "/"), "/api/v4") + "/"
	root.RawPath = ""
	root.RawQuery = ""
	root.Fragment = ""

	return root
}

// instanceRelativePath returns the escaped path of link relative to the instance
// root, and whether link points to that instance at all.
func instanceRelativePath(root, link *url.URL) (string, bool) {
	if !strings.EqualFold(root.Host, link.Host) {
		return "", false
	}

	linkPath := link.EscapedPath()
	rootPath := root.EscapedPath()

	if !strings.HasPrefix(linkPath, rootPath) {
		return "", false
	}

	return strings.TrimPrefix(linkPath, rootPath), true
}

// parseGenericPackagePath parses an escaped, instance relative path of the form
// api/v4/projects/:id/packages/generic/:name/:version/:file (the file name may
// span several segments).
func parseGenericPackagePath(relativePath string) (genericPackageFile, bool) {
	segments := strings.Split(relativePath, "/")
	if len(segments) < apiGenericPackagePathSegments ||
		segments[0] != "api" || segments[1] != "v4" || segments[2] != "projects" ||
		segments[4] != "packages" || segments[5] != "generic" {
		return genericPackageFile{}, false
	}

	// Keep :id and everything from :name on, dropping the fixed packages/generic segments.
	components := slices.Concat(segments[3:4], segments[6:])

	unescaped := make([]string, 0, len(components))
	for _, segment := range components {
		value, err := url.PathUnescape(segment)
		if err != nil || value == "" {
			return genericPackageFile{}, false
		}

		unescaped = append(unescaped, value)
	}

	return genericPackageFile{
		ProjectID: unescaped[0],
		Name:      unescaped[1],
		Version:   unescaped[2],
		FileName:  strings.Join(unescaped[3:], "/"),
	}, true
}

// directAssetPath extracts the direct asset path (e.g. "/binaries/app") from a link
// direct_asset_url. GitLab answers the link URL itself when no direct asset path is
// set, in which case an empty string is returned.
func directAssetPath(link *gitlab.ReleaseLink) string {
	_, afterReleases, found := strings.Cut(link.DirectAssetURL, releasesPathMarker)
	if !found {
		return ""
	}

	_, assetPath, found := strings.Cut(afterReleases, releaseDownloadsMarker)
	if !found || assetPath == "" {
		return ""
	}

	return "/" + assetPath
}

// sanitizePackageComponent replaces every character GitLab refuses in a generic
// package version or file name with a dash, and removes consecutive dots.
func sanitizePackageComponent(value string) string {
	sanitized := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.', r == '_', r == '-', r == '+', r == '~':
			return r
		default:
			return '-'
		}
	}, value)

	for strings.Contains(sanitized, "..") {
		sanitized = strings.ReplaceAll(sanitized, "..", ".")
	}

	return strings.Trim(sanitized, ".")
}

// releaseAssetVersion returns the generic package version used to store the assets of the release tagged tagName.
func releaseAssetVersion(tagName string) string {
	version := sanitizePackageComponent(tagName)
	if version == "" {
		return defaultReleaseAssetVersion
	}

	return version
}

// releaseAssetBaseFileName picks the file name an instance file is stored under:
// the last element of its direct asset path, or of its URL, or its link name.
func releaseAssetBaseFileName(plan *releaseAssetPlan) string {
	candidates := []string{path.Base(plan.DirectAssetPath), path.Base(plan.SourceURL.Path), plan.Link.Name}
	for _, candidate := range candidates {
		if fileName := sanitizePackageComponent(candidate); fileName != "" && fileName != "-" {
			return fileName
		}
	}

	return defaultReleaseAssetFileName
}

// planReleaseAssets classifies every link of a source release.
// sourceRoot is the web root of the source instance (see instanceRootURL).
//
// The files of links that are not a generic package are all stored in the same
// package version, so their file names must be unique: when several links share a
// file name, the one with the lowest ID keeps it and the others get their link ID
// as a prefix. Links added to the release later get higher IDs, so the files that
// were already mirrored keep their names across runs.
func planReleaseAssets(sourceRoot *url.URL, release *gitlab.Release) []*releaseAssetPlan {
	links := slices.DeleteFunc(slices.Clone(release.Assets.Links), func(link *gitlab.ReleaseLink) bool {
		return link == nil
	})
	slices.SortFunc(links, func(a, b *gitlab.ReleaseLink) int {
		return cmp.Compare(a.ID, b.ID)
	})

	plans := make([]*releaseAssetPlan, 0, len(links))
	usedFileNames := make(map[string]struct{}, len(links))

	for _, link := range links {
		plan := &releaseAssetPlan{Link: link, DirectAssetPath: directAssetPath(link), Kind: releaseAssetExternal}
		plans = append(plans, plan)

		linkURL, err := url.Parse(link.URL)
		if err != nil {
			continue
		}

		relativePath, onInstance := instanceRelativePath(sourceRoot, linkURL)
		if !onInstance {
			continue
		}

		plan.SourceURL = linkURL

		if packageFile, ok := parseGenericPackagePath(relativePath); ok {
			plan.Kind = releaseAssetGenericPackage
			plan.Package = packageFile

			continue
		}

		fileName := releaseAssetBaseFileName(plan)
		if _, used := usedFileNames[fileName]; used {
			fileName = fmt.Sprintf("%d-%s", link.ID, fileName)
		}

		usedFileNames[fileName] = struct{}{}

		plan.Kind = releaseAssetInstanceFile
		plan.Package = genericPackageFile{
			Name:     releaseAssetsPackageName,
			Version:  releaseAssetVersion(release.TagName),
			FileName: fileName,
		}
	}

	return plans
}

// ===========================================================================
//                        RELEASE ASSETS TRANSFER                           //
// ===========================================================================

// acquireAssetTransfer blocks until an asset transfer slot is available and
// returns the function releasing it.
func (g *GitlabInstance) acquireAssetTransfer() func() {
	if g.assetTransfers == nil {
		return func() {}
	}

	g.assetTransfers <- struct{}{}

	return func() { <-g.assetTransfers }
}

// downloadInstanceFile downloads an arbitrary file of the instance, authenticated
// with the instance token. link must point to the instance (see instanceRelativePath).
func (g *GitlabInstance) downloadInstanceFile(link *url.URL) ([]byte, error) {
	// Rebuild the URL on the configured base URL: the client refuses any other
	// scheme / host, and it keeps the token from being sent anywhere else.
	downloadURL := g.Gitlab.BaseURL()
	downloadURL.Path = link.Path
	downloadURL.RawPath = link.RawPath
	downloadURL.RawQuery = link.RawQuery
	downloadURL.Fragment = ""

	req, err := g.Gitlab.NewRequestToURL(http.MethodGet, downloadURL, nil, []gitlab.RequestOptionFunc{gitlab.WithHeader("Accept", "*/*")})
	if err != nil {
		return nil, fmt.Errorf("failed to build download request for %s: %w", downloadURL.Redacted(), err)
	}

	var content bytes.Buffer

	resp, err := g.Gitlab.Do(req, &content)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", downloadURL.Redacted(), err)
	}

	// Web routes that do not accept the API token redirect to the sign in page,
	// which would otherwise be mirrored as the asset content.
	if resp != nil && resp.Request != nil && strings.HasSuffix(resp.Request.URL.Path, signInPathSuffix) {
		return nil, fmt.Errorf("failed to download %s: the source instance redirected to its sign in page", downloadURL.Redacted())
	}

	return content.Bytes(), nil
}

// downloadReleaseAsset downloads the file a source release link points to.
func (g *GitlabInstance) downloadReleaseAsset(plan *releaseAssetPlan) ([]byte, error) {
	if plan.Kind == releaseAssetGenericPackage {
		content, _, err := g.Gitlab.GenericPackages.DownloadPackageFile(plan.Package.ProjectID, plan.Package.Name, plan.Package.Version, plan.Package.FileName)
		if errors.Is(err, gitlab.ErrNotFound) {
			return nil, fmt.Errorf("failed to download generic package file %s: %w: %w", plan.SourceURL.Redacted(), errSourceAssetMissing, err)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to download generic package file %s: %w", plan.SourceURL.Redacted(), err)
		}

		return content, nil
	}

	return g.downloadInstanceFile(plan.SourceURL)
}

// copyReleaseAssetFile copies the file of a source release link to the destination
// project generic package registry, and returns the URL of the copy.
func (destinationGitlab *GitlabInstance) copyReleaseAssetFile(sourceGitlab *GitlabInstance, destinationProject *gitlab.Project, plan *releaseAssetPlan) (string, error) {
	release := destinationGitlab.acquireAssetTransfer()
	defer release()

	content, err := sourceGitlab.downloadReleaseAsset(plan)
	if err != nil {
		return "", err
	}

	packageFile := plan.Package

	_, _, err = destinationGitlab.Gitlab.GenericPackages.PublishPackageFile(destinationProject.ID, packageFile.Name, packageFile.Version, packageFile.FileName, bytes.NewReader(content), nil)
	if err != nil {
		return "", fmt.Errorf("failed to publish generic package file %s/%s/%s: %w", packageFile.Name, packageFile.Version, packageFile.FileName, err)
	}

	return genericPackageFileURL(destinationGitlab.Gitlab, destinationProject, packageFile), nil
}

// genericPackageFileURL returns the download URL of a generic package file of project.
// It is built here rather than with GenericPackagesService.FormatPackageURL, which
// escapes every dot and would show links such as .../1%2E0%2E0/app%2Etar%2Egz.
func genericPackageFileURL(client *gitlab.Client, project *gitlab.Project, packageFile genericPackageFile) string {
	fileSegments := strings.Split(packageFile.FileName, "/")
	for i, segment := range fileSegments {
		fileSegments[i] = url.PathEscape(segment)
	}

	return fmt.Sprintf("%sprojects/%d/packages/generic/%s/%s/%s",
		client.BaseURL().String(),
		project.ID,
		url.PathEscape(packageFile.Name),
		url.PathEscape(packageFile.Version),
		strings.Join(fileSegments, "/"),
	)
}

// ===========================================================================
//                       GENERIC PACKAGE CHECKSUMS                          //
// ===========================================================================

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

			packageFiles, err := g.listPackageFilesNamed(pid, pkg.ID, packageFile.FileName)
			if err != nil {
				return nil, err
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
		zap.L().Info("Deleting package file", zap.String("file", file.File.FileName), zap.Int64("package", file.PackageID), zap.String(ROLE_DESTINATION, project.HTTPURLToRepo))

		_, err := g.Gitlab.Packages.DeletePackageFile(project.ID, file.PackageID, file.File.ID)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to delete file %s of package %d: %w", file.File.FileName, file.PackageID, err))
		}
	}

	return errors.Join(errs...)
}

// ===========================================================================
//                        RELEASE ASSETS MIRRORING                          //
// ===========================================================================

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

// createReleaseLink creates the destination release link of plan, pointing to linkURL.
func (destinationGitlab *GitlabInstance) createReleaseLink(destinationProject *gitlab.Project, tagName string, plan *releaseAssetPlan, linkURL string) error {
	linkOptions := &gitlab.CreateReleaseLinkOptions{
		Name: &plan.Link.Name,
		URL:  &linkURL,
	}
	if plan.DirectAssetPath != "" {
		linkOptions.DirectAssetPath = &plan.DirectAssetPath
	}

	if plan.Link.LinkType != "" {
		linkOptions.LinkType = &plan.Link.LinkType
	}

	_, _, err := destinationGitlab.Gitlab.ReleaseLinks.CreateReleaseLink(destinationProject.ID, tagName, linkOptions)
	if err != nil {
		return fmt.Errorf("failed to create release link: %w", err)
	}

	return nil
}

// updateReleaseLink points the existing destination release link of plan to linkURL.
func (destinationGitlab *GitlabInstance) updateReleaseLink(destinationProject *gitlab.Project, tagName string, plan *releaseAssetPlan, existingLink *gitlab.ReleaseLink, linkURL string) error {
	linkOptions := &gitlab.UpdateReleaseLinkOptions{
		URL: &linkURL,
	}
	if plan.DirectAssetPath != "" {
		linkOptions.DirectAssetPath = &plan.DirectAssetPath
	}

	if plan.Link.LinkType != "" {
		linkOptions.LinkType = &plan.Link.LinkType
	}

	_, _, err := destinationGitlab.Gitlab.ReleaseLinks.UpdateReleaseLink(destinationProject.ID, tagName, existingLink.ID, linkOptions)
	if err != nil {
		return fmt.Errorf("failed to update release link: %w", err)
	}

	return nil
}

// syncGenericPackageFile makes the destination copy of the generic package file of
// plan match the source file: it is copied when missing or when its checksum
// differs, and its copies are deleted when the file no longer exists on the source.
func (destinationGitlab *GitlabInstance) syncGenericPackageFile(sourceGitlab *GitlabInstance, destinationProject *gitlab.Project, tagName string, plan *releaseAssetPlan) error {
	destinationFiles, err := destinationGitlab.listGenericPackageFiles(destinationProject.ID, plan.Package)
	if err != nil {
		return fmt.Errorf("failed to look up the destination copy: %w", err)
	}

	sourceFiles, err := sourceGitlab.listGenericPackageFiles(plan.Package.ProjectID, plan.Package)
	if err != nil {
		return fmt.Errorf("failed to look up the source file: %w", err)
	}

	sourceFile := newestPackageFile(sourceFiles)
	if packageFilesMatch(sourceFile, newestPackageFile(destinationFiles)) {
		zap.L().Debug("generic package file is up to date", zap.String("release", tagName), zap.String("asset", plan.Link.Name), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))

		return nil
	}

	_, err = destinationGitlab.copyReleaseAssetFile(sourceGitlab, destinationProject, plan)
	// The file is only considered gone when the source package listing (which did
	// succeed, so the project is readable) does not have it AND its download answers
	// 404: a listing alone could miss a file stored under an unexpected name.
	if sourceFile == nil && errors.Is(err, errSourceAssetMissing) {
		zap.L().Warn("Release asset file no longer exists on the source, deleting its destination copies", zap.String("release", tagName), zap.String("asset", plan.Link.Name), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))

		return destinationGitlab.deletePackageFiles(destinationProject, destinationFiles)
	}

	return err
}

// mirrorGenericPackageAsset mirrors a release link pointing to a generic package file
// of the source instance (see syncGenericPackageFile). The link is then created, or
// updated when it points somewhere else than the destination copy.
func (destinationGitlab *GitlabInstance) mirrorGenericPackageAsset(sourceGitlab *GitlabInstance, destinationProject *gitlab.Project, tagName string, plan *releaseAssetPlan, existingLink *gitlab.ReleaseLink) error {
	err := destinationGitlab.syncGenericPackageFile(sourceGitlab, destinationProject, tagName, plan)
	if err != nil {
		return err
	}

	linkURL := genericPackageFileURL(destinationGitlab.Gitlab, destinationProject, plan.Package)

	switch {
	case existingLink == nil:
		return destinationGitlab.createReleaseLink(destinationProject, tagName, plan, linkURL)
	case existingLink.URL != linkURL:
		return destinationGitlab.updateReleaseLink(destinationProject, tagName, plan, existingLink, linkURL)
	default:
		return nil
	}
}

// MirrorReleaseAsset mirrors a single release link (and its file, when it lives on
// the source instance) to the release tagged tagName of the destination project.
// existingLink is the destination release link with the same name, if any.
//
// Generic package files are compared with their destination copy by checksum, so
// they are copied again when they changed or went missing, and their copies are
// deleted when they no longer exist on the source. Other links are only mirrored
// when existingLink is nil: their source offers no checksum to compare.
func (destinationGitlab *GitlabInstance) MirrorReleaseAsset(sourceGitlab *GitlabInstance, destinationProject *gitlab.Project, tagName string, plan *releaseAssetPlan, existingLink *gitlab.ReleaseLink) error {
	if plan == nil || plan.Link == nil {
		return errors.New("release asset link is nil")
	}

	zap.L().Debug("Mirroring release asset", zap.String("release", tagName), zap.String("asset", plan.Link.Name), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))

	if plan.Kind == releaseAssetGenericPackage {
		return destinationGitlab.mirrorGenericPackageAsset(sourceGitlab, destinationProject, tagName, plan, existingLink)
	}

	if existingLink != nil {
		zap.L().Debug("release asset already exists", zap.String("release", tagName), zap.String("asset", plan.Link.Name), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))

		return nil
	}

	linkURL := plan.Link.URL

	if plan.Kind == releaseAssetInstanceFile {
		copyURL, err := destinationGitlab.copyReleaseAssetFile(sourceGitlab, destinationProject, plan)
		if err != nil {
			return err
		}

		linkURL = copyURL
	}

	return destinationGitlab.createReleaseLink(destinationProject, tagName, plan, linkURL)
}

// DeleteStaleReleaseAsset deletes a destination release link that the source release
// no longer has, along with the file it points to (see deleteLinkedPackageFile).
func (destinationGitlab *GitlabInstance) DeleteStaleReleaseAsset(destinationProject *gitlab.Project, tagName string, link *gitlab.ReleaseLink, referenced map[genericPackageFile]struct{}) error {
	zap.L().Info("Deleting release asset that no longer exists on the source", zap.String("release", tagName), zap.String("asset", link.Name), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))

	_, _, err := destinationGitlab.Gitlab.ReleaseLinks.DeleteReleaseLink(destinationProject.ID, tagName, link.ID)
	if err != nil {
		return fmt.Errorf("failed to delete release link: %w", err)
	}

	return destinationGitlab.deleteLinkedPackageFile(destinationProject, link, referenced)
}

// deleteLinkedPackageFile deletes the file a deleted destination release link points
// to, when that file is a generic package file of the destination project that no
// source link is copied to (referenced, see referencedPackageFiles).
func (destinationGitlab *GitlabInstance) deleteLinkedPackageFile(destinationProject *gitlab.Project, link *gitlab.ReleaseLink, referenced map[genericPackageFile]struct{}) error {
	packageFile, ok := destinationGitlab.ownGenericPackageFile(destinationProject, link.URL)
	if !ok {
		return nil
	}

	if _, used := referenced[packageFile]; used {
		zap.L().Debug("Keeping package file still used by another release asset", zap.String("file", packageFile.FileName), zap.String(ROLE_DESTINATION, destinationProject.HTTPURLToRepo))

		return nil
	}

	files, err := destinationGitlab.listGenericPackageFiles(destinationProject.ID, packageFile)
	if err != nil {
		return fmt.Errorf("failed to look up the files of deleted link %s: %w", link.Name, err)
	}

	return destinationGitlab.deletePackageFiles(destinationProject, files)
}

// MirrorReleaseAssets mirrors the links of a source release to the destination
// release (see MirrorReleaseAsset), then deletes the destination links the source
// release no longer has (see DeleteStaleReleaseAsset). existingLinks are the links
// of the destination release, matched by name. referenced are the destination
// package files used by the links of every source release of the project; when
// nil, only the links of release are considered.
// Every failure is logged as it happens via helpers.Report.
func (destinationGitlab *GitlabInstance) MirrorReleaseAssets(sourceGitlab *GitlabInstance, destinationProject *gitlab.Project, release *gitlab.Release, existingLinks []*gitlab.ReleaseLink, referenced map[genericPackageFile]struct{}) {
	sourceRoot := instanceRootURL(sourceGitlab.Gitlab)
	if referenced == nil {
		referenced = referencedPackageFiles(sourceRoot, []*gitlab.Release{release})
	}

	linksByName := make(map[string]*gitlab.ReleaseLink, len(existingLinks))
	for _, link := range existingLinks {
		if link != nil {
			linksByName[link.Name] = link
		}
	}

	for _, plan := range planReleaseAssets(sourceRoot, release) {
		err := destinationGitlab.MirrorReleaseAsset(sourceGitlab, destinationProject, release.TagName, plan, linksByName[plan.Link.Name])
		if err != nil {
			helpers.Report(fmt.Errorf("failed to mirror asset %s of release %s in project %s: %w", plan.Link.Name, release.TagName, destinationProject.HTTPURLToRepo, err))
		}

		delete(linksByName, plan.Link.Name)
	}

	// What is left are the destination links the source release does not have.
	for _, staleLink := range linksByName {
		err := destinationGitlab.DeleteStaleReleaseAsset(destinationProject, release.TagName, staleLink, referenced)
		if err != nil {
			helpers.Report(fmt.Errorf("failed to delete asset %s of release %s in project %s: %w", staleLink.Name, release.TagName, destinationProject.HTTPURLToRepo, err))
		}
	}
}
