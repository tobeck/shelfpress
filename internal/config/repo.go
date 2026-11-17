package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// inferRepo determines an "owner/name" identifier for the repository at dir,
// preferring the origin remote and falling back to the directory name.
//
// The value becomes the sp-repo tag written into BookStack, so it only needs to
// be stable for a given checkout, not globally unique.
func inferRepo(dir string) string {
	if repo := repoFromGitConfig(dir); repo != "" {
		return repo
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return filepath.Base(abs)
	}
	return filepath.Base(dir)
}

// repoFromGitConfig reads .git/config and returns "owner/name" parsed from the
// origin remote URL, or "" when there is no readable origin.
//
// The file is parsed directly rather than shelling out to git, so the tool has
// no runtime dependency on a git binary.
func repoFromGitConfig(dir string) string {
	path, ok := gitConfigPath(dir)
	if !ok {
		return ""
	}

	f, err := os.Open(path) //nolint:gosec // path derived from the caller's own directory
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	var inOrigin bool
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "[") {
			inOrigin = strings.HasPrefix(line, `[remote "origin"]`)
			continue
		}
		if !inOrigin {
			continue
		}
		if name, value, found := strings.Cut(line, "="); found && strings.TrimSpace(name) == "url" {
			return parseRemoteURL(strings.TrimSpace(value))
		}
	}
	return ""
}

// gitConfigPath locates the git config for dir, handling both a .git directory
// and the .git file a worktree uses.
func gitConfigPath(dir string) (string, bool) {
	gitPath := filepath.Join(dir, ".git")

	info, err := os.Stat(gitPath)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		return filepath.Join(gitPath, "config"), true
	}

	// A worktree's .git is a file containing "gitdir: <path>".
	data, err := os.ReadFile(gitPath) //nolint:gosec // path derived from the caller's own directory
	if err != nil {
		return "", false
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return "", false
	}
	return filepath.Join(strings.TrimSpace(gitDir), "config"), true
}

// parseRemoteURL extracts "owner/name" from the SSH and HTTPS remote forms,
// returning "" when the URL does not carry both segments.
func parseRemoteURL(raw string) string {
	s := strings.TrimSuffix(raw, ".git")

	// scp-style: git@host:owner/name
	if _, after, found := strings.Cut(s, "@"); found && !strings.Contains(s, "://") {
		if _, path, ok := strings.Cut(after, ":"); ok {
			return lastTwoSegments(path)
		}
	}

	// URL form: scheme://[user@]host/owner/name
	if _, after, found := strings.Cut(s, "://"); found {
		if _, path, ok := strings.Cut(after, "/"); ok {
			return lastTwoSegments(path)
		}
		return ""
	}

	return lastTwoSegments(s)
}

// lastTwoSegments returns the final "owner/name" pair of a slash-separated
// path, or "" when fewer than two non-empty segments are present.
func lastTwoSegments(path string) string {
	parts := make([]string, 0, 2)
	for _, p := range strings.Split(strings.Trim(path, "/"), "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) < 2 {
		return ""
	}
	return strings.Join(parts[len(parts)-2:], "/")
}
