// Package document parses markdown files into the fields shelfpress publishes.
//
// Nothing here is specific to any markdown dialect: a file without frontmatter,
// an H1, or a numeric prefix parses perfectly well. [MADR] architecture
// decision records are the flagship case and get first-class treatment — their
// frontmatter is preserved for tag mapping and their NNNN- prefix sets page
// order — but they are one supported shape, not a requirement.
//
// Validating that a document is a *correct* MADR is deliberately out of scope;
// dedicated linters such as adrkit and adrlint do that well. This package only
// cares what will reach BookStack.
//
// [MADR]: https://adr.github.io/madr/
package document

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
