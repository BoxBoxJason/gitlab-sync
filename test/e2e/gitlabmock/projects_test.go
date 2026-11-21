package gitlabmock_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"

	"github.com/boxboxjason/gitlab-sync/test/e2e/gitlabmock"
)

func TestProjectsArePaginated(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

func TestArchivedProjectsCanBeHidden(t *testing.T) {
	t.Parallel()

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
