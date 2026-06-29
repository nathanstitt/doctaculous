package css

import (
	"context"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// LayoutPaged lays out root at viewportW points and fragments the result into
// pageH-tall pages, returning one-or-more pages. pageH <= 0 means no pagination:
// it returns exactly what Layout returns (a single tall page sized to the content
// height) — the byte-identical path. Otherwise it builds the same fragment tree as
// Layout (via the shared layoutTree), then splits the document's top-level in-flow
// block fragments into pageH-tall pages, breaking between block boundaries and at
// forced page breaks (break-before / break-after: page|always and the legacy
// page-break-* aliases).
//
// It never panics on malformed input: a recover at the page boundary returns a
// single empty pageH-tall page. It degrades gracefully — a single block taller than
// a page overflows its page rather than splitting (logged once per over-tall block).
// position:relative blocks paginate normally (a top-level one is routed to its bucket's
// page; one nested under a static wrapper rides its nearest top-level ancestor's page),
// and a relative block's abs descendants and its own border/clip follow it (shiftFragment
// moves them). Only absolute/fixed boxes whose containing block is the page (not a
// paginated relative ancestor) are undistributed and ride the first page — a documented
// deferral. The html/body wrapper's border/background is fragmented per page.
func (e *Engine) LayoutPaged(ctx context.Context, root *cssbox.Box, viewportW, pageH float64) (pages *layout.Pages, err error) {
	if pageH <= 0 {
		return e.Layout(ctx, root, viewportW)
	}
	defer func() {
		if r := recover(); r != nil {
			e.logf("css layout: recovered from panic: %v", r)
			pages = &layout.Pages{Pages: []layout.Page{{WidthPt: viewportW, HeightPt: pageH}}}
			err = nil
		}
	}()

	frag := e.layoutTree(ctx, root, viewportW)
	if frag == nil {
		return &layout.Pages{Pages: []layout.Page{{WidthPt: viewportW, HeightPt: pageH}}}, nil
	}
	return e.paginate(frag, viewportW, pageH), nil
}

// paginate fragments the laid-out root fragment into pageH-tall pages, breaking only
// between the document's top-level in-flow block fragments (the body fragment's
// Children). Floats and positioned descendants are lifted out of Children (into the
// root's Floats / Positioned), so the walk never sees them; they flatten with the
// first page's root wrapper.
func (e *Engine) paginate(root *Fragment, viewportW, pageH float64) *layout.Pages {
	body := bodyFragment(root)
	if body == nil || len(body.Children) == 0 {
		// No top-level blocks: the whole document is a single page, pageH tall.
		return &layout.Pages{Pages: []layout.Page{root.Page(viewportW, pageH)}}
	}

	// This bounded slice breaks only BETWEEN top-level body blocks. A forced
	// break-before/after on a NESTED (non-top-level) block cannot drive a mid-block
	// split here, but a break on content at the START / END of a top-level block is
	// propagated to that block (see effectiveBreaks); a genuinely mid-block forced
	// break is still dropped — warned once rather than silently (handling it properly
	// needs mid-box fragmentation, a follow-up).
	warnMidBlockForcedBreaks(body.Children, e.logf)

	// The containing-block width the top-level blocks' own margins resolve against is
	// the body content width (border box minus the body's own border + padding). It is
	// needed to recover a block's used top margin when retaining it at an unforced break.
	cbWidth := contentWidth(body, viewportW)
	buckets := bucketBlocks(body.Children, pageH, cbWidth, e.logf)

	// Pull the FIRST page's top up to the outermost wrapper border-box top when the
	// html/body wrapper has a border or background ABOVE the first block (its top border +
	// padding). bucketBlocks sets page 0's top to the first block's Y, which would clip the
	// body's own top border/background off the top of page 0 (the wrapper sits above the
	// first block). Including the wrapper box on page 0 keeps the body's TOP border on
	// page 0 and lets the first block sit below it. Only page 0 is adjusted — the wrapper
	// has no "top" on later pages (its content continues), so their tops stay at the first
	// block of that page. A wrapper with no visible decoration leaves top unchanged
	// (decorationTop returns the block Y), so the un-bordered common case — including the
	// existing paginate golden — is byte-identical.
	if len(buckets) > 0 {
		top := buckets[0].top
		if t := wrapperDecorationTop(root, top); t < top {
			top = t
		}
		if t := wrapperDecorationTop(body, top); t < top {
			top = t
		}
		buckets[0].top = top
	}

	// Distribute the root's positioned layer across pages. A top-level position:relative
	// block is in flow (so it is in body.Children and gets bucketed) but is ALSO lifted
	// into the root's positioned layer for painting — the SAME *Fragment pointer appears
	// in both root.Positioned and a bucket; it is routed to its block's page and shifted
	// for free as a bucket block. A page-CB absolute box is routed to the page whose band
	// contains its top and shifted into that page's local frame here; a position:fixed box
	// is cloned onto every page (each clone shifted), so it repeats. (An abs box whose CB
	// is a positioned ancestor is not in root.Positioned — it follows that ancestor.)
	perPagePos := splitPositionedByPage(root, buckets)

	pages := make([]layout.Page, 0, len(buckets))
	for i, bk := range buckets {
		// Shallow-clone the root/body wrapper so each page can carry its own block
		// list without mutating the shared tree. The block fragments themselves are
		// the original pointers — each belongs to exactly one page, so shifting them
		// in place is safe (no cross-page aliasing).
		pageRoot := *root
		pageBody := *body
		pageBody.Children = bk.blocks
		// Assign this page's share of the positioned layer (its relative blocks, the
		// page-CB abs boxes whose band is this page, and a clone of every fixed box). The
		// out-of-flow float layer is not distributed and rides the first page only;
		// null it on later pages so a top-level float is not duplicated. The body owns
		// no positioned/float layer of its own (the root does), so its copies are
		// cleared.
		pageRoot.Positioned = perPagePos[i].frags
		pageRoot.PositionedInfo = perPagePos[i].infos
		pageBody.Positioned = nil
		pageBody.PositionedInfo = nil
		pageBody.Floats = nil
		if i > 0 {
			pageRoot.Floats = nil
		}
		// Fragment the html/body wrapper's own border + background across pages. The
		// wrapper clones are NOT in bk.blocks, so shiftFragments below does not move them;
		// position each wrapper's OWN box (not its children) into the same block-shifted
		// frame by subtracting bk.top. This puts the wrapper's full-document border box at
		// local Y body.Y-bk.top..body.Y+body.H-bk.top, so the page bitmap naturally
		// fragments it: the TOP edge (at the box top) is on-page only where the box top
		// falls (page 0), the BOTTOM edge only on the last page, and the LEFT/RIGHT side
		// edges span every page's band (clipped to the bitmap). Without this the wrapper
		// border painted at full-document geometry on every page — a spurious top edge at
		// Y 0 and full-height sides on pages ≥1. (Backgroundless, borderless wrappers — the
		// common case, incl. the existing paginate golden — emit nothing here, so this is
		// byte-identical for them.)
		shiftFragmentSelf(&pageBody, -bk.top)
		shiftFragmentSelf(&pageRoot, -bk.top)
		// Replace the body entry (root.Children[last]) with the body clone, preserving
		// any non-body children.
		children := make([]*Fragment, len(root.Children))
		copy(children, root.Children)
		children[len(children)-1] = &pageBody
		pageRoot.Children = children

		// Translate this page's blocks up so the page's first content sits at local
		// Y 0. Must run BEFORE Page(...) flattens (the flatten reads the shifted Ys).
		// Because a routed relative-block entry is the SAME pointer as a bucket block,
		// this shift also moves that entry into its page's local space — no separate
		// positioned shift is needed.
		shiftFragments(bk.blocks, -bk.top)

		pages = append(pages, pageRoot.Page(viewportW, pageH))
	}
	return &layout.Pages{Pages: pages}
}

// pageBucket is one page's worth of top-level block fragments plus the page-space Y
// (top) where the page's content begins; the blocks are shifted up by top so the
// page's first content lands at local Y 0.
type pageBucket struct {
	top    float64
	blocks []*Fragment
}

// pagePositioned is one page's slice of the positioned layer: the fragments painted
// in that page's positioned band and the parallel PositionedInfo (same length).
type pagePositioned struct {
	frags []*Fragment
	infos []PositionedInfo
}

// splitPositionedByPage partitions root.Positioned (and its parallel PositionedInfo)
// across the buckets, distributing each kind of positioned entry to the page(s) it
// belongs on:
//
//   - A position:relative block — in flow (so it is bucketed) but painted from the
//     positioned layer — is routed to the page its in-flow position landed on, so it
//     paginates normally: directly when its *Fragment pointer is itself a top-level bucket
//     block, or via its nearest TOP-LEVEL ANCESTOR block when it is a relative block NESTED
//     below a static wrapper (its pointer bubbled to root.Positioned but its in-flow box
//     lives in that ancestor's subtree, shifted onto the ancestor's page). It is NOT shifted
//     here — the entry IS the in-flow fragment, already moved by the caller's shiftFragments.
//   - A page-CB position:absolute box is routed to the page whose band contains its
//     border-box top (pageForY) and SHIFTED into that page's local frame here (it is out of
//     flow — not a bucket block — so this is its only shift). A box above page 0 / below the
//     last band clamps to the first / last page (never dropped).
//   - A page-CB position:fixed box repeats on EVERY page: a deep clone per page, each shifted
//     into that page's local frame, so it paints at the same on-page coordinates everywhere.
//     (A clone is required because shiftFragment mutates in place and one *Fragment cannot
//     carry N page origins.)
//
// (An abs box whose CB is a positioned ancestor is in that ancestor's .Positioned, not
// root.Positioned, so it is not seen here — it follows the ancestor.) The result has one
// entry per bucket; each page's parallel info slice stays aligned with its frags.
func splitPositionedByPage(root *Fragment, buckets []pageBucket) []pagePositioned {
	out := make([]pagePositioned, len(buckets))
	if root == nil || len(root.Positioned) == 0 || len(buckets) == 0 {
		return out
	}
	// Map each top-level block — AND every positioned (relative) descendant in its in-flow
	// subtree — to the block's bucket index. Walking the subtree (Children only: the
	// in-flow chain) catches a relative block nested under static wrappers whose pointer
	// bubbled to root.Positioned. Abs/fixed entries are out of flow and so are NOT in any
	// block's Children subtree → they are not in this map and are distributed by kind below.
	blockPage := make(map[*Fragment]int)
	var mark func(f *Fragment, page int)
	mark = func(f *Fragment, page int) {
		blockPage[f] = page
		for _, c := range f.Children {
			mark(c, page)
		}
	}
	for bi := range buckets {
		for _, b := range buckets[bi].blocks {
			mark(b, bi)
		}
	}
	assign := func(page int, frag *Fragment, info PositionedInfo) {
		out[page].frags = append(out[page].frags, frag)
		out[page].infos = append(out[page].infos, info)
	}
	for i, frag := range root.Positioned {
		// PositionedInfo may be shorter than Positioned (a nil/short slice reads as the
		// zero value); guard the parallel index.
		var info PositionedInfo
		if i < len(root.PositionedInfo) {
			info = root.PositionedInfo[i]
		}
		if p, ok := blockPage[frag]; ok {
			// A relative block (or a relative descendant of one) → its in-flow page; the
			// caller's shiftFragments moves it (it is a bucket block / in a bucket subtree).
			assign(p, frag, info)
			continue
		}
		if isFixedFragment(frag) {
			// position:fixed → repeat on every page. A fixed box is positioned against the
			// VIEWPORT, so resolveAbsolute placed it against the page rect {y:0,h:pageH}:
			// its frag.Y is already its offset from the page top (e.g. top:10px → Y 10),
			// which is the correct page-LOCAL Y on EVERY page. So it is assigned to every
			// page WITHOUT a per-page shift — the same read-only fragment is shared (the
			// flatten only reads it; no page mutates it, since the abs shift below touches a
			// different pointer and the block shift touches bucket blocks). (A bottom-anchored
			// fixed box is positioned against the full single-tall height — a known
			// limitation; per-page bottom anchoring is a follow-up.)
			for pi := range buckets {
				assign(pi, frag, info)
			}
			continue
		}
		// page-CB position:absolute → the page whose band holds its top, shifted into that
		// page's local frame (it is out of flow, not a bucket block, so this is its only
		// shift).
		page := pageForY(frag.Y, buckets)
		shiftFragment(frag, -buckets[page].top)
		assign(page, frag, info)
	}
	return out
}

// pageForY returns the index of the bucket whose vertical band contains page-space Y y:
// the last bucket whose top is <= y (bands are [buckets[i].top, buckets[i+1].top), the last
// extending to +inf). A y above the first band clamps to page 0; below the last clamps to
// the last page. buckets must be non-empty and ordered by ascending top (bucketBlocks
// guarantees both).
func pageForY(y float64, buckets []pageBucket) int {
	page := 0
	for i := range buckets {
		if buckets[i].top <= y {
			page = i
		} else {
			break
		}
	}
	return page
}

// isFixedFragment reports whether a fragment came from a position:fixed box (its CB is the
// viewport, so it repeats on every page). A nil fragment or nil Box reads as not-fixed.
func isFixedFragment(f *Fragment) bool {
	return f.Box != nil && f.Box.Position == cssbox.PosFixed
}

// bucketBlocks assigns top-level in-flow block fragments to pageH-tall pages,
// breaking between blocks on overflow and at forced page breaks. It is pure (no
// engine receiver) so it can be unit-tested in isolation; logf records the over-tall
// degradation. cbWidth is the blocks' containing-block (body content) width, used to
// recover a block's used top margin when retaining it at an unforced break. It always
// returns at least one bucket (an empty bucket{top:0} for empty input), and never
// emits a leading or trailing empty bucket for a forced break on the first / last block.
//
// A top-level block's effective forced break (effectiveBreaks) folds in a forced
// break-before on content at the START of the block and a forced break-after on
// content at its END (a nested break propagated to the top-level ancestor), so the
// common `.page-break { break-before: page }` on a nested element is honored.
func bucketBlocks(blocks []*Fragment, pageH, cbWidth float64, logf func(string, ...any)) []pageBucket {
	if len(blocks) == 0 {
		return []pageBucket{{top: 0}}
	}
	var buckets []pageBucket
	var cur pageBucket
	for _, b := range blocks {
		forcedBefore, forcedAfter := effectiveBreaks(b)
		overflow := len(cur.blocks) > 0 && (b.Y+b.H)-cur.top > pageH
		// Close the current page only if it already has content: a forced-before on
		// the first block (cur empty) is a no-op, not a leading empty page.
		if (forcedBefore || overflow) && len(cur.blocks) > 0 {
			buckets = append(buckets, cur)
			cur = pageBucket{}
		}
		if len(cur.blocks) == 0 {
			// The page top determines where the page's first block lands once shifted to
			// local space. Reading b.Y (its BORDER-box top), not a provisional
			// previous-block bottom, keeps the top correct even when a margin gaps the
			// block below the prior one.
			//
			// At a FORCED break CSS truncates the leading margin, so the block lands flush
			// at the page top: cur.top = b.Y. At an UNFORCED/overflow break CSS RETAINS the
			// block's own leading margin as whitespace at the top of the new page, so we
			// pull the page top up by that margin (cur.top = b.Y - mT) and the block lands
			// at local Y == its top margin. (The very first page has no preceding break —
			// forcedBefore is false and cur is empty from the start — so it takes the
			// b.Y branch via the !startsWithForcedBreak path; its body-decoration top is
			// handled separately by wrapperDecorationTop.)
			cur.top = b.Y
			if overflow && !forcedBefore {
				cur.top = b.Y - usedTopMargin(b, cbWidth)
			}
			// A block taller than a page on an otherwise-empty page cannot be split in
			// this slice: keep it, it overflows the page bottom (clipped by the bitmap).
			if b.H > pageH {
				logf("css pagination: block taller than page (%.0fpt > %.0fpt); overflowing, not splitting", b.H, pageH)
			}
		}
		cur.blocks = append(cur.blocks, b)
		if forcedAfter {
			buckets = append(buckets, cur)
			cur = pageBucket{}
		}
	}
	// Append the final page if it has content. After a forced-after on the last
	// block, cur is empty, so it is not appended (no trailing empty page).
	if len(cur.blocks) > 0 {
		buckets = append(buckets, cur)
	}
	if len(buckets) == 0 {
		buckets = append(buckets, pageBucket{top: 0})
	}
	return buckets
}

// wrapperDecorationTop returns f's border-box top (f.Y) when f paints a decoration that
// extends above the page's first content — a non-transparent background or any drawn
// border edge — so the first page's top is pulled up to include it; otherwise it returns
// fallback unchanged (an undecorated wrapper must not move the page top, preserving the
// byte-identical un-bordered case). A nil fragment returns fallback.
func wrapperDecorationTop(f *Fragment, fallback float64) float64 {
	if f == nil {
		return fallback
	}
	if f.Background.A > 0 {
		return f.Y
	}
	for _, e := range f.Border {
		if e.Width > 0 && e.Style != layout.BorderNone {
			return f.Y
		}
	}
	return fallback
}

// effectiveBreaks returns a top-level block's effective forced break-before / break-after,
// folding a NESTED forced break that sits at the block's leading or trailing edge into the
// block itself. A browser forces a break on the ancestor when the break point falls at the
// start/end of its content, so:
//   - a forced break-before on the block, OR on content at the START of the block (the
//     leftmost in-flow descendant spine), yields forcedBefore;
//   - a forced break-after on the block, OR on content at the END of the block (the
//     rightmost in-flow descendant spine), yields forcedAfter.
//
// A forced break in the MIDDLE of the block (neither the leading nor trailing edge) is NOT
// reflected here — honoring it needs splitting the block (mid-box fragmentation, out of
// scope); warnMidBlockForcedBreaks logs that dropped case. This keeps the bounded
// between-top-level-blocks model while honoring the common edge-break authoring pattern.
func effectiveBreaks(b *Fragment) (forcedBefore, forcedAfter bool) {
	forcedBefore = isForcedBreak(breakBefore(b)) || leadingEdgeForcedBreakBefore(b)
	forcedAfter = isForcedBreak(breakAfter(b)) || trailingEdgeForcedBreakAfter(b)
	return forcedBefore, forcedAfter
}

// leadingEdgeForcedBreakBefore reports whether a forced break-before sits on the leftmost
// in-flow descendant spine of b — i.e. on content that begins b, so the break is at b's
// leading edge. It descends into the FIRST in-flow child at each level (skipping out-of-flow
// positioned/float children, which do not begin in-flow content), checking break-before on
// each spine node below b. (b's own break-before is handled by the caller.)
func leadingEdgeForcedBreakBefore(b *Fragment) bool {
	node := b
	for {
		first := firstInFlowChild(node)
		if first == nil {
			return false
		}
		if isForcedBreak(breakBefore(first)) {
			return true
		}
		node = first
	}
}

// trailingEdgeForcedBreakAfter reports whether a forced break-after sits on the rightmost
// in-flow descendant spine of b — content that ends b, so the break is at b's trailing edge.
// Mirror of leadingEdgeForcedBreakBefore over the LAST in-flow child at each level.
func trailingEdgeForcedBreakAfter(b *Fragment) bool {
	node := b
	for {
		last := lastInFlowChild(node)
		if last == nil {
			return false
		}
		if isForcedBreak(breakAfter(last)) {
			return true
		}
		node = last
	}
}

// firstInFlowChild returns f's first in-flow child fragment (skipping out-of-flow
// positioned and floated children), or nil if there is none.
func firstInFlowChild(f *Fragment) *Fragment {
	for _, c := range f.Children {
		if c.IsPositioned || c.IsFloat {
			continue
		}
		return c
	}
	return nil
}

// lastInFlowChild returns f's last in-flow child fragment (skipping out-of-flow positioned
// and floated children), or nil if there is none.
func lastInFlowChild(f *Fragment) *Fragment {
	for i := len(f.Children) - 1; i >= 0; i-- {
		c := f.Children[i]
		if c.IsPositioned || c.IsFloat {
			continue
		}
		return c
	}
	return nil
}

// warnMidBlockForcedBreaks logs once if any descendant of a top-level block carries a forced
// break-before/after that is NOT at the block's leading/trailing edge (so effectiveBreaks
// did not propagate it) — a genuinely mid-block forced break this bounded pass cannot honor
// without splitting the block. Edge breaks ARE honored (propagated), so they must not warn;
// only the dropped mid-block case is logged, once, to keep the remaining omission visible
// rather than silent.
func warnMidBlockForcedBreaks(topLevel []*Fragment, logf func(string, ...any)) {
	// anyForcedBreak reports whether the subtree rooted at f (f and its descendants)
	// carries any forced break-before/after.
	var anyForcedBreak func(f *Fragment) bool
	anyForcedBreak = func(f *Fragment) bool {
		if isForcedBreak(breakBefore(f)) || isForcedBreak(breakAfter(f)) {
			return true
		}
		for _, c := range f.Children {
			if anyForcedBreak(c) {
				return true
			}
		}
		return false
	}
	for _, b := range topLevel {
		// A forced break exists somewhere strictly inside b (a descendant), but neither
		// edge propagated it ⇒ it is mid-block and dropped.
		descendantBreak := false
		for _, c := range b.Children {
			if anyForcedBreak(c) {
				descendantBreak = true
				break
			}
		}
		if !descendantBreak {
			continue
		}
		if leadingEdgeForcedBreakBefore(b) || trailingEdgeForcedBreakAfter(b) {
			continue // an edge break — propagated by effectiveBreaks, not dropped
		}
		logf("css pagination: a mid-block forced break on a nested (non-top-level) block is not honored in this slice; break ignored")
		return
	}
}

// contentWidth returns f's content-box width (its border-box W minus its own left/right
// border + padding), the containing-block width its in-flow children's horizontal
// metrics resolve against. cbWidth is the basis for resolving f's own percentage
// edges. A nil f or nil Box returns f.W (or 0) unchanged.
func contentWidth(f *Fragment, cbWidth float64) float64 {
	if f == nil {
		return 0
	}
	if f.Box == nil {
		return f.W
	}
	ed := usedEdges(f.Box, cbWidth)
	w := f.W - ed.bL - ed.bR - ed.pL - ed.pR
	if w < 0 {
		w = 0
	}
	return w
}

// usedTopMargin returns a top-level block fragment's own used top margin, resolved
// against its containing-block width cbWidth (the body content width). Returns 0 for a
// fragment with no Box. A negative margin returns its value (it pulls the block up,
// matching layout); the caller clamps page geometry as needed.
func usedTopMargin(b *Fragment, cbWidth float64) float64 {
	if b == nil || b.Box == nil {
		return 0
	}
	return usedEdges(b.Box, cbWidth).mT
}

// isForcedBreak reports whether a break-before / break-after value forces a page
// break. "page" / "always" (and the named page-side values left / right / recto /
// verso, treated as a plain forced break — this model has no left/right page
// distinction) are forced; "auto" / "avoid" / "avoid-page" / "" / anything else is
// not.
func isForcedBreak(v string) bool {
	switch v {
	case "page", "always", "left", "right", "recto", "verso":
		return true
	}
	return false
}

// bodyFragment returns the body fragment — the document's top-level block container
// — which is the root fragment's last child (x/net/html always synthesizes
// <html><body>; a leading <style> is display:none and not emitted). Returns nil for
// a nil or childless root.
func bodyFragment(root *Fragment) *Fragment {
	if root == nil || len(root.Children) == 0 {
		return nil
	}
	return root.Children[len(root.Children)-1]
}

// breakBefore reads a fragment's break-before hint (the legacy page-break-before
// folds onto the same style field). A nil fragment or nil Box reads as "" (no
// forced break).
func breakBefore(f *Fragment) string {
	if f == nil || f.Box == nil {
		return ""
	}
	return f.Box.Style.BreakBefore
}

// breakAfter reads a fragment's break-after hint (the legacy page-break-after folds
// onto the same style field). A nil fragment or nil Box reads as "" (no forced
// break).
func breakAfter(f *Fragment) string {
	if f == nil || f.Box == nil {
		return ""
	}
	return f.Box.Style.BreakAfter
}
