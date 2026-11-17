package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestVersionCommand(t *testing.T) {
	var out bytes.Buffer
	root := newRootForTest(&out)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != version {
		t.Errorf("version output = %q, want %q", got, version)
	}
}

// newRootForTest builds the command tree with output redirected.
func newRootForTest(out *bytes.Buffer) *cobra.Command {
	root := newRoot()
	root.SetOut(out)
	root.SetErr(out)
	return root
}
