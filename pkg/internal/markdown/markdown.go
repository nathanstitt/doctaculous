// Package markdown is the Markdown input frontend: it converts CommonMark +
// GFM source into a complete HTML document that the HTML pipeline (parse → box
// generation → CSS layout) renders. Reusing the HTML pipeline — rather than
// lowering a Markdown AST to boxes directly — inherits whitespace collapsing,
// anonymous-box fixups, list markers, tables, pagination, and the semantic
// annotations (SemTag/HeadingLvl/Href) the structure writers need, so a
// Markdown document round-trips back to Markdown.
package markdown

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	ghtml "github.com/yuin/goldmark/renderer/html"
)

// maxBlockNesting bounds how many block-quote or list markers may open on a
// single line before the source is refused.
//
// The bound exists because the cost is in the dependency. goldmark's block
// parser is quadratic in the nesting opened on one line: measured, a line of N
// "- " markers takes 0.49s at N=12,500, 2.6s at 25,000 and 10.8s at 50,000 --
// four times the work for twice the input -- and 200,000 markers (a 400 KB
// file) does not finish in a minute. Block quotes behave the same way, roughly
// 3x cheaper.
//
// It is DEPTH that costs, not size: 50,000 list items spread over 50,000 lines
// parse in 79ms, while the same 50,000 markers on ONE line take 8.6s -- a
// hundred times slower for half the bytes. So the bound is per line rather than
// a document-size limit, which would reject large legitimate documents while
// still admitting the small hostile one.
//
// 1024 is far past real Markdown (even a pathologically nested list runs to
// single digits) and keeps the worst case in single-digit milliseconds.
const maxBlockNesting = 1024

// ErrTooDeeplyNested is returned by ToHTML when a line opens more than
// [maxBlockNesting] block-level markers. It is a distinct sentinel because the
// source is syntactically fine -- it is refused for cost, not for being invalid.
var ErrTooDeeplyNested = errors.New("markdown: block nesting too deep")

// ToHTML converts CommonMark + GFM source (tables, strikethrough, task lists,
// autolinks) into a complete, self-contained HTML document with the embedded
// default stylesheet, ready for the HTML pipeline.
//
// Raw inline/block HTML in the source is passed through (goldmark's "unsafe"
// mode): it is core CommonMark, and the only consumer is this module's own
// layout engine, which executes nothing.
//
// Source whose per-line block nesting exceeds [maxBlockNesting] is refused with
// an error wrapping [ErrTooDeeplyNested]; see that constant for why.
func ToHTML(src []byte) ([]byte, error) {
	if line, depth, ok := deepestBlockNesting(src); !ok {
		return nil, fmt.Errorf("%w: line %d opens %d markers, over the %d limit",
			ErrTooDeeplyNested, line, depth, maxBlockNesting)
	}
	return renderDocument(src)
}

// renderDocument is ToHTML's conversion half, split out so the nesting check
// above reads as a precondition rather than being tangled with the rendering.
func renderDocument(src []byte) ([]byte, error) {
	// A fresh converter per call: goldmark does not document goroutine safety,
	// and construction is trivial next to a document layout.
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(ghtml.WithUnsafe()),
	)
	var buf bytes.Buffer
	buf.WriteString("<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n<style>\n")
	buf.WriteString(DefaultCSS)
	buf.WriteString("</style>\n</head>\n<body>\n")
	if err := md.Convert(src, &buf); err != nil {
		return nil, fmt.Errorf("markdown: convert: %w", err)
	}
	buf.WriteString("</body>\n</html>\n")
	return buf.Bytes(), nil
}

// deepestBlockNesting scans src for the line that opens the most block-level
// containers, returning that line's 1-based number, its marker count, and
// whether it is within maxBlockNesting.
//
// Only the leading run of a line is examined, because that is what goldmark's
// block parser descends through: "> > > x" opens three quotes, but "x > > >" is
// a paragraph containing greater-than signs. The markers counted are the
// container-opening ones -- block quote (>), bullet list (-, *, +) and ordered
// list (N. or N)) -- since those are the constructs whose nesting is quadratic.
//
// Fenced code blocks are skipped, because their contents are literal text: a
// README showing example Markdown inside ``` opens no containers, and measured,
// 50,000 markers inside a fence cost 238 microseconds against 10.8 seconds for
// the same markers outside one. Counting them would refuse a document that is
// both legitimate and cheap.
//
// The scan is linear in the input and allocation-free, so it is affordable on
// every document rather than only on suspicious ones.
func deepestBlockNesting(src []byte) (line, depth int, ok bool) {
	lineNo, worst, worstLine := 1, 0, 1
	fence := "" // the open fence's delimiter run, or "" when not inside one
	for i := 0; i < len(src); lineNo++ {
		end := bytes.IndexByte(src[i:], '\n')
		if end < 0 {
			end = len(src)
		} else {
			end += i
		}
		lineBytes := src[i:end]

		if fence != "" {
			// Inside a fence: only a matching closing delimiter matters.
			if f := fenceDelimiter(lineBytes); f != "" && strings.HasPrefix(f, fence) {
				fence = ""
			}
			i = end + 1
			continue
		}
		if f := fenceDelimiter(lineBytes); f != "" {
			fence = f
			i = end + 1
			continue
		}

		if n := leadingBlockMarkers(lineBytes); n > worst {
			worst, worstLine = n, lineNo
			if worst > maxBlockNesting {
				return worstLine, worst, false
			}
		}
		i = end + 1
	}
	return worstLine, worst, true
}

// leadingBlockMarkers counts the container-opening markers at the start of one
// line. Indentation between markers is skipped the way the block parser skips
// it; anything else ends the run.
func leadingBlockMarkers(lineBytes []byte) int {
	n, i := 0, 0
	for i < len(lineBytes) {
		// Leading space/tab belongs to the marker run.
		for i < len(lineBytes) && (lineBytes[i] == ' ' || lineBytes[i] == '\t') {
			i++
		}
		if i >= len(lineBytes) {
			break
		}
		switch c := lineBytes[i]; {
		case c == '>':
			n++
			i++
		case c == '-' || c == '*' || c == '+':
			// A bullet marker must be followed by whitespace; "---" is a thematic
			// break and "**bold**" is inline emphasis, neither of which nests.
			if i+1 >= len(lineBytes) || (lineBytes[i+1] != ' ' && lineBytes[i+1] != '\t') {
				return n
			}
			n++
			i++
		case c >= '0' && c <= '9':
			// An ordered marker is digits followed by "." or ")" then whitespace.
			j := i
			for j < len(lineBytes) && lineBytes[j] >= '0' && lineBytes[j] <= '9' {
				j++
			}
			if j >= len(lineBytes) || (lineBytes[j] != '.' && lineBytes[j] != ')') {
				return n
			}
			j++
			if j >= len(lineBytes) || (lineBytes[j] != ' ' && lineBytes[j] != '\t') {
				return n
			}
			n++
			i = j
		default:
			return n
		}
	}
	return n
}

// fenceDelimiter returns the code-fence delimiter run opening or closing a line
// (three or more backticks or tildes, after up to three spaces of indent), or ""
// when the line is not a fence marker.
//
// A closing fence must be at least as long as the opening one and use the same
// character, which is why the run itself is returned rather than a bool.
func fenceDelimiter(lineBytes []byte) string {
	i := 0
	for i < len(lineBytes) && i < 3 && lineBytes[i] == ' ' {
		i++
	}
	if i >= len(lineBytes) {
		return ""
	}
	c := lineBytes[i]
	if c != '`' && c != '~' {
		return ""
	}
	j := i
	for j < len(lineBytes) && lineBytes[j] == c {
		j++
	}
	if j-i < 3 {
		return ""
	}
	return string(lineBytes[i:j])
}
