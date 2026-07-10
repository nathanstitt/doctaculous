package boxwalk

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// InlineState is the inherited inline styling in force at a point in the walk.
type InlineState struct {
	Bold   bool
	Italic bool
	Strike bool
	Code   bool
	Href   string // non-empty inside a link
}

// StyledRun is a run of text with its resolved inline styling. Literal, when
// non-empty, is pre-formatted output (e.g. an image tag) emitted verbatim without
// escaping or emphasis wrapping; a literal run is never merged with a neighbor.
type StyledRun struct {
	Text    string
	Literal string
	InlineState
}

// CollectRuns walks b's inline subtree, threading the styling state, appending a
// StyledRun for each text leaf. Bold/italic come from the computed style (so DOCX
// bold, which has no <strong> tag, is honored) as well as the SemTag; code and href
// come from SemTag/Href. image renders an <img> replaced box to the writer's
// format (its result is a Literal run); image must be non-nil.
func CollectRuns(b *cssbox.Box, st InlineState, image func(*cssbox.ReplacedContent) string, out *[]StyledRun) {
	if b.Style.Bold {
		st.Bold = true
	}
	if b.Style.Italic {
		st.Italic = true
	}
	if b.Style.TextDecorationLine == "line-through" {
		st.Strike = true
	}
	switch b.SemTag {
	case "strong":
		st.Bold = true
	case "em":
		st.Italic = true
	case "s":
		st.Strike = true
	case "code":
		st.Code = true
	case "a":
		if b.Href != "" {
			st.Href = b.Href
		}
	}
	if b.Kind == cssbox.BoxText {
		if b.Text != "" {
			*out = append(*out, StyledRun{Text: b.Text, InlineState: st})
		}
		return
	}
	if b.Kind == cssbox.BoxReplaced && b.Replaced != nil && b.Replaced.Tag == "img" {
		*out = append(*out, StyledRun{Literal: image(b.Replaced), InlineState: st})
		return
	}
	for _, c := range b.Children {
		CollectRuns(c, st, image, out)
	}
}

// Coalesce merges adjacent runs with identical styling so a single element split
// into multiple text leaves emits one marker/tag pair.
func Coalesce(runs []StyledRun) []StyledRun {
	var out []StyledRun
	for _, r := range runs {
		if n := len(out); n > 0 && r.Literal == "" && out[n-1].Literal == "" && out[n-1].InlineState == r.InlineState {
			out[n-1].Text += r.Text
			continue
		}
		out = append(out, r)
	}
	return out
}

// CollapseSpaces collapses runs of whitespace to a single space and trims the
// ends, the normal-flow whitespace model for inline content.
func CollapseSpaces(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// RawText concatenates every text leaf under b verbatim (no whitespace
// collapsing), used for <pre> content where whitespace is significant.
func RawText(b *cssbox.Box) string {
	var sb strings.Builder
	var walk func(*cssbox.Box)
	walk = func(n *cssbox.Box) {
		if n.Kind == cssbox.BoxText {
			sb.WriteString(n.Text)
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(b)
	return sb.String()
}
