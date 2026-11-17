package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
)

// testFlags builds the flag set Load consumes, with only the named flags marked
// as set so precedence can be exercised.
func testFlags(t *testing.T, set map[string]string) *pflag.FlagSet {
	t.Helper()

	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("root", "", "")
	fs.String("shelf", "", "")
	fs.String("repo", "", "")
	fs.Bool("prune", true, "")
	fs.Bool("dry-run", false, "")
	fs.String("format", "", "")
	fs.String("output-file", "", "")
	fs.String("url", "", "")
	fs.String("token-id", "", "")
	fs.String("token-secret", "", "")

	for name, value := range set {
		if err := fs.Set(name, value); err != nil {
			t.Fatalf("setting --%s: %v", name, err)
		}
	}
	return fs
}

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, DefaultFileName), []byte(content), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
}

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()

	cfg, err := Load(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Root != DefaultRoot {
		t.Errorf("Root = %q, want %q", cfg.Root, DefaultRoot)
	}
	if !cfg.Prune {
		t.Error("Prune = false, want true by default")
	}
	if cfg.Format != FormatHuman {
		t.Errorf("Format = %q, want %q", cfg.Format, FormatHuman)
	}
	if cfg.DryRun {
		t.Error("DryRun = true, want false by default")
	}
}

func TestLoadPrecedence(t *testing.T) {
	const (
		fileValue = "from-file"
		envValue  = "from-env"
		flagValue = "from-flag"
	)

	tests := []struct {
		name string
		file bool
		env  bool
		flag bool
		want string
	}{
		{name: "file only", file: true, want: fileValue},
		{name: "env overrides file", file: true, env: true, want: envValue},
		{name: "flag overrides env", file: true, env: true, flag: true, want: flagValue},
		{name: "flag overrides file", file: true, flag: true, want: flagValue},
		{name: "env only", env: true, want: envValue},
		{name: "flag only", flag: true, want: flagValue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			if tt.file {
				writeConfig(t, dir, "root: "+fileValue+"\n")
			}
			if tt.env {
				t.Setenv("SHELFPRESS_ROOT", envValue)
			}

			opts := Options{Dir: dir}
			if tt.flag {
				opts.Flags = testFlags(t, map[string]string{"root": flagValue})
			} else {
				opts.Flags = testFlags(t, nil)
			}

			cfg, err := Load(opts)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.Root != tt.want {
				t.Errorf("Root = %q, want %q", cfg.Root, tt.want)
			}
		})
	}
}

func TestLoadUnsetFlagsDoNotOverride(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "root: from-file\nprune: false\n")

	// A flag set where nothing was explicitly set must not mask the file.
	cfg, err := Load(Options{Dir: dir, Flags: testFlags(t, nil)})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Root != "from-file" {
		t.Errorf("Root = %q, want %q", cfg.Root, "from-file")
	}
	if cfg.Prune {
		t.Error("Prune = true, want the config file's false to survive unset flags")
	}
}

func TestLoadBookStackEnv(t *testing.T) {
	dir := t.TempDir()

	t.Setenv("BOOKSTACK_URL", "https://wiki.example.com")
	t.Setenv("BOOKSTACK_TOKEN_ID", "env-id")
	t.Setenv("BOOKSTACK_TOKEN_SECRET", "env-secret")

	cfg, err := Load(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.BookStack.URL != "https://wiki.example.com" {
		t.Errorf("URL = %q", cfg.BookStack.URL)
	}
	if cfg.BookStack.TokenID != "env-id" {
		t.Errorf("TokenID = %q", cfg.BookStack.TokenID)
	}
	if cfg.BookStack.TokenSecret != "env-secret" {
		t.Errorf("TokenSecret = %q", cfg.BookStack.TokenSecret)
	}
}

func TestLoadNestedConfigFile(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `
root: docs/adr
shelf: My Decisions
prune: false
bookstack:
  url: https://wiki.example.com
`)

	cfg, err := Load(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Root != "docs/adr" {
		t.Errorf("Root = %q", cfg.Root)
	}
	if cfg.Shelf != "My Decisions" {
		t.Errorf("Shelf = %q", cfg.Shelf)
	}
	if cfg.Prune {
		t.Error("Prune = true, want false from the file")
	}
	if cfg.BookStack.URL != "https://wiki.example.com" {
		t.Errorf("BookStack.URL = %q", cfg.BookStack.URL)
	}
}

func TestLoadExplicitFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.yml")
	if err := os.WriteFile(path, []byte("root: custom\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	t.Run("explicit file is read", func(t *testing.T) {
		cfg, err := Load(Options{Dir: dir, File: path})
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.Root != "custom" {
			t.Errorf("Root = %q, want %q", cfg.Root, "custom")
		}
	})

	t.Run("missing explicit file is an error", func(t *testing.T) {
		_, err := Load(Options{Dir: dir, File: filepath.Join(dir, "absent.yml")})
		if err == nil {
			t.Fatal("Load() = nil error, want an error for a missing explicit file")
		}
	})

	t.Run("missing default file is not an error", func(t *testing.T) {
		if _, err := Load(Options{Dir: t.TempDir()}); err != nil {
			t.Fatalf("Load() error = %v, want nil when no config file exists", err)
		}
	})
}

func TestLoadMalformedFile(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "root: [unclosed\n")

	if _, err := Load(Options{Dir: dir}); err == nil {
		t.Fatal("Load() = nil error, want an error for malformed YAML")
	}
}

func TestShelfDefaultsFromRepo(t *testing.T) {
	tests := []struct {
		name string
		repo string
		want string
	}{
		{name: "owner and name", repo: "tobeck/shelfpress", want: "shelfpress"},
		{name: "bare name", repo: "shelfpress", want: "shelfpress"},
		{name: "empty", repo: "", want: ""},
		{name: "trailing slash", repo: "tobeck/", want: "tobeck/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shelfFromRepo(tt.repo); got != tt.want {
				t.Errorf("shelfFromRepo(%q) = %q, want %q", tt.repo, got, tt.want)
			}
		})
	}
}

func TestExplicitShelfSurvives(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "shelf: Explicit Shelf\nrepo: tobeck/shelfpress\n")

	cfg, err := Load(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Shelf != "Explicit Shelf" {
		t.Errorf("Shelf = %q, want the configured value to survive the fallback", cfg.Shelf)
	}
}
