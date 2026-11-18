package gitlabmock_test

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"

	"github.com/boxboxjason/gitlab-sync/test/e2e/gitlabmock"
)

func newServer(t *testing.T) *gitlabmock.Server {
	t.Helper()

	server, err := gitlabmock.New()
	require.NoError(t, err)
	t.Cleanup(server.Close)

	return server
}

func newClient(t *testing.T, server *gitlabmock.Server, options ...gitlab.ClientOptionFunc) *gitlab.Client {
	t.Helper()

	options = append([]gitlab.ClientOptionFunc{gitlab.WithBaseURL(server.URL()), gitlab.WithCustomRetryMax(0)}, options...)

	client, err := gitlab.NewClient("secret", options...)
	require.NoError(t, err)

	return client
}

func TestUnknownEndpointAnswers501AndIsRemembered(t *testing.T) {
	server := newServer(t)

	response, err := http.Get(server.URL() + "/api/v4/not/implemented") //nolint:noctx // test
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	assert.Equal(t, http.StatusNotImplemented, response.StatusCode)
	assert.Equal(t, []string{"GET /api/v4/not/implemented"}, server.Unimplemented())
}

func TestMetadataAndLicense(t *testing.T) {
	server := newServer(t)
	client := newClient(t, server)

	metadata, _, err := client.Metadata.GetMetadata()
	require.NoError(t, err)
	assert.Equal(t, gitlabmock.DefaultVersion, metadata.Version)

	license, _, err := client.License.GetLicense()
	require.NoError(t, err)
	assert.Nil(t, license, "an unlicensed instance answers null")

	server.SetLicenseEndpointMissing(true)

	_, response, err := client.License.GetLicense()
	require.Error(t, err, "a Community Edition has no license endpoint")
	assert.Equal(t, http.StatusNotFound, response.StatusCode)

	server.SetLicenseEndpointMissing(false)

	server.SetLicensePlan("premium")
	server.SetVersion("18.1.0")

	license, _, err = client.License.GetLicense()
	require.NoError(t, err)
	assert.Equal(t, "premium", license.Plan)

	metadata, _, err = client.Metadata.GetMetadata()
	require.NoError(t, err)
	assert.Equal(t, "18.1.0", metadata.Version)
}

func TestProjectsArePaginated(t *testing.T) {
	server := newServer(t)
	require.NoError(t, server.AddGeneratedProjects("big", 250))

	client := newClient(t, server)

	var seen int

	options := &gitlab.ListProjectsOptions{ListOptions: gitlab.ListOptions{PerPage: 100, Page: 1}}

	for {
		projects, response, err := client.Projects.ListProjects(options)
		require.NoError(t, err)

		seen += len(projects)

		if response.CurrentPage >= response.TotalPages {
			assert.EqualValues(t, 3, response.TotalPages)

			break
		}

		options.Page = response.NextPage
	}

	assert.Equal(t, 250, seen)
}

func TestProjectLookupByPathAndID(t *testing.T) {
	server := newServer(t)
	require.NoError(t, server.AddProject("team/sub/app", gitlabmock.WithDescription("an app")))

	client := newClient(t, server)

	byPath, _, err := client.Projects.GetProject("team/sub/app", nil)
	require.NoError(t, err)
	assert.Equal(t, "an app", byPath.Description)
	assert.Equal(t, server.URL()+"/team/sub/app.git", byPath.HTTPURLToRepo)

	byID, _, err := client.Projects.GetProject(byPath.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, byPath.PathWithNamespace, byID.PathWithNamespace)

	_, response, err := client.Projects.GetProject("team/missing", nil)
	require.Error(t, err)
	assert.Equal(t, http.StatusNotFound, response.StatusCode)

	assert.Equal(t, []string{"team", "team/sub"}, server.GroupPaths())
}

func TestCreateGroupThenProjectInIt(t *testing.T) {
	server := newServer(t)
	client := newClient(t, server)

	top, _, err := client.Groups.CreateGroup(&gitlab.CreateGroupOptions{Name: new("Top"), Path: new("top")})
	require.NoError(t, err)

	sub, _, err := client.Groups.CreateGroup(&gitlab.CreateGroupOptions{Name: new("Sub"), Path: new("sub"), ParentID: &top.ID})
	require.NoError(t, err)
	assert.Equal(t, "top/sub", sub.FullPath)

	created, _, err := client.Projects.CreateProject(&gitlab.CreateProjectOptions{Name: new("App"), Path: new("app"), NamespaceID: &sub.ID, Visibility: new(gitlab.InternalVisibility)})
	require.NoError(t, err)
	assert.Equal(t, "top/sub/app", created.PathWithNamespace)
	assert.Equal(t, gitlab.InternalVisibility, created.Visibility)

	subgroups, _, err := client.Groups.ListSubGroups(top.ID, nil)
	require.NoError(t, err)
	require.Len(t, subgroups, 1)

	projects, _, err := client.Groups.ListGroupProjects(sub.ID, nil)
	require.NoError(t, err)
	require.Len(t, projects, 1)
}

func TestDefaultBranchCannotPointToAMissingBranch(t *testing.T) {
	server := newServer(t)
	require.NoError(t, server.AddProject("g/app"))

	client := newClient(t, server)
	project, _ := server.Project("g/app")

	_, _, err := client.Projects.EditProject(project.ID, &gitlab.EditProjectOptions{DefaultBranch: new("develop")})
	require.Error(t, err, "GitLab refuses a default branch the repository does not have")

	_, err = server.Commit("g/app", "develop", "a.txt", "a")
	require.NoError(t, err)

	edited, _, err := client.Projects.EditProject(project.ID, &gitlab.EditProjectOptions{DefaultBranch: new("develop")})
	require.NoError(t, err)
	assert.Equal(t, "develop", edited.DefaultBranch)
}

func TestPullMirrorNeedsAnImportURL(t *testing.T) {
	server := newServer(t)
	require.NoError(t, server.AddProject("g/app"))

	client := newClient(t, server)
	project, _ := server.Project("g/app")

	_, _, err := client.Projects.EditProject(project.ID, &gitlab.EditProjectOptions{Mirror: new(true)})
	require.Error(t, err)

	_, _, err = client.Projects.ConfigureProjectPullMirror(project.ID, &gitlab.ConfigureProjectPullMirrorOptions{URL: new("https://src/g/app.git"), Enabled: new(true)})
	require.NoError(t, err)

	mirrored, _ := server.Project("g/app")
	assert.True(t, mirrored.Mirror)
	assert.Equal(t, "https://src/g/app.git", mirrored.ImportURL)
}

func TestMembersEditNeedsAnExistingMember(t *testing.T) {
	server := newServer(t)
	require.NoError(t, server.AddProject("g/app"))

	client := newClient(t, server)
	project, _ := server.Project("g/app")

	_, response, err := client.ProjectMembers.EditProjectMember(project.ID, 1, &gitlab.EditProjectMemberOptions{AccessLevel: new(gitlab.OwnerPermissions)})
	require.Error(t, err)
	assert.Equal(t, http.StatusNotFound, response.StatusCode)

	_, _, err = client.ProjectMembers.AddProjectMember(project.ID, &gitlab.AddProjectMemberOptions{UserID: 1, AccessLevel: new(gitlab.DeveloperPermissions)})
	require.NoError(t, err)

	_, response, err = client.ProjectMembers.AddProjectMember(project.ID, &gitlab.AddProjectMemberOptions{UserID: 1, AccessLevel: new(gitlab.DeveloperPermissions)})
	require.Error(t, err)
	assert.Equal(t, http.StatusConflict, response.StatusCode)

	_, _, err = client.ProjectMembers.EditProjectMember(project.ID, 1, &gitlab.EditProjectMemberOptions{AccessLevel: new(gitlab.OwnerPermissions)})
	require.NoError(t, err)

	level, ok := server.ProjectMember("g/app")
	assert.True(t, ok)
	assert.EqualValues(t, gitlab.OwnerPermissions, level)
}

func TestIssuesCanBeCreatedClosedAndListedByState(t *testing.T) {
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
	response, err := http.Get(link.DirectAssetURL) //nolint:noctx // test
	require.NoError(t, err)

	content, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, "payload", string(content))

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

func TestFaultsFailTheMatchingRequestsOnly(t *testing.T) {
	server := newServer(t)
	require.NoError(t, server.AddProject("g/app"))
	server.InjectFault("GET", "/api/v4/projects/g/*", http.StatusForbidden, 2)

	client := newClient(t, server)

	for range 2 {
		_, response, err := client.Projects.GetProject("g/app", nil)
		require.Error(t, err)
		assert.Equal(t, http.StatusForbidden, response.StatusCode)
	}

	_, _, err := client.Projects.GetProject("g/app", nil)
	require.NoError(t, err, "the fault only fails two requests")

	_, _, err = client.Metadata.GetMetadata()
	require.NoError(t, err, "a request that does not match the pattern is not affected")
}

func TestServiceUnavailableIsRetriedByTheClient(t *testing.T) {
	server := newServer(t)
	server.InjectFault("GET", "/api/v4/metadata", http.StatusServiceUnavailable, 2)

	client := newClient(t, server, gitlab.WithCustomRetryMax(2))

	_, _, err := client.Metadata.GetMetadata()
	require.NoError(t, err)
	assert.Len(t, server.Matching("GET", "/api/v4/metadata"), 3)
}

func TestRequiredTokenRejectsOthers(t *testing.T) {
	server := newServer(t)
	server.RequireToken("good")

	client := newClient(t, server)

	_, response, err := client.Users.CurrentUser()
	require.Error(t, err)
	assert.Equal(t, http.StatusUnauthorized, response.StatusCode)

	good, err := gitlab.NewClient("good", gitlab.WithBaseURL(server.URL()))
	require.NoError(t, err)

	user, _, err := good.Users.CurrentUser()
	require.NoError(t, err)
	assert.Equal(t, gitlabmock.DefaultUser, user.Username)
}

func TestJournalRecordsRequestsAndWrites(t *testing.T) {
	server := newServer(t)
	require.NoError(t, server.AddProject("g/app"))

	client := newClient(t, server, gitlab.WithUserAgent("e2e-agent/1"))

	_, _, err := client.Projects.GetProject("g/app", nil)
	require.NoError(t, err)

	assert.Empty(t, server.WriteRequests(), "a read is not a write")

	_, _, err = client.Issues.CreateIssue("g/app", &gitlab.CreateIssueOptions{Title: new("t")})
	require.NoError(t, err)

	writes := server.WriteRequests()
	require.Len(t, writes, 1)
	assert.Equal(t, http.MethodPost, writes[0].Method)
	assert.Contains(t, writes[0].Body, `"title":"t"`)

	for _, request := range server.Requests() {
		assert.Equal(t, "e2e-agent/1", request.Header.Get("User-Agent"))
	}

	server.ResetJournal()
	assert.Empty(t, server.Requests())
}

func TestCloneAndPushOverSmartHTTP(t *testing.T) {
	server := newServer(t)
	require.NoError(t, server.AddProject("g/source"))
	require.NoError(t, server.AddProject("g/target"))

	first, err := server.Commit("g/source", "main", "a.txt", "one")
	require.NoError(t, err)
	_, err = server.Commit("g/source", "main", "a.txt", "two")
	require.NoError(t, err)
	require.NoError(t, server.Tag("g/source", "v1", "main"))

	auth := &githttp.BasicAuth{Username: "git", Password: "secret"}

	clone, err := git.PlainClone(t.TempDir(), true, &git.CloneOptions{URL: server.URL() + "/g/source.git", Mirror: true, Auth: auth})
	require.NoError(t, err)

	_, err = clone.Reference(plumbing.NewTagReferenceName("v1"), true)
	require.NoError(t, err)

	remote := git.NewRemote(clone.Storer, &config.RemoteConfig{Name: "target", URLs: []string{server.URL() + "/g/target.git"}})
	err = remote.Push(&git.PushOptions{
		RemoteName: "target", Auth: auth, Force: true,
		RefSpecs: []config.RefSpec{"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"},
		Options:  map[string]string{"ci.skip": ""},
	})
	require.NoError(t, err)

	source, err := server.Refs("g/source")
	require.NoError(t, err)
	target, err := server.Refs("g/target")
	require.NoError(t, err)

	assert.Equal(t, source, target)
	assert.NotEqual(t, first, target["refs/heads/main"], "the tip is the second commit")
	require.Len(t, server.PushOptions(), 1)
	assert.Equal(t, "/g/target.git/git-receive-pack", server.PushOptions()[0].Path)
	assert.Equal(t, "ci.skip", server.PushOptions()[0].Option)

	pushed, _ := server.Project("g/target")
	assert.False(t, pushed.EmptyRepo)
}

func TestCloneOfAMissingProjectFails(t *testing.T) {
	server := newServer(t)

	_, err := git.PlainClone(t.TempDir(), true, &git.CloneOptions{URL: server.URL() + "/g/missing.git"})
	require.Error(t, err)
}

func TestForbidGit(t *testing.T) {
	server := newServer(t)
	require.NoError(t, server.AddProject("g/app"))
	_, err := server.Commit("g/app", "main", "a.txt", "one")
	require.NoError(t, err)

	require.NoError(t, server.ForbidGit("clone", "g/app"))

	_, err = git.PlainClone(t.TempDir(), true, &git.CloneOptions{URL: server.URL() + "/g/app.git"})
	require.Error(t, err)
	assert.ErrorIs(t, err, transport.ErrAuthorizationFailed, "go-git reports a 403 that way")

	require.Error(t, server.ForbidGit("fetch", ""))
}

func TestAvatarsAreDownloadedAndUploaded(t *testing.T) {
	server := newServer(t)
	require.NoError(t, server.AddProject("g/app"))
	require.NoError(t, server.AddProject("g/other"))
	require.NoError(t, server.SetProjectAvatar("g/app", []byte("png")))
	require.NoError(t, server.SetGroupAvatar("g", []byte("group-png")))

	client := newClient(t, server)
	app, _ := server.Project("g/app")
	other, _ := server.Project("g/other")
	team, _ := server.Group("g")

	downloaded, _, err := client.Projects.DownloadAvatar(app.ID)
	require.NoError(t, err)

	content, err := io.ReadAll(downloaded)
	require.NoError(t, err)
	assert.Equal(t, "png", string(content))

	_, response, err := client.Projects.DownloadAvatar(other.ID)
	require.Error(t, err, "a project without avatar has nothing to download")
	assert.Equal(t, http.StatusNotFound, response.StatusCode)

	_, _, err = client.Projects.UploadAvatar(other.ID, bytes.NewReader([]byte("copied")), "avatar.png")
	require.NoError(t, err)
	assert.Equal(t, "copied", string(server.ProjectAvatar("g/other")))

	_, _, err = client.Groups.UploadAvatar(team.ID, bytes.NewReader([]byte("new-group-png")), "avatar.png")
	require.NoError(t, err)
	assert.Equal(t, "new-group-png", string(server.GroupAvatar("g")))

	updated, _ := server.Project("g/other")
	assert.NotEmpty(t, updated.AvatarURL)
}

func TestGenericPackageDownloadServesTheNewestCopy(t *testing.T) {
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

func TestAMissingUploadIsNotFoundNotUnimplemented(t *testing.T) {
	server := newServer(t)

	response, err := http.Get(server.URL() + "/uploads/-/system/gone.txt") //nolint:noctx // test
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	assert.Equal(t, http.StatusNotFound, response.StatusCode)
	assert.Empty(t, server.Unimplemented())
}

func TestPushDeletingATagDeletesItsRelease(t *testing.T) {
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

func TestArchivedProjectsCanBeHidden(t *testing.T) {
	server := newServer(t)
	require.NoError(t, server.AddProject("g/live"))
	require.NoError(t, server.AddProject("g/old", gitlabmock.WithArchived()))

	client := newClient(t, server)

	visible, _, err := client.Projects.ListProjects(&gitlab.ListProjectsOptions{Archived: new(false)})
	require.NoError(t, err)
	require.Len(t, visible, 1)
	assert.Equal(t, "g/live", visible[0].PathWithNamespace)

	all, _, err := client.Projects.ListProjects(&gitlab.ListProjectsOptions{})
	require.NoError(t, err)
	assert.Len(t, all, 2)
}
