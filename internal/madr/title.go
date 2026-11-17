package madr

import (
	"html"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// numericPrefix matches the leading NNNN- sequence of a MADR filename.
var numericPrefix = regexp.MustCompile(`^(\d+)[-_]`)

// md parses markdown structure. No renderer is configured because the body is
// published verbatim; goldmark is used only to locate the H1.
var md = goldmark.New()

// resolveTitle picks a title from, in order: frontmatter `title`, the first H1
// heading, then the filename.
func resolveTitle(fm map[string]any, body []byte, path string) string {
	if t := frontmatterTitle(fm); t != "" {
		return t
	}
	if t := firstHeading(body); t != "" {
		return t
	}
	return titleFromFilename(path)
}

// frontmatterTitle returns a non-empty string `title` from the frontmatter.
func frontmatterTitle(fm map[string]any) string {
	raw, ok := fm["title"]
	if !ok {
		return ""
	}
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// firstHeading returns the text of the document's first level-1 heading, with
// any inline markup reduced to its plain text. It returns "" when the document
// has no H1.
func firstHeading(body []byte) string {
	doc := md.Parser().Parse(text.NewReader(body))

	var title string
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		heading, ok := n.(*ast.Heading)
		if !ok || heading.Level != 1 {
			return ast.WalkContinue, nil
		}
		title = strings.TrimSpace(headingText(heading, body))
		return ast.WalkStop, nil
	})
	if err != nil {
		return ""
	}
	return title
}

// titleFromFilename derives a title from the file's base name by dropping the
// extension and any NNNN- prefix, then turning separators into spaces and
// title-casing the result.
func titleFromFilename(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = numericPrefix.ReplaceAllString(base, "")

	fields := strings.FieldsFunc(base, func(r rune) bool {
		return r == '-' || r == '_' || unicode.IsSpace(r)
	})
	for i, f := range fields {
		fields[i] = capitalise(f)
	}
	return strings.Join(fields, " ")
}

// capitalise upper-cases the first rune, leaving the rest untouched so that
// acronyms and camelCase words survive.
func capitalise(s string) string {
	for i, r := range s {
		return string(unicode.ToUpper(r)) + s[i+len(string(r)):]
	}
	return s
}

// priorityFromPath reads the NNNN- prefix of a filename, returning 0 when the
// name carries no numeric prefix.
func priorityFromPath(path string) int {
	m := numericPrefix.FindStringSubmatch(filepath.Base(path))
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// headingText flattens a heading's inline children to plain text, so that
// emphasis, code spans, and links contribute their text but not their markup.
//
// Node.Text is deprecated in goldmark, so the segments are read from the
// inline nodes directly.
func headingText(n ast.Node, source []byte) string {
	var b strings.Builder

	err := ast.Walk(n, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := child.(type) {
		case *ast.Text:
			b.Write(t.Segment.Value(source))
			if t.SoftLineBreak() || t.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(t.Value)
		case *ast.AutoLink:
			b.Write(t.URL(source))
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return ""
	}
	// Entities are left raw by the parser because decoding normally happens at
	// render time; titles are plain text in BookStack, so decode them here.
	return html.UnescapeString(b.String())
}
