package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "ssh scp-style", url: "git@github.com:tobeck/shelfpress.git", want: "tobeck/shelfpress"},
		{name: "ssh without suffix", url: "git@github.com:tobeck/shelfpress", want: "tobeck/shelfpress"},
		{name: "https", url: "https://github.com/tobeck/shelfpress.git", want: "tobeck/shelfpress"},
		{name: "https without suffix", url: "https://github.com/tobeck/shelfpress", want: "tobeck/shelfpress"},
		{name: "ssh url form", url: "ssh://git@github.com/tobeck/shelfpress.git", want: "tobeck/shelfpress"},
		{name: "codeberg", url: "https://codeberg.org/bookstack/bookstack.git", want: "bookstack/bookstack"},
		{name: "nested group", url: "https://gitlab.com/group/sub/project.git", want: "sub/project"},
		{name: "trailing slash", url: "https://github.com/tobeck/shelfpress/", want: "tobeck/shelfpress"},
		{name: "host only", url: "https://github.com/", want: ""},
		{name: "single segment", url: "https://github.com/lonely", want: ""},
		{name: "empty", url: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRemoteURL(tt.url); got != tt.want {
				t.Errorf("parseRemoteURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

// writeGitConfig creates a .git directory containing the given config body.
func writeGitConfig(t *testing.T, dir, body string) {
	t.Helper()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o750); err != nil {
		t.Fatalf("creating .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(body), 0o600); err != nil {
		t.Fatalf("writing .git/config: %v", err)
	}
}

func TestRepoFromGitConfig(t *testing.T) {
	t.Run("reads the origin remote", func(t *testing.T) {
		dir := t.TempDir()
		writeGitConfig(t, dir, `[core]
	bare = false
[remote "origin"]
	url = git@github.com:tobeck/shelfpress.git
	fetch = +refs/heads/*:refs/remotes/origin/*
`)
		if got := repoFromGitConfig(dir); got != "tobeck/shelfpress" {
			t.Errorf("repoFromGitConfig() = %q, want %q", got, "tobeck/shelfpress")
		}
	})

	t.Run("ignores non-origin remotes", func(t *testing.T) {
		dir := t.TempDir()
		writeGitConfig(t, dir, `[remote "upstream"]
	url = git@github.com:someone/else.git
[remote "origin"]
	url = git@github.com:tobeck/shelfpress.git
`)
		if got := repoFromGitConfig(dir); got != "tobeck/shelfpress" {
			t.Errorf("repoFromGitConfig() = %q, want the origin remote", got)
		}
	})

	t.Run("no origin section", func(t *testing.T) {
		dir := t.TempDir()
		writeGitConfig(t, dir, "[core]\n\tbare = false\n")
		if got := repoFromGitConfig(dir); got != "" {
			t.Errorf("repoFromGitConfig() = %q, want empty", got)
		}
	})

	t.Run("no git directory", func(t *testing.T) {
		if got := repoFromGitConfig(t.TempDir()); got != "" {
			t.Errorf("repoFromGitConfig() = %q, want empty", got)
		}
	})

	t.Run("worktree gitdir file", func(t *testing.T) {
		gitDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(gitDir, "config"),
			[]byte("[remote \"origin\"]\n\turl = https://github.com/tobeck/shelfpress.git\n"), 0o600); err != nil {
			t.Fatalf("writing config: %v", err)
		}

		work := t.TempDir()
		if err := os.WriteFile(filepath.Join(work, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o600); err != nil {
			t.Fatalf("writing .git file: %v", err)
		}

		if got := repoFromGitConfig(work); got != "tobeck/shelfpress" {
			t.Errorf("repoFromGitConfig() = %q, want %q", got, "tobeck/shelfpress")
		}
	})
}

func TestInferRepoFallsBackToDirName(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "my-project")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("creating dir: %v", err)
	}

	if got := inferRepo(dir); got != "my-project" {
		t.Errorf("inferRepo() = %q, want %q", got, "my-project")
	}
}

func TestInferRepoPrefersGitRemote(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "local-name")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	writeGitConfig(t, dir, "[remote \"origin\"]\n\turl = git@github.com:tobeck/shelfpress.git\n")

	if got := inferRepo(dir); got != "tobeck/shelfpress" {
		t.Errorf("inferRepo() = %q, want the git remote to win", got)
	}
}

func TestLoadInfersRepo(t *testing.T) {
	dir := t.TempDir()
	writeGitConfig(t, dir, "[remote \"origin\"]\n\turl = git@github.com:tobeck/shelfpress.git\n")

	cfg, err := Load(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Repo != "tobeck/shelfpress" {
		t.Errorf("Repo = %q, want %q", cfg.Repo, "tobeck/shelfpress")
	}
	if cfg.Shelf != "shelfpress" {
		t.Errorf("Shelf = %q, want it derived from the repo", cfg.Shelf)
	}
}

func TestLoadExplicitRepoWins(t *testing.T) {
	dir := t.TempDir()
	writeGitConfig(t, dir, "[remote \"origin\"]\n\turl = git@github.com:tobeck/shelfpress.git\n")
	writeConfig(t, dir, "repo: explicit/name\n")

	cfg, err := Load(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Repo != "explicit/name" {
		t.Errorf("Repo = %q, want the configured value", cfg.Repo)
	}
}
