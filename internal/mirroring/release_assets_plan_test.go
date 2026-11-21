package mirroring

import (
	"net/url"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", rawURL, err)
	}
	return u
}

func TestInstanceRootURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{name: "root install", baseURL: "https://gitlab.example.com", want: "https://gitlab.example.com/"},
		{name: "relative URL install", baseURL: "https://example.com/gitlab/", want: "https://example.com/gitlab/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, err := gitlab.NewClient("", gitlab.WithBaseURL(tt.baseURL))
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}
			if got := instanceRootURL(client).String(); got != tt.want {
				t.Errorf("instanceRootURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInstanceRelativePath(t *testing.T) {
	t.Parallel()

	root := mustParseURL(t, "https://example.com/gitlab/")

	tests := []struct {
		name       string
		link       string
		want       string
		onInstance bool
	}{
		{name: "same instance", link: "https://example.com/gitlab/group/project/-/jobs/1/artifacts/raw/a.txt", want: "group/project/-/jobs/1/artifacts/raw/a.txt", onInstance: true},
		{name: "host is case insensitive", link: "https://EXAMPLE.com/gitlab/api/v4/projects/1", want: "api/v4/projects/1", onInstance: true},
		{name: "other scheme same host", link: "http://example.com/gitlab/a", want: "a", onInstance: true},
		{name: "outside of the relative root", link: "https://example.com/other/a", onInstance: false},
		{name: "other host", link: "https://github.com/gitlab/a", onInstance: false},
		{name: "other port", link: "https://example.com:8443/gitlab/a", onInstance: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, onInstance := instanceRelativePath(root, mustParseURL(t, tt.link))
			if onInstance != tt.onInstance || got != tt.want {
				t.Errorf("instanceRelativePath() = (%q, %v), want (%q, %v)", got, onInstance, tt.want, tt.onInstance)
			}
		})
	}
}

func TestParseGenericPackagePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want genericPackageFile
		ok   bool
	}{
		{
			name: "numeric project ID",
			path: "api/v4/projects/12/packages/generic/app/1.0.0/app.tar.gz",
			want: genericPackageFile{ProjectID: "12", Name: "app", Version: "1.0.0", FileName: "app.tar.gz"},
			ok:   true,
		},
		{
			name: "escaped project path and file name",
			path: "api/v4/projects/group%2Fproject/packages/generic/app/1.0.0/my%2Bfile.bin",
			want: genericPackageFile{ProjectID: "group/project", Name: "app", Version: "1.0.0", FileName: "my+file.bin"},
			ok:   true,
		},
		{
			name: "nested file name",
			path: "api/v4/projects/12/packages/generic/app/1.0.0/linux/amd64/app",
			want: genericPackageFile{ProjectID: "12", Name: "app", Version: "1.0.0", FileName: "linux/amd64/app"},
			ok:   true,
		},
		{name: "missing file name", path: "api/v4/projects/12/packages/generic/app/1.0.0", ok: false},
		{name: "empty file name", path: "api/v4/projects/12/packages/generic/app/1.0.0/", ok: false},
		{name: "other package type", path: "api/v4/projects/12/packages/maven/app/1.0.0/app.jar", ok: false},
		{name: "job artifact", path: "group/project/-/jobs/1/artifacts/raw/dist/app", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseGenericPackagePath(tt.path)
			if ok != tt.ok || got != tt.want {
				t.Errorf("parseGenericPackagePath() = (%+v, %v), want (%+v, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestDirectAssetPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		directAssetURL string
		want           string
	}{
		{name: "direct asset path", directAssetURL: "https://example.com/group/project/-/releases/v1.0.0/downloads/bin/app", want: "/bin/app"},
		{name: "no direct asset path", directAssetURL: "https://example.com/group/project/-/jobs/1/artifacts/raw/app", want: ""},
		{name: "empty", directAssetURL: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := directAssetPath(&gitlab.ReleaseLink{DirectAssetURL: tt.directAssetURL}); got != tt.want {
				t.Errorf("directAssetPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReleaseAssetVersion(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"v1.0.0":       "v1.0.0",
		"release/1.0":  "release-1.0",
		"v1..0 beta":   "v1.0-beta",
		".hidden.":     "hidden",
		"":             defaultReleaseAssetVersion,
		"v1.0.0+build": "v1.0.0+build",
	}

	for tag, want := range tests {
		if got := releaseAssetVersion(tag); got != want {
			t.Errorf("releaseAssetVersion(%q) = %q, want %q", tag, got, want)
		}
	}
}

func TestPlanReleaseAssets(t *testing.T) {
	t.Parallel()

	root := mustParseURL(t, "https://gitlab.example.com/")
	release := &gitlab.Release{
		TagName: "release/1.0",
		Assets: gitlab.ReleaseAssets{
			Links: []*gitlab.ReleaseLink{
				// Listed out of order on purpose: the lowest ID keeps the plain file name.
				{ID: 5, Name: "Second report", URL: "https://gitlab.example.com/g/p/-/jobs/2/artifacts/raw/report.txt"},
				{ID: 4, Name: "First report", URL: "https://gitlab.example.com/g/p/-/jobs/1/artifacts/raw/report.txt"},
				{ID: 3, Name: "Package", URL: "https://gitlab.example.com/api/v4/projects/1/packages/generic/app/1.0/app.bin", LinkType: gitlab.PackageLinkType},
				{ID: 2, Name: "Docs", URL: "https://docs.example.com/app"},
				{
					ID:             1,
					Name:           "Download",
					URL:            "https://gitlab.example.com/g/p/-/jobs/3/artifacts/download",
					DirectAssetURL: "https://gitlab.example.com/g/p/-/releases/release%2F1.0/downloads/bin/app-linux",
				},
				nil,
			},
		},
	}

	plans := planReleaseAssets(root, release)
	if len(plans) != 5 {
		t.Fatalf("expected 5 plans, got %d", len(plans))
	}

	byName := make(map[string]*releaseAssetPlan, len(plans))
	for _, plan := range plans {
		byName[plan.Link.Name] = plan
	}

	if plan := byName["Docs"]; plan.Kind != releaseAssetExternal {
		t.Errorf("Docs: expected an external link, got kind %d", plan.Kind)
	}

	if plan := byName["Package"]; plan.Kind != releaseAssetGenericPackage || plan.Package != (genericPackageFile{ProjectID: "1", Name: "app", Version: "1.0", FileName: "app.bin"}) {
		t.Errorf("Package: unexpected plan %+v", plan)
	}

	expectedFiles := map[string]string{
		"Download":      "app-linux",
		"First report":  "report.txt",
		"Second report": "5-report.txt",
	}
	for name, fileName := range expectedFiles {
		plan := byName[name]
		want := genericPackageFile{Name: releaseAssetsPackageName, Version: "release-1.0", FileName: fileName}
		if plan.Kind != releaseAssetInstanceFile || plan.Package != want {
			t.Errorf("%s: got kind %d package %+v, want instance file %+v", name, plan.Kind, plan.Package, want)
		}
	}

	if got := byName["Download"].DirectAssetPath; got != "/bin/app-linux" {
		t.Errorf("Download: direct asset path = %q, want %q", got, "/bin/app-linux")
	}
}
