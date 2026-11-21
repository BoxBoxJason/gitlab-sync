package mirroring

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.uber.org/zap"
)

// acquireAssetTransfer blocks until an asset transfer slot is available and
// returns the function releasing it.
func (g *GitlabInstance) acquireAssetTransfer() func() {
	if g.assetTransfers == nil {
		return func() {}
	}

	g.assetTransfers <- struct{}{}

	return func() { <-g.assetTransfers }
}

// downloadInstanceFile streams an arbitrary file of the instance to output, authenticated
// with the instance token. link must point to the instance (see instanceRelativePath).
func (g *GitlabInstance) downloadInstanceFile(link *url.URL, output io.Writer) error {
	// Rebuild the URL on the configured base URL: the client refuses any other
	// scheme / host. Redirects to other hosts drop the token (see stripTokenOnCrossHostRedirect).
	downloadURL := g.Gitlab.BaseURL()
	downloadURL.Path = link.Path
	downloadURL.RawPath = link.RawPath
	downloadURL.RawQuery = link.RawQuery
	downloadURL.Fragment = ""

	req, err := g.Gitlab.NewRequestToURL(http.MethodGet, downloadURL, nil, []gitlab.RequestOptionFunc{gitlab.WithHeader("Accept", "*/*")})
	if err != nil {
		return fmt.Errorf("failed to build download request for %s: %w", downloadURL.Redacted(), err)
	}

	resp, err := g.Gitlab.Do(req, output)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", downloadURL.Redacted(), err)
	}

	// Web routes that do not accept the API token redirect to the sign in page,
	// which would otherwise be mirrored as the asset content.
	if resp != nil && resp.Request != nil && strings.HasSuffix(resp.Request.URL.Path, signInPathSuffix) {
		return fmt.Errorf("failed to download %s: the source instance redirected to its sign in page", downloadURL.Redacted())
	}

	return nil
}

// downloadPackageFile streams the generic package file of plan to output.
// GenericPackagesService.DownloadPackageFile is not used: it buffers the whole file.
func (g *GitlabInstance) downloadPackageFile(plan *releaseAssetPlan, output io.Writer) error {
	packageFile := plan.Package

	packagePath, err := g.Gitlab.GenericPackages.FormatPackageURL(packageFile.ProjectID, packageFile.Name, packageFile.Version, packageFile.FileName)
	if err != nil {
		return fmt.Errorf("failed to build generic package file path %s: %w", plan.SourceURL.Redacted(), err)
	}

	req, err := g.Gitlab.NewRequest(http.MethodGet, packagePath, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to build download request for generic package file %s: %w", plan.SourceURL.Redacted(), err)
	}

	_, err = g.Gitlab.Do(req, output)
	if errors.Is(err, gitlab.ErrNotFound) {
		return fmt.Errorf("failed to download generic package file %s: %w: %w", plan.SourceURL.Redacted(), errSourceAssetMissing, err)
	}

	if err != nil {
		return fmt.Errorf("failed to download generic package file %s: %w", plan.SourceURL.Redacted(), err)
	}

	return nil
}

// downloadReleaseAsset streams the file a source release link points to into output.
func (g *GitlabInstance) downloadReleaseAsset(plan *releaseAssetPlan, output io.Writer) error {
	if plan.Kind == releaseAssetGenericPackage {
		return g.downloadPackageFile(plan, output)
	}

	return g.downloadInstanceFile(plan.SourceURL, output)
}

// spooledAsset is the upload body of a release asset spooled to disk. It is an
// io.ReadSeeker with a Len, so the HTTP client streams it with a Content-Length and
// rewinds it on retries, instead of reading it all in memory as it does for a plain io.Reader.
type spooledAsset struct {
	*io.SectionReader
}

// Len returns the size of the spooled asset.
func (a spooledAsset) Len() int {
	return int(a.Size())
}

// copyReleaseAssetFile copies the file of a source release link to the destination
// project generic package registry, and returns the URL of the copy. The file is
// spooled to a temporary file rather than held in memory, as release assets can be
// large binaries.
func (g *GitlabInstance) copyReleaseAssetFile(sourceGitlab *GitlabInstance, destinationProject *gitlab.Project, plan *releaseAssetPlan) (string, error) {
	release := g.acquireAssetTransfer()
	defer release()

	spool, err := os.CreateTemp("", releaseAssetSpoolPattern)
	if err != nil {
		return "", fmt.Errorf("failed to create a temporary file for release asset %s: %w", plan.SourceURL.Redacted(), err)
	}

	defer func() {
		closeErr := spool.Close()
		removeErr := os.Remove(spool.Name())

		if closeErr != nil || removeErr != nil {
			zap.L().Warn("Failed to clean up the temporary file of a release asset", zap.String("file", spool.Name()), zap.Error(errors.Join(closeErr, removeErr)))
		}
	}()

	err = sourceGitlab.downloadReleaseAsset(plan, spool)
	if err != nil {
		return "", err
	}

	size, err := spool.Seek(0, io.SeekCurrent)
	if err != nil {
		return "", fmt.Errorf("failed to read the size of release asset %s: %w", plan.SourceURL.Redacted(), err)
	}

	packageFile := plan.Package
	content := spooledAsset{io.NewSectionReader(spool, 0, size)}

	_, _, err = g.Gitlab.GenericPackages.PublishPackageFile(destinationProject.ID, packageFile.Name, packageFile.Version, packageFile.FileName, content, nil)
	if err != nil {
		return "", fmt.Errorf("failed to publish generic package file %s/%s/%s: %w", packageFile.Name, packageFile.Version, packageFile.FileName, err)
	}

	return genericPackageFileURL(g.Gitlab, destinationProject, packageFile), nil
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
