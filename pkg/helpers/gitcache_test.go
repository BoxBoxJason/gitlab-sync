package helpers

import (
	"path/filepath"
	"testing"
)

const (
	reuseMarker = "reused-entry-marker"
)

func TestSplitRepoURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rawURL   string
		wantHost string
		wantPath string
	}{
		{"https", "https://gitlab.example.com/group/sub/repo.git", "gitlab.example.com", "group/sub/repo.git"},
		{"https with port", "https://gitlab.example.com:8443/group/repo.git", "gitlab.example.com", "group/repo.git"},
		{"ssh scheme", "ssh://git@gitlab.example.com:22/group/repo.git", "gitlab.example.com", "group/repo.git"},
		{"scp like", "git@gitlab.example.com:group/repo.git", "gitlab.example.com", "group/repo.git"},
		{"file", "file:///tmp/repos/repo.git", "", "tmp/repos/repo.git"},
		{"bare path", "/tmp/repos/repo.git", "", "tmp/repos/repo.git"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			location := splitRepoURL(tt.rawURL)
			host, path := location.host, location.path
			if host != tt.wantHost || path != tt.wantPath {
				t.Errorf("splitRepoURL(%q) = (%q, %q); want (%q, %q)", tt.rawURL, host, path, tt.wantHost, tt.wantPath)
			}
		})
	}
}

func TestGitCacheRepoPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	cache, err := NewGitCache(root, 0)
	if err != nil {
		t.Fatalf("NewGitCache failed: %v", err)
	}

	tests := []struct {
		name      string
		sourceURL string
		want      string
		wantErr   bool
	}{
		{
			name:      "https url keeps its namespace",
			sourceURL: "https://gitlab.example.com/group/sub/repo.git",
			want:      filepath.Join(root, "gitlab.example.com", "group", "sub", "repo.git"),
		},
		{
			name:      "the .git suffix is added when the url omits it",
			sourceURL: "https://gitlab.example.com/group/repo",
			want:      filepath.Join(root, "gitlab.example.com", "group", "repo.git"),
		},
		{
			name:      "a hostless url lands under the local directory",
			sourceURL: "file:///tmp/repo.git",
			want:      filepath.Join(root, unknownHostDir, "tmp", "repo.git"),
		},
		{
			name:      "traversal segments cannot escape the root",
			sourceURL: "https://gitlab.example.com/../../etc/repo.git",
			want:      filepath.Join(root, "gitlab.example.com", "etc", "repo.git"),
		},
		{
			name:      "separators inside a segment are neutralised",
			sourceURL: "https://gitlab.example.com/gr%2Foup/repo.git",
			want:      filepath.Join(root, "gitlab.example.com", "gr_2Foup", "repo.git"),
		},
		{
			name:      "a url without a repository is rejected",
			sourceURL: "https://gitlab.example.com",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, pathErr := cache.repoPath(tt.sourceURL)
			if tt.wantErr {
				if pathErr == nil {
					t.Fatalf("repoPath(%q) = %q; want an error", tt.sourceURL, got)
				}

				return
			}

			if pathErr != nil {
				t.Fatalf("repoPath(%q) failed: %v", tt.sourceURL, pathErr)
			}

			if got != tt.want {
				t.Errorf("repoPath(%q) = %q; want %q", tt.sourceURL, got, tt.want)
			}
		})
	}
}

func TestGitCacheRepoPathIsDeterministic(t *testing.T) {
	t.Parallel()

	cache, err := NewGitCache(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("NewGitCache failed: %v", err)
	}

	const sourceURL = "https://gitlab.example.com/group/repo.git"

	first, err := cache.repoPath(sourceURL)
	if err != nil {
		t.Fatalf("repoPath failed: %v", err)
	}

	second, err := cache.repoPath(sourceURL)
	if err != nil {
		t.Fatalf("repoPath failed: %v", err)
	}

	if first != second {
		t.Errorf("repoPath is not deterministic: %q != %q", first, second)
	}
}
