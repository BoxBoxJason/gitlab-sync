package gitlabmock_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"

	"github.com/boxboxjason/gitlab-sync/test/e2e/gitlabmock"
)

func newServer(t *testing.T) *gitlabmock.Server {
	t.Helper()

	server, err := gitlabmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })

	return server
}

// answer is what a URL served.
type answer struct {
	body   []byte
	status int
}

// get fetches a URL.
func get(t *testing.T, target string) answer {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody)
	require.NoError(t, err)

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	content, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	return answer{status: response.StatusCode, body: content}
}

func newClient(t *testing.T, server *gitlabmock.Server, options ...gitlab.ClientOptionFunc) *gitlab.Client {
	t.Helper()

	options = append([]gitlab.ClientOptionFunc{gitlab.WithBaseURL(server.URL()), gitlab.WithCustomRetryMax(0)}, options...)

	client, err := gitlab.NewClient("secret", options...)
	require.NoError(t, err)

	return client
}

func TestUnknownEndpointAnswers501AndIsRemembered(t *testing.T) {
	t.Parallel()

	server := newServer(t)

	got := get(t, server.URL()+"/api/v4/not/implemented")

	assert.Equal(t, http.StatusNotImplemented, got.status)
	assert.Equal(t, []string{"GET /api/v4/not/implemented"}, server.Unimplemented())
}

func TestMetadataAndLicense(t *testing.T) {
	t.Parallel()

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

func TestFaultsFailTheMatchingRequestsOnly(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	server := newServer(t)
	server.InjectFault("GET", "/api/v4/metadata", http.StatusServiceUnavailable, 2)

	client := newClient(t, server, gitlab.WithCustomRetryMax(2))

	_, _, err := client.Metadata.GetMetadata()
	require.NoError(t, err)
	assert.Len(t, server.Matching("GET", "/api/v4/metadata"), 3)
}

func TestRequiredTokenRejectsOthers(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
