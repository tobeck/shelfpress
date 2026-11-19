// Package scan walks a documentation directory and turns the markdown files
// it finds into documents the planner can diff against BookStack.
//
// v0.1 handles a single flat directory. The subdirectory and mixed-layout
// rules described in docs/ARCHITECTURE.md arrive in v0.3.
package scan

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tobeck/shelfpress/internal/document"
)

// markdownExt is the only file extension shelfpress publishes.
const markdownExt = ".md"

// Document is a markdown file found under the docs root, ready to publish.
type Document struct {
	// SourcePath identifies the file and is written to BookStack as the
	// sp-source tag. It is always slash-separated so a sync from Windows
	// matches one from Linux, and it is the identity key for the whole
	// reconcile — see docs/ARCHITECTURE.md.
	SourcePath string

	// Title, Body, Priority and Frontmatter come from parsing the file.
	Title       string
	Body        string
	Priority    int
	Frontmatter map[string]any
}

// Options controls a walk.
type Options struct {
	// Root is the directory to scan.
	Root string

	// Base is the directory SourcePath is reported relative to, normally the
	// repository root. When empty, paths are relative to Root.
	Base string

	// Warnf receives non-fatal notices, such as a skipped subdirectory. When
	// nil, notices are discarded.
	Warnf func(format string, args ...any)
}

// Walk returns a Document for every markdown file directly inside the root,
// sorted by SourcePath so that plans are stable between runs.
//
// Subdirectories are skipped with a warning; support for them lands in v0.3.
// An empty directory is not an error.
func Walk(opts Options) ([]Document, error) {
	if opts.Root == "" {
		return nil, fmt.Errorf("scan: root directory is required")
	}

	entries, err := os.ReadDir(opts.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("scan: docs root %q does not exist", opts.Root)
		}
		return nil, fmt.Errorf("scan: reading docs root %q: %w", opts.Root, err)
	}

	base := opts.Base
	if base == "" {
		base = opts.Root
	}

	docs := make([]Document, 0, len(entries))
	for _, entry := range entries {
		doc, ok, err := consider(opts, base, entry)
		if err != nil {
			return nil, err
		}
		if ok {
			docs = append(docs, doc)
		}
	}

	slices.SortFunc(docs, func(a, b Document) int {
		return strings.Compare(a.SourcePath, b.SourcePath)
	})
	return docs, nil
}

// consider decides whether one directory entry becomes a Document, reporting
// false for entries that are skipped.
func consider(opts Options, base string, entry fs.DirEntry) (Document, bool, error) {
	name := entry.Name()

	// Dotfiles and dot-directories are editor and VCS noise, never content.
	if strings.HasPrefix(name, ".") {
		return Document{}, false, nil
	}

	if entry.IsDir() {
		warn(opts, "skipping subdirectory %q: directory mapping arrives in v0.3", name)
		return Document{}, false, nil
	}

	if !strings.EqualFold(filepath.Ext(name), markdownExt) {
		return Document{}, false, nil
	}

	full := filepath.Join(opts.Root, name)

	// A symlink reported by ReadDir is not followed blindly: resolve it and
	// skip anything that is not a regular file.
	info, err := os.Stat(full)
	if err != nil {
		return Document{}, false, fmt.Errorf("scan: reading %s: %w", full, err)
	}
	if !info.Mode().IsRegular() {
		warn(opts, "skipping %q: not a regular file", name)
		return Document{}, false, nil
	}

	content, err := os.ReadFile(full) //nolint:gosec // path is built from the configured root
	if err != nil {
		return Document{}, false, fmt.Errorf("scan: reading %s: %w", full, err)
	}

	sourcePath, err := relativeTo(base, full)
	if err != nil {
		return Document{}, false, err
	}

	parsed, err := document.Parse(sourcePath, content)
	if err != nil {
		return Document{}, false, fmt.Errorf("scan: %w", err)
	}

	return Document{
		SourcePath:  sourcePath,
		Title:       parsed.Title,
		Body:        parsed.Body,
		Priority:    parsed.Priority,
		Frontmatter: parsed.Frontmatter,
	}, true, nil
}

// relativeTo expresses path relative to base using forward slashes, so the
// same file yields the same SourcePath on every operating system.
func relativeTo(base, path string) (string, error) {
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("scan: resolving %q: %w", base, err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("scan: resolving %q: %w", path, err)
	}

	rel, err := filepath.Rel(absBase, absPath)
	if err != nil {
		return "", fmt.Errorf("scan: relating %q to %q: %w", path, base, err)
	}
	return filepath.ToSlash(rel), nil
}

// warn reports a non-fatal notice when the caller supplied a sink.
func warn(opts Options, format string, args ...any) {
	if opts.Warnf != nil {
		opts.Warnf(format, args...)
	}
}
