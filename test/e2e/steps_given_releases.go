//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

// release creates a release, along with its tag on the default branch when the
// project does not have it yet.
func (s *steps) release(role, name, tag string) error {
	fullPath := s.w.fullPath(role, name)

	_, _, err := s.w.env.client.Releases.CreateRelease(fullPath, &gitlab.CreateReleaseOptions{
		TagName:     &tag,
		Name:        new("Release " + tag),
		Description: new("notes of " + tag),
		Ref:         new(defaultBranch),
	})
	if err != nil {
		return fmt.Errorf("create the release %s of %s: %w", tag, fullPath, err)
	}

	return nil
}

func (s *steps) addReleaseLink(name, tag string, options *gitlab.CreateReleaseLinkOptions) error {
	fullPath := s.w.fullPath(roleSource, name)

	_, _, err := s.w.env.client.ReleaseLinks.CreateReleaseLink(fullPath, tag, options)
	if err != nil {
		return fmt.Errorf("link %s to the release %s of %s: %w", *options.URL, tag, fullPath, err)
	}

	return nil
}

func (s *steps) releaseLink(name, tag, link, target string) error {
	target = s.w.expand(target)

	return s.addReleaseLink(name, tag, &gitlab.CreateReleaseLinkOptions{Name: &link, URL: &target})
}

func (s *steps) releaseLinkToPackage(name, tag, link, file, directPath string) error {
	spec, err := splitPackageFile(file)
	if err != nil {
		return err
	}

	target := fmt.Sprintf("%s/api/v4/projects/%s/packages/generic/%s/%s/%s",
		s.w.url(roleSource), url.PathEscape(s.w.fullPath(roleSource, name)), spec.name, spec.version, spec.file)

	return s.addReleaseLink(name, tag, &gitlab.CreateReleaseLinkOptions{Name: &link, URL: &target, DirectAssetPath: &directPath})
}

func (s *steps) releaseLinkToUpload(name, tag, link, file string) error {
	target, ok := s.w.uploads[file]
	if !ok {
		return fmt.Errorf("no file %q was uploaded", file)
	}

	return s.addReleaseLink(name, tag, &gitlab.CreateReleaseLinkOptions{Name: &link, URL: &target})
}

// uploadedFile attaches a file to a project: it is served by the instance, outside
// of the package registry.
func (s *steps) uploadedFile(name, file, content string) error {
	fullPath := s.w.fullPath(roleSource, name)

	uploaded, _, err := s.w.env.client.ProjectMarkdownUploads.UploadProjectMarkdown(fullPath, strings.NewReader(content), file)
	if err != nil {
		return fmt.Errorf("upload %s to %s: %w", file, fullPath, err)
	}

	s.w.uploads[file] = s.w.url(roleSource) + uploaded.FullPath

	return nil
}

func (s *steps) packageFile(name, file, content string) error {
	spec, err := splitPackageFile(file)
	if err != nil {
		return err
	}

	fullPath := s.w.fullPath(roleSource, name)

	_, _, err = s.w.env.client.GenericPackages.PublishPackageFile(fullPath, spec.name, spec.version, spec.file, bytes.NewReader([]byte(content)), nil)
	if err != nil {
		return fmt.Errorf("publish %s to %s: %w", file, fullPath, err)
	}

	return nil
}

func (s *steps) removePackageFile(name, file string) error {
	fullPath := s.w.fullPath(roleSource, name)

	found, err := s.packageFiles(fullPath)
	if err != nil {
		return err
	}

	removed := 0

	for _, candidate := range found {
		if candidate.path != file {
			continue
		}

		_, err = s.w.env.client.Packages.DeletePackageFile(fullPath, candidate.packageID, candidate.id)
		if err != nil {
			return fmt.Errorf("delete %s of %s: %w", file, fullPath, err)
		}

		removed++
	}

	if removed == 0 {
		return fmt.Errorf("%s has no package file %s", fullPath, file)
	}

	return nil
}

func (s *steps) removeRelease(name, tag string) error {
	fullPath := s.w.fullPath(roleSource, name)

	_, _, err := s.w.env.client.Releases.DeleteRelease(fullPath, tag)
	if err != nil {
		return fmt.Errorf("delete the release %s of %s: %w", tag, fullPath, err)
	}

	return nil
}

func (s *steps) removeReleaseLink(name, tag, link string) error {
	fullPath := s.w.fullPath(roleSource, name)

	links, response, err := s.w.env.client.ReleaseLinks.ListReleaseLinks(fullPath, tag, nil)
	if err != nil && (response == nil || response.StatusCode != http.StatusNotFound) {
		return fmt.Errorf("list the links of the release %s of %s: %w", tag, fullPath, err)
	}

	for _, candidate := range links {
		if candidate.Name != link {
			continue
		}

		_, _, err = s.w.env.client.ReleaseLinks.DeleteReleaseLink(fullPath, tag, candidate.ID)
		if err != nil {
			return fmt.Errorf("delete the link %s of %s: %w", link, fullPath, err)
		}

		return nil
	}

	return fmt.Errorf("the release %s of %s has no link %q", tag, fullPath, link)
}

// packageFile is a generic package file: name/version/file.
type packageFile struct {
	name, version, file string
}

// splitPackageFile splits "name/version/file".
func splitPackageFile(spec string) (packageFile, error) {
	parts := strings.SplitN(spec, "/", packageFileParts)

	if len(parts) != packageFileParts {
		return packageFile{}, fmt.Errorf("%q is not a package file, want name/version/file", spec)
	}

	return packageFile{name: parts[0], version: parts[1], file: parts[2]}, nil
}
