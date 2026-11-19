package document

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		content      string
		wantTitle    string
		wantBody     string
		wantPriority int
		wantFM       bool
	}{
		{
			name: "frontmatter title wins over H1",
			path: "docs/decisions/0001-use-quarkus.md",
			content: "---\ntitle: Frontmatter Title\nstatus: accepted\n---\n" +
				"# Heading Title\n\nBody text.\n",
			wantTitle:    "Frontmatter Title",
			wantBody:     "# Heading Title\n\nBody text.\n",
			wantPriority: 1,
			wantFM:       true,
		},
		{
			name:         "H1 used when frontmatter has no title",
			path:         "docs/decisions/0002-use-kafka.md",
			content:      "---\nstatus: accepted\n---\n# Use Kafka\n\nBody.\n",
			wantTitle:    "Use Kafka",
			wantBody:     "# Use Kafka\n\nBody.\n",
			wantPriority: 2,
			wantFM:       true,
		},
		{
			name:         "H1 without frontmatter",
			path:         "docs/decisions/0003-use-redis.md",
			content:      "# Use Redis\n\nBody.\n",
			wantTitle:    "Use Redis",
			wantBody:     "# Use Redis\n\nBody.\n",
			wantPriority: 3,
		},
		{
			name:         "filename fallback when no H1",
			path:         "docs/decisions/0004-use-postgres-db.md",
			content:      "Just body text, no heading.\n",
			wantTitle:    "Use Postgres Db",
			wantBody:     "Just body text, no heading.\n",
			wantPriority: 4,
		},
		{
			name:         "no numeric prefix",
			path:         "docs/decisions/use-vuejs.md",
			content:      "Body only.\n",
			wantTitle:    "Use Vuejs",
			wantBody:     "Body only.\n",
			wantPriority: 0,
		},
		{
			name:         "underscores in filename",
			path:         "docs/0005_use_nats.md",
			content:      "Body.\n",
			wantTitle:    "Use Nats",
			wantPriority: 5,
			wantBody:     "Body.\n",
		},
		{
			name:         "empty file",
			path:         "docs/decisions/0006-empty.md",
			content:      "",
			wantTitle:    "Empty",
			wantBody:     "",
			wantPriority: 6,
		},
		{
			name:         "H2 is not used as the title",
			path:         "docs/decisions/0007-second-level.md",
			content:      "## Not The Title\n\nBody.\n",
			wantTitle:    "Second Level",
			wantBody:     "## Not The Title\n\nBody.\n",
			wantPriority: 7,
		},
		{
			name:         "setext H1 is recognised",
			path:         "docs/decisions/0008-setext.md",
			content:      "Underlined Title\n================\n\nBody.\n",
			wantTitle:    "Underlined Title",
			wantBody:     "Underlined Title\n================\n\nBody.\n",
			wantPriority: 8,
		},
		{
			name:         "later H1 used when the first heading is deeper",
			path:         "docs/decisions/0009-later.md",
			content:      "## Sub\n\n# Real Title\n\nBody.\n",
			wantTitle:    "Real Title",
			wantBody:     "## Sub\n\n# Real Title\n\nBody.\n",
			wantPriority: 9,
		},
		{
			name:         "frontmatter only, no body",
			path:         "docs/decisions/0010-meta.md",
			content:      "---\ntitle: Only Meta\n---\n",
			wantTitle:    "Only Meta",
			wantBody:     "",
			wantPriority: 10,
			wantFM:       true,
		},
		{
			name:         "empty frontmatter block",
			path:         "docs/decisions/0011-blank-meta.md",
			content:      "---\n---\n# Real Title\n\nBody.\n",
			wantTitle:    "Real Title",
			wantBody:     "# Real Title\n\nBody.\n",
			wantPriority: 11,
		},
		{
			name:         "unterminated frontmatter is treated as body",
			path:         "docs/decisions/0012-unterminated.md",
			content:      "---\ntitle: Never Closed\n\nstill body\n",
			wantTitle:    "Unterminated",
			wantBody:     "---\ntitle: Never Closed\n\nstill body\n",
			wantPriority: 12,
		},
		{
			name:         "horizontal rule mid-document is not frontmatter",
			path:         "docs/decisions/0013-rule.md",
			content:      "# Title\n\n---\n\nAfter the rule.\n",
			wantTitle:    "Title",
			wantBody:     "# Title\n\n---\n\nAfter the rule.\n",
			wantPriority: 13,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Parse(tt.path, []byte(tt.content))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			if doc.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", doc.Title, tt.wantTitle)
			}
			if doc.Body != tt.wantBody {
				t.Errorf("Body = %q, want %q", doc.Body, tt.wantBody)
			}
			if doc.Priority != tt.wantPriority {
				t.Errorf("Priority = %d, want %d", doc.Priority, tt.wantPriority)
			}
			if gotFM := doc.Frontmatter != nil; gotFM != tt.wantFM {
				t.Errorf("Frontmatter present = %t, want %t (%v)", gotFM, tt.wantFM, doc.Frontmatter)
			}
		})
	}
}

func TestParseCRLF(t *testing.T) {
	content := "---\r\ntitle: Windows Authored\r\nstatus: accepted\r\n---\r\n# Heading\r\n\r\nBody.\r\n"

	doc, err := Parse("docs/0001-windows.md", []byte(content))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if doc.Title != "Windows Authored" {
		t.Errorf("Title = %q, want %q", doc.Title, "Windows Authored")
	}
	if doc.Frontmatter["status"] != "accepted" {
		t.Errorf("Frontmatter[status] = %v, want %q", doc.Frontmatter["status"], "accepted")
	}
	// The body is published verbatim, so CRLF line endings survive.
	if doc.Body != "# Heading\r\n\r\nBody.\r\n" {
		t.Errorf("Body = %q, want the CRLF body preserved", doc.Body)
	}
}

func TestParseMalformedFrontmatter(t *testing.T) {
	const path = "docs/decisions/0001-broken.md"
	content := "---\ntitle: [unclosed\nstatus: accepted\n---\n# Heading\n"

	_, err := Parse(path, []byte(content))
	if err == nil {
		t.Fatal("Parse() = nil error, want an error for malformed YAML")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error does not name the file: %v", err)
	}
	if !strings.Contains(err.Error(), "frontmatter") {
		t.Errorf("error does not mention frontmatter: %v", err)
	}
}

func TestParseFrontmatterIsExposedNotMapped(t *testing.T) {
	content := "---\n" +
		"status: accepted\n" +
		"date: 2026-11-17\n" +
		"decision-makers: [alice, bob]\n" +
		"---\n# Title\n"

	doc, err := Parse("docs/0001-meta.md", []byte(content))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if doc.Frontmatter["status"] != "accepted" {
		t.Errorf("status = %v", doc.Frontmatter["status"])
	}
	if _, ok := doc.Frontmatter["decision-makers"]; !ok {
		t.Error("decision-makers missing from frontmatter")
	}
	// Tag mapping is deliberately out of scope until v0.4; the raw map is all
	// this package provides.
	if len(doc.Frontmatter) != 3 {
		t.Errorf("Frontmatter has %d keys, want 3", len(doc.Frontmatter))
	}
}

func TestParseDoesNotReformatBody(t *testing.T) {
	// Deliberately irregular markdown: the body must survive byte for byte
	// because BookStack receives it verbatim.
	body := "#   Spaced Heading\n\n\n*  loose  list\n-    another\n\n\ttab-indented\n"

	doc, err := Parse("docs/0001-x.md", []byte(body))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if doc.Body != body {
		t.Errorf("Body was altered:\ngot:  %q\nwant: %q", doc.Body, body)
	}
}
