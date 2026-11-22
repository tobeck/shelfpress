//go:build integration

// Package integration exercises shelfpress against a live BookStack instance.
//
// Nothing in this package is a test double. The unit suites cover behaviour
// against fakes; these tests exist because a fake can only confirm what its
// author already believed. The bug in #22 — every sync rewriting every page,
// because /api/search returns no page body — passed a unit suite at 91%
// coverage, since the fake returned bodies the real API never sends.
//
// The whole package is behind the `integration` build tag, so `go test ./...`
// does not compile it and a contributor without Docker still gets a green
// unit suite. Run it with `make test-integration` against a BookStack started
// by `make dev-up` and a token from `make dev-token`.
package integration

import (
	"fmt"
	"os"
	"testing"
)

// Environment variables the suite reads. These are the same ones the CLI
// reads, so a developer who can run shelfpress can run these tests.
const (
	envURL         = "BOOKSTACK_URL"
	envTokenID     = "BOOKSTACK_TOKEN_ID"
	envTokenSecret = "BOOKSTACK_TOKEN_SECRET"
)

// skipReason is empty when the suite can run, and otherwise explains what is
// missing. It is consulted by requireInstance, which every test calls.
var skipReason string

// TestMain records whether an instance is configured. The check cannot skip
// from here — `go test` discards a package's own output when the run
// succeeds, so a message printed here is invisible and the result reads "ok",
// as though tests had run. Skipping per-test instead makes `go test` report
// SKIP lines, which is what a developer needs to see.
func TestMain(m *testing.M) {
	if missing := missingEnv(); len(missing) > 0 {
		skipReason = fmt.Sprintf(
			"%s not set — these tests need a live BookStack. "+
				`Run: make dev-up && eval "$(make dev-token)" && make test-integration `+
				"(see deploy/dev/README.md)",
			join(missing),
		)
	}

	os.Exit(m.Run())
}

// requireInstance skips the calling test when no BookStack is configured.
// Every test in the package starts with this.
func requireInstance(t *testing.T) {
	t.Helper()

	if skipReason != "" {
		t.Skip(skipReason)
	}
}

// missingEnv reports which required variables are unset or empty.
func missingEnv() []string {
	var missing []string
	for _, key := range []string{envURL, envTokenID, envTokenSecret} {
		if os.Getenv(key) == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

// join renders a variable list for the skip message.
func join(keys []string) string {
	switch len(keys) {
	case 1:
		return keys[0]
	case 2:
		return keys[0] + " and " + keys[1]
	default:
		return keys[0] + ", " + join(keys[1:])
	}
}
