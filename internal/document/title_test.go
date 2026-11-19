package document

import "testing"

func TestTitleFromFilename(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "madr style", path: "docs/decisions/0001-use-quarkus.md", want: "Use Quarkus"},
		{name: "no prefix", path: "docs/use-quarkus.md", want: "Use Quarkus"},
		{name: "underscores", path: "docs/0002_use_kafka.md", want: "Use Kafka"},
		{name: "mixed separators", path: "docs/0003-use_both-styles.md", want: "Use Both Styles"},
		{name: "single word", path: "docs/overview.md", want: "Overview"},
		{name: "already capitalised", path: "docs/0004-Use-Quarkus.md", want: "Use Quarkus"},
		{name: "acronym preserved", path: "docs/0005-use-HTTP-api.md", want: "Use HTTP Api"},
		{name: "camel case preserved", path: "docs/0006-useVueJS.md", want: "UseVueJS"},
		{name: "long prefix", path: "docs/00012-use-redis.md", want: "Use Redis"},
		{name: "no extension", path: "docs/0007-plain", want: "Plain"},
		{name: "bare filename", path: "0008-top-level.md", want: "Top Level"},
		{name: "digits only", path: "docs/0009-.md", want: ""},
		{name: "unicode", path: "docs/0010-använd-quarkus.md", want: "Använd Quarkus"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := titleFromFilename(tt.path); got != tt.want {
				t.Errorf("titleFromFilename(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestPriorityFromPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want int
	}{
		{name: "leading zeros", path: "docs/0001-x.md", want: 1},
		{name: "four digits", path: "docs/1234-x.md", want: 1234},
		{name: "underscore separator", path: "docs/0007_x.md", want: 7},
		{name: "no prefix", path: "docs/use-quarkus.md", want: 0},
		{name: "digits not a prefix", path: "docs/use-2-things.md", want: 0},
		{name: "zero", path: "docs/0000-x.md", want: 0},
		{name: "no separator after digits", path: "docs/0001x.md", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := priorityFromPath(tt.path); got != tt.want {
				t.Errorf("priorityFromPath(%q) = %d, want %d", tt.path, got, tt.want)
			}
		})
	}
}

func TestFirstHeading(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "atx", body: "# Plain Title\n", want: "Plain Title"},
		{name: "setext", body: "Plain Title\n===\n", want: "Plain Title"},
		{name: "inline code", body: "# Use `goldmark` here\n", want: "Use goldmark here"},
		{name: "emphasis", body: "# Use *Quarkus* now\n", want: "Use Quarkus now"},
		{name: "bold", body: "# Use **Kafka**\n", want: "Use Kafka"},
		{name: "link", body: "# See [the docs](https://example.com)\n", want: "See the docs"},
		{name: "trailing spaces", body: "#   Spaced   \n", want: "Spaced"},
		{name: "closed atx", body: "# Closed Form #\n", want: "Closed Form"},
		{name: "no heading", body: "Just text.\n", want: ""},
		{name: "only h2", body: "## Second Level\n", want: ""},
		{name: "empty", body: "", want: ""},
		{name: "heading in code fence ignored", body: "```\n# Not A Heading\n```\n", want: ""},
		{name: "nested emphasis", body: "# A ***very*** bold one\n", want: "A very bold one"},
		{name: "autolink", body: "# See <https://example.com>\n", want: "See https://example.com"},
		{name: "image alt text", body: "# Title ![alt](img.png)\n", want: "Title alt"},
		{name: "html entity", body: "# Caf&eacute; choice\n", want: "Café choice"},
		{name: "multiline setext", body: "First line\nsecond line\n===\n", want: "First line second line"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstHeading([]byte(tt.body)); got != tt.want {
				t.Errorf("firstHeading(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

func TestFrontmatterTitle(t *testing.T) {
	tests := []struct {
		name string
		fm   map[string]any
		want string
	}{
		{name: "string title", fm: map[string]any{"title": "My Title"}, want: "My Title"},
		{name: "padded", fm: map[string]any{"title": "  My Title  "}, want: "My Title"},
		{name: "absent", fm: map[string]any{"status": "accepted"}, want: ""},
		{name: "nil map", fm: nil, want: ""},
		{name: "empty string", fm: map[string]any{"title": ""}, want: ""},
		{name: "non-string", fm: map[string]any{"title": 42}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := frontmatterTitle(tt.fm); got != tt.want {
				t.Errorf("frontmatterTitle(%v) = %q, want %q", tt.fm, got, tt.want)
			}
		})
	}
}
