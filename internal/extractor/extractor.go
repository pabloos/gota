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

// marker is the line that introduces a per-handler gota block. It may
// appear with or without a trailing value on the same line, e.g. "gota:"
// on its own line followed by an indented YAML mapping.
const marker = "gota:"

// docMarker introduces a document-level block: OpenAPI fields that belong
// to the whole document (info, servers, security, tags, securitySchemes)
// rather than any single handler. It can live in any doc comment in the
// analyzed source. Because it shares the "gota:" prefix, Extract must skip
// it so a "gota:doc:" line is never mistaken for a per-handler block.
const docMarker = "gota:doc:"

// Extract looks for a "gota:" block in doc and parses the YAML that follows
// it into an Operation fragment. It returns (nil, false) if doc contains no
// gota block. A "gota:doc:" block is not a per-handler operation and is
// ignored here (see ExtractDoc).
func Extract(doc *ast.CommentGroup) (*model.Operation, bool, error) {
	block, found, err := findBlock(doc, marker, docMarker)
	if err != nil || !found {
		return nil, found, err
	}
	var op model.Operation
	if err := yaml.Unmarshal([]byte(block), &op); err != nil {
		return nil, false, fmt.Errorf("extractor: invalid YAML in gota: block: %w", err)
	}
	return &op, true, nil
}

// ExtractDoc looks for a "gota:doc:" block in doc and parses the YAML that
// follows it into a document-level fragment. It returns (nil, false) if doc
// contains no such block.
func ExtractDoc(doc *ast.CommentGroup) (*model.DocumentMeta, bool, error) {
	block, found, err := findBlock(doc, docMarker)
	if err != nil || !found {
		return nil, found, err
	}
	var meta model.DocumentMeta
	if err := yaml.Unmarshal([]byte(block), &meta); err != nil {
		return nil, false, fmt.Errorf("extractor: invalid YAML in gota:doc: block: %w", err)
	}
	return &meta, true, nil
}

// findBlock finds the want marker in doc's comment lines and returns the
// dedented YAML block that follows it. Lines whose trimmed text begins with
// any of the exclude markers are skipped, so a more specific marker (e.g.
// "gota:doc:") is never captured by a less specific one ("gota:").
func findBlock(doc *ast.CommentGroup, want string, exclude ...string) (string, bool, error) {
	if doc == nil {
		return "", false, nil
	}
	lines := commentLines(doc)
	marker, end, firstLineRest, found := locateBlock(lines, want, exclude)
	if !found {
		return "", false, nil
	}

	var yamlLines []string
	if firstLineRest != "" {
		yamlLines = append(yamlLines, firstLineRest)
	}
	yamlLines = append(yamlLines, lines[marker+1:end]...)

	block := dedent(yamlLines)
	if strings.TrimSpace(block) == "" {
		return "", false, fmt.Errorf("extractor: empty %s block", want)
	}
	return block, true, nil
}

// locateBlock finds the want-marker block among lines and returns the index
// of the marker line, the exclusive end index of the block body, any inline
// remainder on the marker line, and whether a block was found. Lines
// [marker+1, end) are the block body; everything outside [marker, end) is
// ordinary prose. The body is bounded so it doesn't swallow the prose that
// commonly follows a block (a gota:doc: block at the top of a package
// comment is the prime case): after skipping the blank line gofmt inserts
// right after the marker, the body runs until the first blank line, or the
// first line flush with the left margin once the block has itself been
// indented — either being where prose resumes.
func locateBlock(lines []string, want string, exclude []string) (marker, end int, firstLineRest string, found bool) {
	marker = -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isExcluded(trimmed, exclude) {
			continue
		}
		if trimmed == want {
			marker = i
			break
		}
		if rest, ok := strings.CutPrefix(trimmed, want); ok && strings.TrimSpace(rest) != "" {
			// Inline form: "gota: {summary: ...}" begins on the marker line.
			marker, firstLineRest = i, rest
			break
		}
	}
	if marker == -1 {
		return 0, 0, "", false
	}

	end = len(lines)
	seenContent := firstLineRest != ""
	indented := false
	for j := marker + 1; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "" {
			if seenContent {
				end = j
				break
			}
			continue
		}
		indent := len(lines[j]) - len(strings.TrimLeft(lines[j], " \t"))
		if seenContent && indented && indent == 0 {
			end = j
			break
		}
		seenContent = true
		if indent > 0 {
			indented = true
		}
	}
	return marker, end, firstLineRest, true
}

// Prose returns the human-readable text of doc with any "gota:" or
// "gota:doc:" block removed — the handler's own documentation, which gota
// uses as the operation description when the comment declares none.
// Paragraph breaks are preserved; leading and trailing blank lines are
// trimmed. It returns "" when nothing but a block (or nothing at all)
// remains.
func Prose(doc *ast.CommentGroup) string {
	if doc == nil {
		return ""
	}
	lines := commentLines(doc)

	// Drop each block region (marker line through its bounded body). Remove
	// gota:doc: first so the shorter "gota:" marker can't match a gota:doc:
	// line; loop until none remain, though in practice there is at most one
	// of each.
	for _, spec := range []struct {
		want    string
		exclude []string
	}{
		{docMarker, nil},
		{marker, []string{docMarker}},
	} {
		for {
			m, end, _, found := locateBlock(lines, spec.want, spec.exclude)
			if !found {
				break
			}
			lines = append(lines[:m:m], lines[end:]...)
		}
	}

	// Removing a block from the middle can leave two blank lines where its
	// surrounding blanks met; collapse any such run to a single paragraph
	// break so the description reads cleanly.
	var kept []string
	prevBlank := false
	for _, line := range lines {
		blank := strings.TrimSpace(line) == ""
		if blank && prevBlank {
			continue
		}
		kept = append(kept, line)
		prevBlank = blank
	}

	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// isExcluded reports whether trimmed begins with any marker in exclude.
func isExcluded(trimmed string, exclude []string) bool {
	for _, e := range exclude {
		if strings.HasPrefix(trimmed, e) {
			return true
		}
	}
	return false
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
