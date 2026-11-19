package scan

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fixture builds a directory tree from a path -> content map. A path ending
// in "/" creates a directory.
func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()

	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(full, 0o750); err != nil {
				t.Fatalf("creating dir %s: %v", name, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("creating parent of %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return root
}

// sourcePaths extracts the SourcePath of each document, in order.
func sourcePaths(docs []Document) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.SourcePath
	}
	return out
}

func TestWalkFlatDirectory(t *testing.T) {
	root := fixture(t, map[string]string{
		"0002-use-kafka.md":   "# Use Kafka\n\nBody.\n",
		"0001-use-quarkus.md": "# Use Quarkus\n\nBody.\n",
		"0003-use-redis.md":   "# Use Redis\n\nBody.\n",
	})

	docs, err := Walk(Options{Root: root})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}

	want := []string{"0001-use-quarkus.md", "0002-use-kafka.md", "0003-use-redis.md"}
	got := sourcePaths(docs)
	if len(got) != len(want) {
		t.Fatalf("got %d documents (%v), want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("document %d = %q, want %q (results must be sorted)", i, got[i], want[i])
		}
	}

	if docs[0].Title != "Use Quarkus" {
		t.Errorf("Title = %q, want %q", docs[0].Title, "Use Quarkus")
	}
	if docs[0].Priority != 1 {
		t.Errorf("Priority = %d, want 1", docs[0].Priority)
	}
}

func TestWalkIgnoresNonMarkdown(t *testing.T) {
	root := fixture(t, map[string]string{
		"0001-adr.md":  "# ADR\n",
		"README.txt":   "not markdown",
		"diagram.png":  "binary-ish",
		"notes":        "no extension",
		"template.mdx": "not plain markdown",
	})

	docs, err := Walk(Options{Root: root})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	if got := sourcePaths(docs); len(got) != 1 || got[0] != "0001-adr.md" {
		t.Errorf("got %v, want only [0001-adr.md]", got)
	}
}

func TestWalkUppercaseExtension(t *testing.T) {
	root := fixture(t, map[string]string{"0001-shouty.MD": "# Shouty\n"})

	docs, err := Walk(Options{Root: root})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("got %d documents, want 1 — .MD is still markdown", len(docs))
	}
}

func TestWalkSkipsSubdirectoriesWithWarning(t *testing.T) {
	root := fixture(t, map[string]string{
		"0001-top.md":         "# Top\n",
		"backend/0001-sub.md": "# Sub\n",
		"ui/":                 "",
	})

	var warnings []string
	docs, err := Walk(Options{
		Root:  root,
		Warnf: func(format string, _ ...any) { warnings = append(warnings, format) },
	})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}

	if got := sourcePaths(docs); len(got) != 1 || got[0] != "0001-top.md" {
		t.Errorf("got %v, want only the top-level file", got)
	}
	if len(warnings) != 2 {
		t.Errorf("got %d warnings, want one per skipped subdirectory", len(warnings))
	}
	for _, w := range warnings {
		if !strings.Contains(w, "v0.3") {
			t.Errorf("warning %q should say when directory support arrives", w)
		}
	}
}

func TestWalkIgnoresDotEntries(t *testing.T) {
	root := fixture(t, map[string]string{
		"0001-real.md":      "# Real\n",
		".hidden.md":        "# Hidden\n",
		".git/config.md":    "# Not content\n",
		".obsidian/note.md": "# Editor noise\n",
	})

	docs, err := Walk(Options{Root: root})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	if got := sourcePaths(docs); len(got) != 1 || got[0] != "0001-real.md" {
		t.Errorf("got %v, want only [0001-real.md]", got)
	}
}

func TestWalkEmptyDirectoryIsNotAnError(t *testing.T) {
	docs, err := Walk(Options{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("Walk() error = %v, want nil for an empty directory", err)
	}
	if len(docs) != 0 {
		t.Errorf("got %d documents, want 0", len(docs))
	}
}

func TestWalkMissingRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")

	_, err := Walk(Options{Root: missing})
	if err == nil {
		t.Fatal("Walk() = nil error, want an error for a missing root")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error = %v, want it to say the root does not exist", err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error = %v, want it to name the path", err)
	}
}

func TestWalkEmptyRootOption(t *testing.T) {
	if _, err := Walk(Options{}); err == nil {
		t.Fatal("Walk() = nil error, want an error when Root is unset")
	}
}

func TestWalkMalformedDocumentFails(t *testing.T) {
	root := fixture(t, map[string]string{
		"0001-broken.md": "---\ntitle: [unclosed\n---\n# Heading\n",
	})

	_, err := Walk(Options{Root: root})
	if err == nil {
		t.Fatal("Walk() = nil error, want the parse failure surfaced")
	}
	if !strings.Contains(err.Error(), "0001-broken.md") {
		t.Errorf("error = %v, want it to name the offending file", err)
	}
}

func TestSourcePathIsRelativeToBase(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "docs", "decisions")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatalf("creating root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "0001-x.md"), []byte("# X\n"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	t.Run("relative to the repository root", func(t *testing.T) {
		docs, err := Walk(Options{Root: root, Base: repo})
		if err != nil {
			t.Fatalf("Walk() error = %v", err)
		}
		if got := docs[0].SourcePath; got != "docs/decisions/0001-x.md" {
			t.Errorf("SourcePath = %q, want %q", got, "docs/decisions/0001-x.md")
		}
	})

	t.Run("relative to the root when no base is given", func(t *testing.T) {
		docs, err := Walk(Options{Root: root})
		if err != nil {
			t.Fatalf("Walk() error = %v", err)
		}
		if got := docs[0].SourcePath; got != "0001-x.md" {
			t.Errorf("SourcePath = %q, want %q", got, "0001-x.md")
		}
	})
}

func TestSourcePathUsesForwardSlashes(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "docs", "decisions")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatalf("creating root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "0001-x.md"), []byte("# X\n"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	docs, err := Walk(Options{Root: root, Base: repo})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	// SourcePath becomes the sp-source tag, so it must never carry a
	// platform separator: a Windows sync would otherwise fail to match a
	// page created on Linux.
	if strings.Contains(docs[0].SourcePath, "\\") {
		t.Errorf("SourcePath %q contains a backslash", docs[0].SourcePath)
	}
	if !strings.Contains(docs[0].SourcePath, "/") {
		t.Errorf("SourcePath %q lost its separators", docs[0].SourcePath)
	}
}

func TestWalkSkipsIrregularFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "0001-real.md"), []byte("# Real\n"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	// A symlink pointing at a directory must not be read as a file.
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "0002-link.md")); err != nil {
		t.Fatalf("creating symlink: %v", err)
	}

	var warnings int
	docs, err := Walk(Options{Root: root, Warnf: func(string, ...any) { warnings++ }})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	if got := sourcePaths(docs); len(got) != 1 || got[0] != "0001-real.md" {
		t.Errorf("got %v, want only the regular file", got)
	}
	if warnings != 1 {
		t.Errorf("got %d warnings, want 1 for the skipped symlink", warnings)
	}
}

func TestWalkBrokenSymlinkIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}

	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(root, "0001-dangling.md")); err != nil {
		t.Fatalf("creating symlink: %v", err)
	}

	// A dangling link is a real problem with the docs tree, not something to
	// silently skip: the author probably meant it to resolve.
	if _, err := Walk(Options{Root: root}); err == nil {
		t.Fatal("Walk() = nil error, want an error for a dangling symlink")
	}
}

func TestWalkNilWarnfIsSafe(t *testing.T) {
	root := fixture(t, map[string]string{
		"0001-top.md": "# Top\n",
		"sub/":        "",
	})

	// Warnf is nil: the skipped subdirectory must not panic.
	if _, err := Walk(Options{Root: root}); err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
}

func TestWalkPreservesParsedFields(t *testing.T) {
	root := fixture(t, map[string]string{
		"0007-tagged.md": "---\nstatus: accepted\ntitle: From Frontmatter\n---\n# Ignored Heading\n\nBody text.\n",
	})

	docs, err := Walk(Options{Root: root})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	doc := docs[0]

	if doc.Title != "From Frontmatter" {
		t.Errorf("Title = %q, want the frontmatter title", doc.Title)
	}
	if doc.Priority != 7 {
		t.Errorf("Priority = %d, want 7", doc.Priority)
	}
	if doc.Frontmatter["status"] != "accepted" {
		t.Errorf("Frontmatter[status] = %v", doc.Frontmatter["status"])
	}
	if !strings.Contains(doc.Body, "# Ignored Heading") {
		t.Errorf("Body lost its content: %q", doc.Body)
	}
	if strings.Contains(doc.Body, "status: accepted") {
		t.Errorf("Body still contains frontmatter: %q", doc.Body)
	}
}
