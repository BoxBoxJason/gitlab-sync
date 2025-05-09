package mirroring

import (
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestNewGitlabInstance(t *testing.T) {
	gitlabURL := "https://gitlab.example.com"
	gitlabToken := "test-token"

	instance, err := newGitlabInstance(&GitlabInstanceOpts{
		GitlabURL:   gitlabURL,
		GitlabToken: gitlabToken,
		Role:        ROLE_SOURCE,
		Timeout:     10,
		MaxRetries:  3,
		BigInstance: false,
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

	projectPath := "test/project"
	project := &gitlab.Project{Name: "Test Project"}

	instance.addProject(projectPath, project)

	if got, exists := instance.Projects[projectPath]; !exists {
		t.Fatalf("expected project %s to be added", projectPath)
	} else if got != project {
		t.Errorf("expected project %v, got %v", project, got)
	}
}

func TestGetProject(t *testing.T) {
	instance := &GitlabInstance{
		Projects: make(map[string]*gitlab.Project),
	}

	projectPath := "test/project"
	project := &gitlab.Project{Name: "Test Project"}
	instance.Projects[projectPath] = project

	got := instance.getProject(projectPath)
	if got != project {
		t.Errorf("expected project %v, got %v", project, got)
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

	groupPath := "test/group"
	group := &gitlab.Group{Name: "Test Group"}

	instance.addGroup(groupPath, group)

	if got, exists := instance.Groups[groupPath]; !exists {
		t.Fatalf("expected group %s to be added", groupPath)
	} else if got != group {
		t.Errorf("expected group %v, got %v", group, got)
	}
}

func TestGetGroup(t *testing.T) {
	instance := &GitlabInstance{
		Groups: make(map[string]*gitlab.Group),
	}

	groupPath := "test/group"
	group := &gitlab.Group{Name: "Test Group"}
	instance.Groups[groupPath] = group

	got := instance.getGroup(groupPath)
	if got != group {
		t.Errorf("expected group %v, got %v", group, got)
	}

	nonExistentPath := "non/existent"
	if got := instance.getGroup(nonExistentPath); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}
