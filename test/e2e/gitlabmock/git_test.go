package gitlabmock_test

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloneAndPushOverSmartHTTP(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	server := newServer(t)

	_, err := git.PlainClone(t.TempDir(), true, &git.CloneOptions{URL: server.URL() + "/g/missing.git"})
	require.Error(t, err)
}

func TestForbidGit(t *testing.T) {
	t.Parallel()

	server := newServer(t)
	require.NoError(t, server.AddProject("g/app"))
	_, err := server.Commit("g/app", "main", "a.txt", "one")
	require.NoError(t, err)

	require.NoError(t, server.ForbidGit("clone", "g/app"))

	_, err = git.PlainClone(t.TempDir(), true, &git.CloneOptions{URL: server.URL() + "/g/app.git"})
	require.Error(t, err)
	require.ErrorIs(t, err, transport.ErrAuthorizationFailed, "go-git reports a 403 that way")

	require.Error(t, server.ForbidGit("fetch", ""))
}

func TestAvatarsAreDownloadedAndUploaded(t *testing.T) {
	t.Parallel()

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

func TestAMissingUploadIsNotFoundNotUnimplemented(t *testing.T) {
	t.Parallel()

	server := newServer(t)

	got := get(t, server.URL()+"/uploads/-/system/gone.txt")

	assert.Equal(t, http.StatusNotFound, got.status)
	assert.Empty(t, server.Unimplemented())
}
