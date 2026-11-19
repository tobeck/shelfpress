package document

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// fence delimits a YAML frontmatter block.
var fence = []byte("---")

// splitFrontmatter separates a leading YAML frontmatter block from the body.
//
// When the content has no frontmatter it returns a nil map and the content
// unchanged. A block that opens but never closes is treated as body text rather
// than an error, matching how common markdown renderers behave; malformed YAML
// inside a properly closed block is an error.
func splitFrontmatter(content []byte) (map[string]any, []byte, error) {
	rest, ok := openingFence(content)
	if !ok {
		return nil, content, nil
	}

	raw, body, ok := closingFence(rest)
	if !ok {
		// Unterminated block: treat the whole document as body.
		return nil, content, nil
	}

	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, body, nil
	}

	var fm map[string]any
	if err := yaml.Unmarshal(raw, &fm); err != nil {
		return nil, nil, fmt.Errorf("malformed YAML frontmatter: %w", err)
	}
	return fm, body, nil
}

// openingFence consumes a leading `---` line, reporting whether one was present
// and returning the content that follows it.
func openingFence(content []byte) ([]byte, bool) {
	// A UTF-8 BOM before the fence is common on Windows-authored files.
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))

	line, rest, found := cutLine(content)
	if !found && len(line) == 0 {
		return nil, false
	}
	if !bytes.Equal(bytes.TrimRight(line, "\r"), fence) {
		return nil, false
	}
	return rest, true
}

// closingFence scans for the terminating `---` line, returning the YAML source
// before it and the body after it.
func closingFence(content []byte) (raw, body []byte, ok bool) {
	var yamlLines [][]byte

	for rest := content; ; {
		line, next, found := cutLine(rest)

		if bytes.Equal(bytes.TrimRight(line, "\r"), fence) {
			return bytes.Join(yamlLines, []byte("\n")), next, true
		}
		if !found {
			// Reached the end without a closing fence.
			return nil, nil, false
		}

		yamlLines = append(yamlLines, line)
		rest = next
	}
}

// cutLine splits off the first line, reporting whether a newline was found.
// The returned line excludes the newline but retains any carriage return, so
// callers decide how to handle CRLF.
func cutLine(b []byte) (line, rest []byte, found bool) {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return b[:i], b[i+1:], true
	}
	return b, nil, false
}
