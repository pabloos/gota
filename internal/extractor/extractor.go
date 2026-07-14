// Package extractor pulls "gota:" blocks out of Go doc comments and parses
// them as OpenAPI Operation fragments. The YAML under "gota:" IS OpenAPI —
// there is no intermediate DSL to learn.
package extractor

import (
	"fmt"
	"go/ast"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/pabloos/gota/pkg/model"
)

// marker is the line that introduces a gota block. It may appear with or
// without a trailing value on the same line, e.g. "gota:" on its own line
// followed by an indented YAML mapping.
const marker = "gota:"

// Extract looks for a "gota:" block in doc and parses the YAML that follows
// it into an Operation fragment. It returns (nil, false) if doc contains no
// gota block.
func Extract(doc *ast.CommentGroup) (*model.Operation, bool, error) {
	if doc == nil {
		return nil, false, nil
	}
	lines := commentLines(doc)

	start := -1
	firstLineRest := ""
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == marker {
			start = i + 1
			break
		}
		if rest, ok := strings.CutPrefix(trimmed, marker); ok && strings.TrimSpace(rest) != "" {
			// Inline form: "gota: {summary: ...}" all on one line.
			start = i
			firstLineRest = rest
			break
		}
	}
	if start == -1 {
		return nil, false, nil
	}

	var yamlLines []string
	if firstLineRest != "" {
		yamlLines = append(yamlLines, firstLineRest)
	}
	yamlLines = append(yamlLines, lines[start:]...)

	block := dedent(yamlLines)
	if strings.TrimSpace(block) == "" {
		return nil, false, fmt.Errorf("extractor: empty gota: block")
	}

	var op model.Operation
	if err := yaml.Unmarshal([]byte(block), &op); err != nil {
		return nil, false, fmt.Errorf("extractor: invalid YAML in gota: block: %w", err)
	}
	return &op, true, nil
}

// commentLines returns the doc comment's text, one entry per source line,
// with the leading "//" (or "/*"..."*/") comment markers stripped but
// original indentation of the content preserved.
func commentLines(doc *ast.CommentGroup) []string {
	var lines []string
	for _, c := range doc.List {
		text := c.Text
		if strings.HasPrefix(text, "//") {
			text = strings.TrimPrefix(text, "//")
			text = strings.TrimPrefix(text, " ")
			lines = append(lines, text)
			continue
		}
		if strings.HasPrefix(text, "/*") {
			text = strings.TrimPrefix(text, "/*")
			text = strings.TrimSuffix(text, "*/")
			for _, l := range strings.Split(text, "\n") {
				lines = append(lines, strings.TrimPrefix(l, " "))
			}
		}
	}
	return lines
}

// dedent removes the common leading whitespace shared by all non-blank
// lines, so the YAML block parses regardless of how deeply the source
// comment itself was indented.
func dedent(lines []string) string {
	minIndent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if minIndent == -1 || indent < minIndent {
			minIndent = indent
		}
	}
	if minIndent <= 0 {
		return strings.Join(lines, "\n")
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		if len(line) >= minIndent {
			out[i] = line[minIndent:]
		} else {
			out[i] = strings.TrimSpace(line)
		}
	}
	return strings.Join(out, "\n")
}
