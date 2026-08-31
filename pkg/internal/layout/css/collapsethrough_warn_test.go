package css

import (
	"fmt"
	"strings"
	"testing"

	gcss "github.com/nathanstitt/omnidoc/pkg/internal/css"
	"github.com/nathanstitt/omnidoc/pkg/internal/layout/cssbox"
)

// sprintfLog formats one engine log line for a capturing logf, newline-terminated so
// several lines stay separable.
func sprintfLog(format string, args ...any) string {
	return strings.TrimSpace(fmt.Sprintf(format, args...)) + "\n"
}

// marginBox builds a static block with explicit top/bottom margins and height.
func marginBox(marginTop, marginBottom float64, h gcss.Length) *cssbox.Box {
	s := posStyle()
	s.Height = h
	s.MarginTop, s.MarginBottom = px(marginTop), px(marginBottom)
	return posBox(s, cssbox.PosStatic)
}

// TestEmptyBlockCollapseThroughWarns pins backlog item E3's first sub-case.
//
// CSS 2.1 §8.3.1: an empty, auto-height, border/padding-free block collapses its own
// top and bottom margins together and through to its siblings. This engine collapses
// adjacent siblings pairwise, so the empty box's margins each collapse with one
// neighbour and the gap DOUBLES. The divergence stays (it needs collapse state carried
// across a split point); what changes is that it is no longer silent.
//
// The doubling is asserted here too, so if the underlying behaviour is ever fixed this
// test fails loudly rather than silently guarding a warning that should have been
// deleted.
func TestEmptyBlockCollapseThroughWarns(t *testing.T) {
	a := marginBox(0, 40, px(20))
	empty := marginBox(40, 40, px(0))
	b := marginBox(40, 0, px(20))

	var log strings.Builder
	eng := New(nil, nil, func(f string, args ...any) { log.WriteString(sprintfLog(f, args...)) })
	frag := eng.layoutTree(t.Context(), blockBox(gcss.ComputedStyle{Display: "block"}, a, empty, b), 200)
	if frag == nil || len(frag.Children) != 3 {
		t.Fatalf("want 3 in-flow children, got %v", frag)
	}

	gap := frag.Children[2].Y - (frag.Children[0].Y + frag.Children[0].H)
	if !approx(gap, 80) {
		t.Fatalf("gap = %vpt, expected the known-wrong 80pt. If this is now 40pt the bug is "+
			"FIXED — delete warnCollapseThrough and this test rather than adjusting the number.", gap)
	}
	if !strings.Contains(log.String(), "do not collapse through") {
		t.Errorf("the doubled gap was not reported; log was:\n%s", log.String())
	}
}

// TestNonEmptyBlockDoesNotWarn: a block with actual content collapses correctly here,
// so it must stay silent.
func TestNonEmptyBlockDoesNotWarn(t *testing.T) {
	a := marginBox(0, 40, px(20))
	middle := marginBox(40, 40, px(30)) // real height — not a collapse-through candidate
	b := marginBox(40, 0, px(20))

	var log strings.Builder
	eng := New(nil, nil, func(f string, args ...any) { log.WriteString(sprintfLog(f, args...)) })
	eng.layoutTree(t.Context(), blockBox(gcss.ComputedStyle{Display: "block"}, a, middle, b), 200)

	if strings.Contains(log.String(), "do not collapse through") {
		t.Errorf("warned about a non-empty block; log was:\n%s", log.String())
	}
}

// TestZeroMarginSpacerDoesNotWarn: an empty block with no margins has nothing to
// collapse, so there is no divergence and nothing to say.
func TestZeroMarginSpacerDoesNotWarn(t *testing.T) {
	a := marginBox(0, 40, px(20))
	spacer := marginBox(0, 0, px(0))
	b := marginBox(40, 0, px(20))

	var log strings.Builder
	eng := New(nil, nil, func(f string, args ...any) { log.WriteString(sprintfLog(f, args...)) })
	eng.layoutTree(t.Context(), blockBox(gcss.ComputedStyle{Display: "block"}, a, spacer, b), 200)

	if strings.Contains(log.String(), "do not collapse through") {
		t.Errorf("warned about a zero-margin spacer; log was:\n%s", log.String())
	}
}

// TestCollapseThroughWarnsOnce: a page full of empty spacer divs warns once.
func TestCollapseThroughWarnsOnce(t *testing.T) {
	kids := []*cssbox.Box{marginBox(0, 40, px(20))}
	for range 15 {
		kids = append(kids, marginBox(40, 40, px(0)), marginBox(0, 0, px(10)))
	}

	var log strings.Builder
	eng := New(nil, nil, func(f string, args ...any) { log.WriteString(sprintfLog(f, args...)) })
	eng.layoutTree(t.Context(), blockBox(gcss.ComputedStyle{Display: "block"}, kids...), 200)

	if n := strings.Count(log.String(), "do not collapse through"); n != 1 {
		t.Errorf("warned %d times for 15 empty blocks, want exactly 1", n)
	}
}
