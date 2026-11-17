// Package madr parses markdown documents into the fields shelfpress publishes.
//
// It is named for [MADR], the architecture-decision-record format shelfpress was
// built around, but nothing here is MADR-specific: any markdown file parses, and
// a document without frontmatter or a numeric prefix is entirely valid.
//
// [MADR]: https://adr.github.io/madr/
package madr

import (
	"fmt"
)

// Document is a parsed markdown file.
type Document struct {
	// Title is the page title, resolved from frontmatter, the first H1, or the
	// filename, in that order.
	Title string

	// Body is the markdown content with any frontmatter removed. It is sent to
	// BookStack verbatim and is never reformatted.
	Body string

	// Priority orders documents within a book, taken from a leading NNNN-
	// filename prefix. Zero when the filename carries no prefix.
	Priority int

	// Frontmatter holds the parsed YAML frontmatter, or nil when the document
	// has none. Mapping these onto BookStack tags is deliberately not done
	// here; that arrives in v0.4.
	Frontmatter map[string]any
}

// Parse reads a markdown document. The path is used to derive the priority and
// the fallback title, and to name the file in any error; it is not read from
// disk, so callers control all I/O.
func Parse(path string, content []byte) (Document, error) {
	fm, body, err := splitFrontmatter(content)
	if err != nil {
		return Document{}, fmt.Errorf("parsing %s: %w", path, err)
	}

	return Document{
		Title:       resolveTitle(fm, body, path),
		Body:        string(body),
		Priority:    priorityFromPath(path),
		Frontmatter: fm,
	}, nil
}
