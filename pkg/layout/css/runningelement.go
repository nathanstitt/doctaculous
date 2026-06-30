package css

import (
	"context"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// captureRunningElement lays a running element's box subtree out as a self-contained
// block at width w (the margin-box content width), returning its fragment in its own
// local space (the ICB origin, ~0,0). Returns nil if layout produces nothing.
//
// layoutTree already accepts a bare block root and lays it out as a block within a
// fresh initial containing block of width w, establishing its own BFC — exactly what a
// running element needs — so no html>body wrapper is synthesized.
func (e *Engine) captureRunningElement(ctx context.Context, box *cssbox.Box, w float64) *Fragment {
	if box == nil {
		return nil
	}
	return e.layoutTree(ctx, box, w)
}

// placeRunningElement paints an ALREADY-CAPTURED running-element fragment into margin
// rect r: it translates the fragment so its top-left sits at (r.x, r.y), then flattens
// it via the same AppendItems painter the page content uses, so the running element
// keeps all of its styling (borders, backgrounds, nested layout).
//
// NOTE: translateFragment and AppendItems MUTATE frag (translate moves X/Y in place). A
// captured fragment shared across pages must therefore not be passed here twice — use
// placeRunningElementBox, which re-captures a fresh fragment per call. This entry is for
// a fragment owned by the caller (e.g. a freshly captured one).
func (e *Engine) placeRunningElement(items []layout.Item, frag *Fragment, r marginRect) []layout.Item {
	if frag == nil {
		return items
	}
	translateFragment(frag, r.x-frag.X, r.y-frag.Y)
	return frag.AppendItems(items)
}

// placeRunningElementBox captures the running element box fresh (at the margin-box
// content width r.w) and places it into rect r. Re-capturing per call — rather than
// deep-cloning a shared fragment — keeps the captured fragment from being corrupted
// across pages (translateFragment/AppendItems mutate), and layout of a small header is
// idempotent and cheap. This is the per-page-safe entry the margin-box loop uses.
func (e *Engine) placeRunningElementBox(ctx context.Context, items []layout.Item, box *cssbox.Box, r marginRect) []layout.Item {
	frag := e.captureRunningElement(ctx, box, r.w)
	return e.placeRunningElement(items, frag, r)
}
