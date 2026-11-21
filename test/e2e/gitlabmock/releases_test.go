package gitlabmock_test

import (
	"bytes"
	"net/http"
	"strconv"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

func TestIssuesCanBeCreatedClosedAndListedByState(t *testing.T) {
	t.Parallel()

	server := newServer(t)
	require.NoError(t, server.AddProject("g/app"))
	require.NoError(t, server.AddIssue("g/app", "old", "body", "closed"))

	client := newClient(t, server)
	project, _ := server.Project("g/app")

	created, _, err := client.Issues.CreateIssue(project.ID, &gitlab.CreateIssueOptions{Title: new("new"), Description: new("text")})
	require.NoError(t, err)
	assert.Equal(t, "opened", created.State)

	_, _, err = client.Issues.UpdateIssue(project.ID, created.IID, &gitlab.UpdateIssueOptions{StateEvent: new("close")})
	require.NoError(t, err)

	opened, _, err := client.Issues.ListProjectIssues(project.ID, &gitlab.ListProjectIssuesOptions{State: new("opened")})
	require.NoError(t, err)
	assert.Empty(t, opened)

	all, _, err := client.Issues.ListProjectIssues(project.ID, nil)
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestReleasesLinksAndPackages(t *testing.T) {
	t.Parallel()

	server := newServer(t)
	require.NoError(t, server.AddProject("g/app"))
	require.NoError(t, server.AddPackageFile("g/app", "tool", "1.0.0", "tool.bin", []byte("payload")))

	client := newClient(t, server)
	project, _ := server.Project("g/app")

	_, _, err := client.Releases.CreateRelease(project.ID, &gitlab.CreateReleaseOptions{TagName: new("v1"), Name: new("One")})
	require.NoError(t, err)

	link, _, err := client.ReleaseLinks.CreateReleaseLink(project.ID, "v1", &gitlab.CreateReleaseLinkOptions{
		Name: new("tool"), URL: new(server.URL() + "/api/v4/projects/" + "g%2Fapp" + "/packages/generic/tool/1.0.0/tool.bin"), DirectAssetPath: new("/bin/tool"),
	})
	require.NoError(t, err)
	assert.Equal(t, server.URL()+"/g/app/-/releases/v1/downloads/bin/tool", link.DirectAssetURL)

	releases, _, err := client.Releases.ListReleases(project.ID, nil)
	require.NoError(t, err)
	require.Len(t, releases, 1)
	require.Len(t, releases[0].Assets.Links, 1)

	// The direct asset URL redirects to the link URL, which serves the package file.
	got := get(t, link.DirectAssetURL)
	assert.Equal(t, "payload", string(got.body))

	packages, _, err := client.Packages.ListProjectPackages(project.ID, &gitlab.ListProjectPackagesOptions{PackageType: new("generic")})
	require.NoError(t, err)
	require.Len(t, packages, 1)

	files, _, err := client.Packages.ListPackageFiles(project.ID, packages[0].ID, nil)
	require.NoError(t, err)
	require.Len(t, files, 1)

	_, err = client.Packages.DeletePackageFile(project.ID, packages[0].ID, files[0].ID)
	require.NoError(t, err)
	assert.Empty(t, server.PackageFiles("g/app"))

	_, _, err = client.ReleaseLinks.DeleteReleaseLink(project.ID, "v1", link.ID)
	require.NoError(t, err)

	_, _, err = client.Releases.DeleteRelease(project.ID, "v1")
	require.NoError(t, err)
	assert.Empty(t, server.Releases("g/app"))
}

func TestPublishGenericPackageFile(t *testing.T) {
	t.Parallel()

	server := newServer(t)
	require.NoError(t, server.AddProject("g/app"))

	client := newClient(t, server)
	project, _ := server.Project("g/app")

	_, _, err := client.GenericPackages.PublishPackageFile(project.ID, "release-assets", "v1", "a.txt", bytes.NewReader([]byte("hello")), nil)
	require.NoError(t, err)

	files := server.PackageFiles("g/app")
	require.Len(t, files, 1)
	assert.Equal(t, "hello", string(files[0].Content))
}

func TestGenericPackageDownloadServesTheNewestCopy(t *testing.T) {
	t.Parallel()

	server := newServer(t)
	require.NoError(t, server.AddProject("g/app"))
	require.NoError(t, server.AddPackageFile("g/app", "tool", "1", "tool.bin", []byte("old")))
	require.NoError(t, server.AddPackageFile("g/app", "tool", "1", "tool.bin", []byte("new")))

	client := newClient(t, server)
	project, _ := server.Project("g/app")

	var downloaded bytes.Buffer

	request, err := client.NewRequest(http.MethodGet, "projects/"+strconv.FormatInt(project.ID, 10)+"/packages/generic/tool/1/tool.bin", nil, nil)
	require.NoError(t, err)
	_, err = client.Do(request, &downloaded)
	require.NoError(t, err)
	assert.Equal(t, "new", downloaded.String())
}

func TestPushDeletingATagDeletesItsRelease(t *testing.T) {
	t.Parallel()

	server := newServer(t)
	require.NoError(t, server.AddProject("g/source"))
	require.NoError(t, server.AddProject("g/target"))
	_, err := server.Commit("g/source", "main", "a.txt", "one")
	require.NoError(t, err)
	_, err = server.Commit("g/target", "main", "a.txt", "one")
	require.NoError(t, err)
	require.NoError(t, server.Tag("g/target", "v1", "main"))
	require.NoError(t, server.AddRelease("g/target", "v1", "One", "notes"))

	auth := &githttp.BasicAuth{Username: "git", Password: "secret"}

	clone, err := git.PlainClone(t.TempDir(), true, &git.CloneOptions{URL: server.URL() + "/g/target.git", Mirror: true, Auth: auth})
	require.NoError(t, err)
	require.NoError(t, clone.Storer.RemoveReference(plumbing.NewTagReferenceName("v1")))

	remote := git.NewRemote(clone.Storer, &config.RemoteConfig{Name: "target", URLs: []string{server.URL() + "/g/target.git"}})
	require.NoError(t, remote.Push(&git.PushOptions{RemoteName: "target", Auth: auth, RefSpecs: []config.RefSpec{":refs/tags/v1"}}))

	assert.Empty(t, server.Releases("g/target"), "GitLab deletes the release of a deleted tag")

	refs, err := server.Refs("g/target")
	require.NoError(t, err)
	assert.NotContains(t, refs, "refs/tags/v1")
}
