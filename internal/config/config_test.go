package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	dir := t.TempDir()

	valid := Config{
		Root:      dir,
		Format:    FormatHuman,
		BookStack: BookStack{URL: "https://wiki.example.com", TokenID: "id", TokenSecret: "secret"},
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "valid", mutate: func(*Config) {}},
		{
			name:    "missing url",
			mutate:  func(c *Config) { c.BookStack.URL = "" },
			wantErr: "BOOKSTACK_URL",
		},
		{
			name:    "missing token id",
			mutate:  func(c *Config) { c.BookStack.TokenID = "" },
			wantErr: "BOOKSTACK_TOKEN_ID",
		},
		{
			name:    "missing token secret",
			mutate:  func(c *Config) { c.BookStack.TokenSecret = "" },
			wantErr: "BOOKSTACK_TOKEN_SECRET",
		},
		{
			name:    "root does not exist",
			mutate:  func(c *Config) { c.Root = filepath.Join(dir, "nope") },
			wantErr: "does not exist",
		},
		{
			name: "root is a file",
			mutate: func(c *Config) {
				f := filepath.Join(dir, "file.md")
				if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
					t.Fatalf("writing fixture: %v", err)
				}
				c.Root = f
			},
			wantErr: "not a directory",
		},
		{
			name:    "unknown format",
			mutate:  func(c *Config) { c.Format = "yaml" },
			wantErr: `unknown format "yaml"`,
		},
		{
			name:    "empty root",
			mutate:  func(c *Config) { c.Root = "" },
			wantErr: "docs root is not set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)

			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tt.wantErr)
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("error does not wrap ErrInvalidConfig: %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateReportsAllProblems(t *testing.T) {
	cfg := Config{Format: "bogus"}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error")
	}
	for _, want := range []string{
		"BOOKSTACK_URL",
		"BOOKSTACK_TOKEN_ID",
		"BOOKSTACK_TOKEN_SECRET",
		"docs root is not set",
		"unknown format",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q; got:\n%s", want, err)
		}
	}
}

func TestRedaction(t *testing.T) {
	cfg := Config{
		Root:      "docs/decisions",
		BookStack: BookStack{URL: "https://wiki.example.com", TokenID: "tok-id", TokenSecret: "tok-secret"},
	}

	t.Run("Redacted replaces secrets", func(t *testing.T) {
		got := cfg.Redacted()
		if got.BookStack.TokenID != redacted {
			t.Errorf("TokenID = %q, want %q", got.BookStack.TokenID, redacted)
		}
		if got.BookStack.TokenSecret != redacted {
			t.Errorf("TokenSecret = %q, want %q", got.BookStack.TokenSecret, redacted)
		}
		if got.BookStack.URL != cfg.BookStack.URL {
			t.Errorf("URL was altered: %q", got.BookStack.URL)
		}
	})

	t.Run("original is unchanged", func(t *testing.T) {
		_ = cfg.Redacted()
		if cfg.BookStack.TokenID != "tok-id" {
			t.Errorf("Redacted mutated the receiver: TokenID = %q", cfg.BookStack.TokenID)
		}
	})

	t.Run("String leaks no secrets", func(t *testing.T) {
		s := cfg.String()
		for _, secret := range []string{"tok-id", "tok-secret"} {
			if strings.Contains(s, secret) {
				t.Errorf("String() leaked %q: %s", secret, s)
			}
		}
	})

	t.Run("empty secrets are not labelled redacted", func(t *testing.T) {
		got := Config{}.Redacted()
		if got.BookStack.TokenID != "" {
			t.Errorf("TokenID = %q, want empty", got.BookStack.TokenID)
		}
	})
}
