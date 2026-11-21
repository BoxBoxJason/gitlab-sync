package mirroring

import (
	"errors"
	"fmt"

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
	// instance downloads / uploads at the same time. Each file is spooled to a
	// temporary file while it is transferred, so this also bounds the disk usage.
	releaseAssetTransferWorkers = 4
	// packagesPerPage is the page size used when listing packages and package files.
	packagesPerPage = 100
	// genericPackageType is the package_type of generic packages.
	genericPackageType = "generic"
	// defaultReleaseAssetFileName is used when no file name can be derived from a link.
	defaultReleaseAssetFileName = "asset"
	// releaseAssetSpoolPattern names the temporary files release assets are spooled to.
	releaseAssetSpoolPattern = "gitlab-sync-release-asset-*"
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

var (
	// errSourceAssetMissing is returned when the file a source release link points to
	// does not exist on the source instance (anymore).
	errSourceAssetMissing = errors.New("the file does not exist on the source instance")
)

// releaseAssetKind tells how a source release link is mirrored.
type releaseAssetKind int

// ===========================================================================
//                       RELEASE ASSETS PLANNING                            //
// ===========================================================================

// ===========================================================================
//                        RELEASE ASSETS TRANSFER                           //
// ===========================================================================

// ===========================================================================
//                       GENERIC PACKAGE CHECKSUMS                          //
// ===========================================================================

// ===========================================================================
//                        RELEASE ASSETS MIRRORING                          //
// ===========================================================================

// createReleaseLink creates the destination release link of plan, pointing to linkURL.
func (g *GitlabInstance) createReleaseLink(destinationProject *gitlab.Project, tagName string, plan *releaseAssetPlan, linkURL string) error {
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

	_, _, err := g.Gitlab.ReleaseLinks.CreateReleaseLink(destinationProject.ID, tagName, linkOptions)
	if err != nil {
		return fmt.Errorf("failed to create release link: %w", err)
	}

	return nil
}

// updateReleaseLink points the existing destination release link of plan to linkURL.
func (g *GitlabInstance) updateReleaseLink(destinationProject *gitlab.Project, tagName string, plan *releaseAssetPlan, existingLink *gitlab.ReleaseLink, linkURL string) error {
	linkOptions := &gitlab.UpdateReleaseLinkOptions{
		URL: &linkURL,
	}
	if plan.DirectAssetPath != "" {
		linkOptions.DirectAssetPath = &plan.DirectAssetPath
	}

	if plan.Link.LinkType != "" {
		linkOptions.LinkType = &plan.Link.LinkType
	}

	_, _, err := g.Gitlab.ReleaseLinks.UpdateReleaseLink(destinationProject.ID, tagName, existingLink.ID, linkOptions)
	if err != nil {
		return fmt.Errorf("failed to update release link: %w", err)
	}

	return nil
}

// syncGenericPackageFile makes the destination copy of the generic package file of
// plan match the source file: it is copied when missing or when its checksum
// differs, and its copies are deleted when the file no longer exists on the source.
func (g *GitlabInstance) syncGenericPackageFile(sourceGitlab *GitlabInstance, destinationProject *gitlab.Project, tagName string, plan *releaseAssetPlan) error {
	destinationFiles, err := g.listGenericPackageFiles(destinationProject.ID, plan.Package)
	if err != nil {
		return fmt.Errorf("failed to look up the destination copy: %w", err)
	}

	sourceFiles, err := sourceGitlab.listGenericPackageFiles(plan.Package.ProjectID, plan.Package)
	if err != nil {
		return fmt.Errorf("failed to look up the source file: %w", err)
	}

	sourceFile := newestPackageFile(sourceFiles)
	if packageFilesMatch(sourceFile, newestPackageFile(destinationFiles)) {
		zap.L().Debug("generic package file is up to date", zap.String("release", tagName), zap.String("asset", plan.Link.Name), zap.String(RoleDestination, destinationProject.HTTPURLToRepo))

		return nil
	}

	_, err = g.copyReleaseAssetFile(sourceGitlab, destinationProject, plan)
	// The file is only considered gone when the source package listing (which did
	// succeed, so the project is readable) does not have it AND its download answers
	// 404: a listing alone could miss a file stored under an unexpected name.
	if sourceFile == nil && errors.Is(err, errSourceAssetMissing) {
		zap.L().Warn("Release asset file no longer exists on the source, deleting its destination copies", zap.String("release", tagName), zap.String("asset", plan.Link.Name), zap.String(RoleDestination, destinationProject.HTTPURLToRepo))

		return g.deletePackageFiles(destinationProject, destinationFiles)
	}

	return err
}

// mirrorGenericPackageAsset mirrors a release link pointing to a generic package file
// of the source instance (see syncGenericPackageFile). The link is then created, or
// updated when it points somewhere else than the destination copy.
func (g *GitlabInstance) mirrorGenericPackageAsset(sourceGitlab *GitlabInstance, destinationProject *gitlab.Project, tagName string, plan *releaseAssetPlan, existingLink *gitlab.ReleaseLink) error {
	err := g.syncGenericPackageFile(sourceGitlab, destinationProject, tagName, plan)
	if err != nil {
		return err
	}

	linkURL := genericPackageFileURL(g.Gitlab, destinationProject, plan.Package)

	switch {
	case existingLink == nil:
		return g.createReleaseLink(destinationProject, tagName, plan, linkURL)
	case existingLink.URL != linkURL:
		return g.updateReleaseLink(destinationProject, tagName, plan, existingLink, linkURL)
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
func (g *GitlabInstance) MirrorReleaseAsset(sourceGitlab *GitlabInstance, destinationProject *gitlab.Project, tagName string, plan *releaseAssetPlan, existingLink *gitlab.ReleaseLink) error {
	if plan == nil || plan.Link == nil {
		return errors.New("release asset link is nil")
	}

	zap.L().Debug("Mirroring release asset", zap.String("release", tagName), zap.String("asset", plan.Link.Name), zap.String(RoleDestination, destinationProject.HTTPURLToRepo))

	if plan.Kind == releaseAssetGenericPackage {
		return g.mirrorGenericPackageAsset(sourceGitlab, destinationProject, tagName, plan, existingLink)
	}

	if existingLink != nil {
		zap.L().Debug("release asset already exists", zap.String("release", tagName), zap.String("asset", plan.Link.Name), zap.String(RoleDestination, destinationProject.HTTPURLToRepo))

		return nil
	}

	linkURL := plan.Link.URL

	if plan.Kind == releaseAssetInstanceFile {
		copyURL, err := g.copyReleaseAssetFile(sourceGitlab, destinationProject, plan)
		if err != nil {
			return err
		}

		linkURL = copyURL
	}

	return g.createReleaseLink(destinationProject, tagName, plan, linkURL)
}

// DeleteStaleReleaseAsset deletes a destination release link that the source release
// no longer has, along with the file it points to (see deleteLinkedPackageFile).
func (g *GitlabInstance) DeleteStaleReleaseAsset(destinationProject *gitlab.Project, tagName string, link *gitlab.ReleaseLink, referenced map[genericPackageFile]struct{}) error {
	zap.L().Info("Deleting release asset that no longer exists on the source", zap.String("release", tagName), zap.String("asset", link.Name), zap.String(RoleDestination, destinationProject.HTTPURLToRepo))

	_, _, err := g.Gitlab.ReleaseLinks.DeleteReleaseLink(destinationProject.ID, tagName, link.ID)
	if err != nil {
		return fmt.Errorf("failed to delete release link: %w", err)
	}

	return g.deleteLinkedPackageFile(destinationProject, link, referenced)
}

// deleteLinkedPackageFile deletes the file a deleted destination release link points
// to, when that file is a generic package file of the destination project that no
// source link is copied to (referenced, see referencedPackageFiles).
func (g *GitlabInstance) deleteLinkedPackageFile(destinationProject *gitlab.Project, link *gitlab.ReleaseLink, referenced map[genericPackageFile]struct{}) error {
	packageFile, ok := g.ownGenericPackageFile(destinationProject, link.URL)
	if !ok {
		return nil
	}

	if _, used := referenced[packageFile]; used {
		zap.L().Debug("Keeping package file still used by another release asset", zap.String("file", packageFile.FileName), zap.String(RoleDestination, destinationProject.HTTPURLToRepo))

		return nil
	}

	files, err := g.listGenericPackageFiles(destinationProject.ID, packageFile)
	if err != nil {
		return fmt.Errorf("failed to look up the files of deleted link %s: %w", link.Name, err)
	}

	return g.deletePackageFiles(destinationProject, files)
}

// MirrorReleaseAssets mirrors the links of a source release to the destination
// release (see MirrorReleaseAsset), then deletes the destination links the source
// release no longer has (see DeleteStaleReleaseAsset). existingLinks are the links
// of the destination release, matched by name. referenced are the destination
// package files used by the links of every source release of the project; when
// nil, only the links of release are considered.
// Every failure is logged as it happens via helpers.Report.
func (g *GitlabInstance) MirrorReleaseAssets(sourceGitlab *GitlabInstance, destinationProject *gitlab.Project, release *gitlab.Release, existingLinks []*gitlab.ReleaseLink, referenced map[genericPackageFile]struct{}) {
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
		err := g.MirrorReleaseAsset(sourceGitlab, destinationProject, release.TagName, plan, linksByName[plan.Link.Name])
		if err != nil {
			helpers.Report(fmt.Errorf("failed to mirror asset %s of release %s in project %s: %w", plan.Link.Name, release.TagName, destinationProject.HTTPURLToRepo, err))
		}

		delete(linksByName, plan.Link.Name)
	}

	// What is left are the destination links the source release does not have.
	for _, staleLink := range linksByName {
		err := g.DeleteStaleReleaseAsset(destinationProject, release.TagName, staleLink, referenced)
		if err != nil {
			helpers.Report(fmt.Errorf("failed to delete asset %s of release %s in project %s: %w", staleLink.Name, release.TagName, destinationProject.HTTPURLToRepo, err))
		}
	}
}
