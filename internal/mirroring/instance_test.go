package mirroring

import (
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestNewGitlabInstance(t *testing.T) {
	gitlabURL := "https://gitlab.example.com"
	gitlabToken := "test-token"

	instance, err := newGitlabInstance(&GitlabInstanceOpts{
		GitlabURL:    gitlabURL,
		GitlabToken:  gitlabToken,
		Role:         ROLE_SOURCE,
		MaxRetries:   3,
		InstanceSize: INSTANCE_SIZE_SMALL,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if instance.Gitlab == nil {
		t.Error("expected Gitlab client to be initialized")
	}

	if instance.Projects == nil {
		t.Error("expected Projects map to be initialized")
	}

	if instance.Groups == nil {
		t.Error("expected Groups map to be initialized")
	}

	if instance.GraphQLClient == nil {
		t.Error("expected GraphQLClient to be initialized")
	}
}

func TestAddProject(t *testing.T) {
	instance := &GitlabInstance{
		Projects: make(map[string]*gitlab.Project),
	}

	instance.addProject(TEST_PROJECT)

	if got, exists := instance.Projects[TEST_PROJECT.PathWithNamespace]; !exists {
		t.Fatalf("expected project %s to be added", TEST_PROJECT.PathWithNamespace)
	} else if got != TEST_PROJECT {
		t.Errorf("expected project %v, got %v", TEST_PROJECT, got)
	}
}

func TestGetProject(t *testing.T) {
	instance := &GitlabInstance{
		Projects: make(map[string]*gitlab.Project),
	}

	instance.Projects[TEST_PROJECT.PathWithNamespace] = TEST_PROJECT

	got := instance.getProject(TEST_PROJECT.PathWithNamespace)
	if got != TEST_PROJECT {
		t.Errorf("expected project %v, got %v", TEST_PROJECT, got)
	}

	nonExistentPath := "non/existent"
	if got := instance.getProject(nonExistentPath); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestAddGroup(t *testing.T) {
	instance := &GitlabInstance{
		Groups: make(map[string]*gitlab.Group),
	}

	instance.addGroup(TEST_GROUP)

	if got, exists := instance.Groups[TEST_GROUP.FullPath]; !exists {
		t.Fatalf("expected group %s to be added", TEST_GROUP.FullPath)
	} else if got != TEST_GROUP {
		t.Errorf("expected group %v, got %v", TEST_GROUP, got)
	}
}

func TestGetGroup(t *testing.T) {
	instance := &GitlabInstance{
		Groups: make(map[string]*gitlab.Group),
	}

	instance.Groups[TEST_GROUP.FullPath] = TEST_GROUP

	got := instance.getGroup(TEST_GROUP.FullPath)
	if got != TEST_GROUP {
		t.Errorf("expected group %v, got %v", TEST_GROUP, got)
	}

	nonExistentPath := "non/existent"
	if got := instance.getGroup(nonExistentPath); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}
