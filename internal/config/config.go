// Package config holds the typed configuration shelfpress runs from and loads it
// from defaults, a YAML file, the environment, and explicit flags.
//
// The core never reads a CI platform's environment variables. Only SHELFPRESS_*
// and BOOKSTACK_* are consulted; wrappers such as the GitHub Action translate
// their platform's environment into CLI flags. See CLAUDE.md rule 1.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Output formats shelfpress can render a plan or result in.
const (
	FormatHuman    = "human"
	FormatJSON     = "json"
	FormatMarkdown = "markdown"
)

// redacted replaces secret values in user-visible output.
const redacted = "[redacted]"

// Defaults applied before any other configuration source.
const (
	DefaultRoot   = "docs/decisions"
	DefaultPrune  = true
	DefaultFormat = FormatHuman
)

// BookStack holds the connection details for a BookStack instance.
type BookStack struct {
	URL         string `mapstructure:"url"`
	TokenID     string `mapstructure:"token_id"`
	TokenSecret string `mapstructure:"token_secret"`
}

// Config is the fully resolved configuration for a shelfpress run.
type Config struct {
	Root             string    `mapstructure:"root"`
	Shelf            string    `mapstructure:"shelf"`
	ShelfDescription string    `mapstructure:"shelf_description"`
	Repo             string    `mapstructure:"repo"`
	Prune            bool      `mapstructure:"prune"`
	DryRun           bool      `mapstructure:"dry_run"`
	Format           string    `mapstructure:"format"`
	OutputFile       string    `mapstructure:"output_file"`
	BookStack        BookStack `mapstructure:"bookstack"`
}

// ErrInvalidConfig is returned by Validate for any configuration problem, so
// callers can map configuration failures to a distinct exit code.
var ErrInvalidConfig = errors.New("invalid configuration")

// validFormats is the set of values Format accepts.
//
// Only FormatHuman is rendered in v0.1; the others are accepted here so the
// milestone that adds them needs no change to validation.
var validFormats = map[string]bool{
	FormatHuman:    true,
	FormatJSON:     true,
	FormatMarkdown: true,
}

// Validate reports whether the configuration is usable, returning an error that
// names both the flag and the environment variable a user could set to fix it.
// All problems are reported together rather than one per run.
func (c Config) Validate() error {
	var problems []string

	if c.BookStack.URL == "" {
		problems = append(problems, "BookStack URL is not set (--url or BOOKSTACK_URL)")
	}
	if c.BookStack.TokenID == "" {
		problems = append(problems, "BookStack token ID is not set (--token-id or BOOKSTACK_TOKEN_ID)")
	}
	if c.BookStack.TokenSecret == "" {
		problems = append(problems, "BookStack token secret is not set (--token-secret or BOOKSTACK_TOKEN_SECRET)")
	}

	if c.Root == "" {
		problems = append(problems, "docs root is not set (--root or SHELFPRESS_ROOT)")
	} else {
		switch info, err := os.Stat(c.Root); {
		case errors.Is(err, os.ErrNotExist):
			problems = append(problems, fmt.Sprintf("docs root %q does not exist (--root or SHELFPRESS_ROOT)", c.Root))
		case err != nil:
			problems = append(problems, fmt.Sprintf("docs root %q cannot be read: %v", c.Root, err))
		case !info.IsDir():
			problems = append(problems, fmt.Sprintf("docs root %q is not a directory", c.Root))
		}
	}

	if !validFormats[c.Format] {
		problems = append(problems, fmt.Sprintf(
			"unknown format %q: want one of %s (--format or SHELFPRESS_FORMAT)",
			c.Format, strings.Join(formatNames(), ", ")))
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%w:\n  - %s", ErrInvalidConfig, strings.Join(problems, "\n  - "))
}

// formatNames returns the accepted format values in a stable order.
func formatNames() []string {
	return []string{FormatHuman, FormatJSON, FormatMarkdown}
}

// Redacted returns a copy of the configuration with secrets replaced, so it can
// be logged or printed safely. See CLAUDE.md rule 6.
func (c Config) Redacted() Config {
	if c.BookStack.TokenID != "" {
		c.BookStack.TokenID = redacted
	}
	if c.BookStack.TokenSecret != "" {
		c.BookStack.TokenSecret = redacted
	}
	return c
}

// String renders the configuration with secrets redacted. It exists so that
// accidentally formatting a Config cannot leak a token.
func (c Config) String() string {
	r := c.Redacted()
	return fmt.Sprintf(
		"Config{Root:%q Shelf:%q Repo:%q Prune:%t DryRun:%t Format:%q OutputFile:%q "+
			"BookStack:{URL:%q TokenID:%q TokenSecret:%q}}",
		r.Root, r.Shelf, r.Repo, r.Prune, r.DryRun, r.Format, r.OutputFile,
		r.BookStack.URL, r.BookStack.TokenID, r.BookStack.TokenSecret)
}
