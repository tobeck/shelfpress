// Package main_test holds architecture tests: rules about the shape of the
// codebase that no single package can enforce on its own.
package main_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// ciEnvPrefixes are environment variables belonging to a CI platform.
//
// shelfpress is distributed as a binary and a container and must run the same
// under GitHub Actions, GitLab CI, Woodpecker, Jenkins, a cron job, or a
// laptop. Reading any of these in the core would quietly make one platform
// privileged. Wrappers (action.yml and the documented CI examples) translate
// their platform's environment into CLI flags instead.
//
// See CLAUDE.md rule 1 and the "Configuration" section of docs/ARCHITECTURE.md.
var ciEnvPrefixes = []string{
	"GITHUB_",
	"GITLAB_",
	"CI_PROJECT_",
	"CI_COMMIT_",
	"WOODPECKER_",
	"JENKINS_",
	"DRONE_",
	"BUILDKITE_",
	"CIRCLE_",
	"TRAVIS_",
	"TEAMCITY_",
	"BITBUCKET_",
}

// scannedDirs are the trees the rule applies to: everything that ships in the
// binary.
var scannedDirs = []string{"internal", "cmd"}

func TestCoreReadsNoCIEnvironmentVariables(t *testing.T) {
	for _, dir := range scannedDirs {
		t.Run(dir, func(t *testing.T) {
			fset := token.NewFileSet()

			err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() || !strings.HasSuffix(path, ".go") {
					return nil
				}
				// The rule is about what the shipped binary reads. Test files
				// legitimately mention these names (this file included).
				if strings.HasSuffix(path, "_test.go") {
					return nil
				}

				file, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
				if parseErr != nil {
					return parseErr
				}

				for _, lit := range stringLiterals(file) {
					prefix, found := matchCIPrefix(lit.value)
					if !found {
						continue
					}
					t.Errorf(`%s: string %q references the CI-platform variable prefix %q.

shelfpress must run identically under any CI system, so the core reads only
SHELFPRESS_* and BOOKSTACK_*. A wrapper (action.yml, or a documented CI
example) should translate its platform's environment into a CLI flag instead
— for example the GitHub Action passes GITHUB_REPOSITORY as --repo.

See CLAUDE.md rule 1.`,
						fset.Position(lit.pos), lit.value, prefix)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("walking %s: %v", dir, err)
			}
		})
	}
}

// literal is a string constant found in the source, with its position.
type literal struct {
	value string
	pos   token.Pos
}

// stringLiterals returns every string literal in the file, with comments and
// identifiers excluded — the rule is about values the program reads, not about
// prose that happens to name a variable.
func stringLiterals(file *ast.File) []literal {
	var out []literal

	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			// A malformed literal would not compile; skip rather than fail.
			return true
		}
		out = append(out, literal{value: value, pos: lit.Pos()})
		return true
	})
	return out
}

// matchCIPrefix reports whether s contains a known CI variable prefix.
//
// It matches anywhere in the string rather than only at the start, so a
// constructed name such as "GITHUB_" + suffix is still caught.
func matchCIPrefix(s string) (string, bool) {
	upper := strings.ToUpper(s)
	for _, prefix := range ciEnvPrefixes {
		if strings.Contains(upper, prefix) {
			return prefix, true
		}
	}
	return "", false
}

// TestGuardDetectsAViolation checks the guard itself: a rule that cannot fail
// is worthless, and this one would silently pass forever if the literal scan
// broke.
func TestGuardDetectsAViolation(t *testing.T) {
	const offending = `package core

import "os"

func repo() string { return os.Getenv("GITHUB_REPOSITORY") }
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "core.go", offending, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}

	var found bool
	for _, lit := range stringLiterals(file) {
		if _, ok := matchCIPrefix(lit.value); ok {
			found = true
		}
	}
	if !found {
		t.Error("the guard did not flag os.Getenv(\"GITHUB_REPOSITORY\"); the scan is broken")
	}
}

func TestMatchCIPrefix(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "github repository", value: "GITHUB_REPOSITORY", want: true},
		{name: "gitlab project path", value: "CI_PROJECT_PATH", want: true},
		{name: "woodpecker", value: "WOODPECKER_REPO", want: true},
		{name: "embedded in a longer string", value: "reading GITHUB_SHA now", want: true},
		{name: "lowercase still matches", value: "github_repository", want: true},
		{name: "shelfpress variable", value: "SHELFPRESS_ROOT", want: false},
		{name: "bookstack variable", value: "BOOKSTACK_URL", want: false},
		{name: "unrelated", value: "docs/decisions", want: false},
		{name: "empty", value: "", want: false},
		{name: "the word github alone", value: "github.com/tobeck/shelfpress", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, got := matchCIPrefix(tt.value); got != tt.want {
				t.Errorf("matchCIPrefix(%q) = %t, want %t", tt.value, got, tt.want)
			}
		})
	}
}
