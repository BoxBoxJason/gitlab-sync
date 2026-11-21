package helpers

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// repoPath returns the deterministic location of the bare mirror backing
// sourceURL: <root>/<host>/<namespace...>/<repository>.git
//
// Every segment is sanitised, and the result is checked against the cache root,
// so a repository URL can never place an entry outside of the cache.
func (c *GitCache) repoPath(sourceURL string) (string, error) {
	segments := repoURLSegments(sourceURL)
	if len(segments) == 0 {
		return "", fmt.Errorf("failed to derive a cache path from the repository URL %q", sourceURL)
	}

	last := len(segments) - 1
	segments[last] = strings.TrimSuffix(segments[last], cacheRepoSuffix) + cacheRepoSuffix

	repoPath := filepath.Join(append([]string{c.root}, segments...)...)
	if !strings.HasPrefix(repoPath, c.root+string(os.PathSeparator)) {
		return "", fmt.Errorf("refusing to use the cache path %q, which escapes the cache root %q", repoPath, c.root)
	}

	return repoPath, nil
}

// repoURLSegments splits a repository URL into the sanitised path segments its
// cache entry is nested under. The host comes first, so two instances serving
// the same namespace never collide on a single entry.
//
// It returns nil when the URL carries nothing that identifies a repository.
func repoURLSegments(sourceURL string) []string {
	location := splitRepoURL(strings.TrimSpace(sourceURL))
	host, repoPath := location.host, location.path

	segments := make([]string, 0, strings.Count(repoPath, "/")+2) //nolint:mnd // host segment + path segments
	segments = appendSanitizedSegment(segments, host, unknownHostDir)

	for segment := range strings.SplitSeq(repoPath, "/") {
		segments = appendSanitizedSegment(segments, segment, "")
	}

	// The first segment is the host: on its own it does not name a repository.
	if len(segments) < 2 { //nolint:mnd // a host and at least one path segment
		return nil
	}

	return segments
}

// repoLocation is the host and the repository path of a repository URL.
type repoLocation struct {
	host string
	path string
}

// splitRepoURL extracts the host and the repository path from the URL forms a
// GitLab instance hands out: https://host/group/repo.git, ssh://git@host/group/repo.git,
// the scp-like git@host:group/repo.git, and the file:// URLs used by the tests.
//
// Both values are returned empty rather than guessed when the URL cannot be
// understood; the caller decides what to do with that.
func splitRepoURL(rawURL string) repoLocation {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Scheme != "" {
		// The escaped form is kept on purpose: decoding it would turn a %2F
		// inside a segment into a directory separator, letting two different
		// repositories share one cache entry.
		return repoLocation{host: parsed.Hostname(), path: strings.Trim(parsed.EscapedPath(), "/")}
	}

	// scp-like syntax carries no scheme: [user@]host:path
	host, repoPath, found := strings.Cut(rawURL, ":")
	if found && !strings.Contains(host, "/") {
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}

		return repoLocation{host: host, path: strings.Trim(repoPath, "/")}
	}

	return repoLocation{path: strings.Trim(rawURL, "/")}
}

// appendSanitizedSegment appends segment to segments once it has been reduced to
// characters that are safe in a path. An empty result is replaced by fallback,
// or dropped when fallback is empty.
func appendSanitizedSegment(segments []string, segment, fallback string) []string {
	sanitized := sanitizePathSegment(segment)
	if sanitized == "" {
		sanitized = fallback
	}

	if sanitized == "" {
		return segments
	}

	return append(segments, sanitized)
}

// sanitizePathSegment reduces a single URL segment to a safe directory name:
// anything outside [A-Za-z0-9._-] becomes an underscore, and the "." and ".."
// traversal segments are rejected outright.
func sanitizePathSegment(segment string) string {
	sanitized := strings.Map(func(char rune) rune {
		switch {
		case char >= 'a' && char <= 'z',
			char >= 'A' && char <= 'Z',
			char >= '0' && char <= '9',
			char == '.', char == '_', char == '-':
			return char
		default:
			return '_'
		}
	}, segment)

	if sanitized == "." || sanitized == ".." {
		return ""
	}

	return sanitized
}
