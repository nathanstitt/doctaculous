package css

import (
	"context"
	"strings"
	"testing"

	gcss "github.com/nathanstitt/omnidoc/pkg/internal/css"
	"github.com/nathanstitt/omnidoc/pkg/internal/layout/cssbox"
)

// inlineRelBox builds an inline box with the given relative offsets, wrapping a text
// leaf — the C6 shape: a non-replaced inline whose glyphs flatten into the parent's
// line, so there is no fragment to carry a RelOffset.
func inlineRelBox(top, left gcss.Length) *cssbox.Box {
	s := posStyle()
	s.Display = "inline"
	s.Top, s.Left = top, left
	b := &cssbox.Box{
		Kind:     cssbox.BoxInline,
		Display:  cssbox.DisplayInline,
		Style:    s,
		Children: []*cssbox.Box{{Kind: cssbox.BoxText, Text: "shifted", Style: s}},
	}
	b.Position = cssbox.PosRelative
	return b
}

// inlineHost wraps inline children in a block that establishes an INLINE formatting
// context — the shape real box generation produces for a <p> whose children are all
// inline-level, and the only shape that reaches gatherInlineRuns. blockBox's BlockFC
// would instead send each child down the block path, where relative offsets DO work.
func inlineHost(kids ...*cssbox.Box) *cssbox.Box {
	b := blockBox(gcss.ComputedStyle{Display: "block"}, kids...)
	b.Formatting = cssbox.InlineFC
	return b
}

// layoutWithLog lays out root and returns everything the engine logged.
func layoutWithLog(t *testing.T, root *cssbox.Box) string {
	t.Helper()
	var log strings.Builder
	eng := New(nil, nil, func(f string, a ...any) { log.WriteString(sprintfLog(f, a...)) })
	if eng.layoutTree(context.Background(), root, 200) == nil {
		t.Fatal("nil root fragment")
	}
	return log.String()
}

// TestRelativeInlineBoxWarns pins backlog item C6's honest degradation.
//
// position:relative on a non-replaced inline box moves nothing — the inline generates
// no fragment, so there is nowhere to hang the offset. That is a structural limitation
// and stays one; what must not happen is it being SILENT. Verified by hand before this
// landed: the PDF for a <span style="position:relative;left:40px;top:15px"> was
// byte-identical to the same span without the declaration, with nothing in the log.
func TestRelativeInlineBoxWarns(t *testing.T) {
	root := inlineHost(inlineRelBox(px(15), px(20)))
	got := layoutWithLog(t, root)

	if !strings.Contains(got, "position:relative on a non-replaced inline box") {
		t.Errorf("no degradation warning for a relatively-positioned inline box; log was:\n%s", got)
	}
	// The resolved offset is quoted so an author can tell which declaration was dropped.
	if !strings.Contains(got, "20pt,15pt") {
		t.Errorf("warning does not quote the ignored offset (want 20pt,15pt); log was:\n%s", got)
	}
}

// TestRelativeInlineBoxWarnsOnce guards the noise budget: a paragraph with many such
// spans must warn once, not once per span.
func TestRelativeInlineBoxWarnsOnce(t *testing.T) {
	kids := make([]*cssbox.Box, 0, 20)
	for range 20 {
		kids = append(kids, inlineRelBox(px(15), px(20)))
	}
	got := layoutWithLog(t, inlineHost(kids...))

	if n := strings.Count(got, "non-replaced inline box"); n != 1 {
		t.Errorf("warned %d times for 20 boxes, want exactly 1", n)
	}
}

// TestRelativeInlineNoOffsetSilent covers the idiom that must NOT warn: `position:
// relative` with no offsets, used only to establish a containing block for an
// absolutely-positioned descendant. That works correctly here, so a warning would be
// actively misleading.
func TestRelativeInlineNoOffsetSilent(t *testing.T) {
	root := inlineHost(inlineRelBox(autoLen(), autoLen()))
	if got := layoutWithLog(t, root); strings.Contains(got, "non-replaced inline box") {
		t.Errorf("warned about a zero-offset position:relative (a working idiom); log was:\n%s", got)
	}
}

// TestRelativeInlineZeroOffsetSilent is the same guard for an explicit `left:0;top:0`
// rather than an absent offset — also a no-op with nothing to report.
func TestRelativeInlineZeroOffsetSilent(t *testing.T) {
	root := inlineHost(inlineRelBox(px(0), px(0)))
	if got := layoutWithLog(t, root); strings.Contains(got, "non-replaced inline box") {
		t.Errorf("warned about an explicit zero offset; log was:\n%s", got)
	}
}

// TestRelativeBlockBoxDoesNotWarn pins the boundary: a BLOCK-level relative box is
// implemented exactly (see TestRelativeBoxInFlowUnchangedButFlagged), so it must stay
// silent — the warning is specific to the inline case.
func TestRelativeBlockBoxDoesNotWarn(t *testing.T) {
	s := posStyle()
	s.Height, s.Top, s.Left = px(30), px(10), px(20)
	root := blockBox(gcss.ComputedStyle{Display: "block"}, posBox(s, cssbox.PosRelative))
	if got := layoutWithLog(t, root); strings.Contains(got, "non-replaced inline box") {
		t.Errorf("warned about a block-level relative box, which is implemented; log was:\n%s", got)
	}
}
