package mirroring

import (
	"cmp"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

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
