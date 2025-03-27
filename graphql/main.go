package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type GraphQLClient struct {
	token string
	URL   string
}

type GraphQLRequest struct {
	Query     string `json:"query"`
	Variables string `json:"variables,omitempty"`
}

func NewGitlabGraphQLClient(token, gitlabUrl string) *GraphQLClient {
	return &GraphQLClient{
		token: token,
		URL:   strings.TrimSuffix(gitlabUrl, "/") + "/api/graphql",
	}
}

func (g *GraphQLClient) SendRequest(request *GraphQLRequest, method string) (string, error) {
	requestBody, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(context.Background(), method, g.URL, bytes.NewBuffer(requestBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GraphQL request failed with status: %s", resp.Status)
	}
	var responseBody bytes.Buffer
	if _, err := responseBody.ReadFrom(resp.Body); err != nil {
		return "", err
	}
	return responseBody.String(), nil
}
