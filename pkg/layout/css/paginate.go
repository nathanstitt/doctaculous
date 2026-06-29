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
	// break-before/after on a NESTED (non-top-level) block is not honored — a browser
	// would propagate the break up to the nearest ancestor, which this pass does not do.
	// Warn once rather than dropping it silently (honoring it properly is a follow-up).
	warnNestedForcedBreaks(body.Children, e.logf)

	buckets := bucketBlocks(body.Children, pageH, e.logf)

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

	// A top-level position:relative block is in flow (so it is in body.Children and
	// gets bucketed) but is ALSO lifted into the root's positioned layer for painting
	// — the SAME *Fragment pointer appears in both root.Positioned and a bucket. The
	// in-flow Children pass skips it (IsPositioned), so it paints only from the
	// positioned layer; if that layer stayed on page 0 the block would vanish from its
	// real page. Route each such entry to the page its block landed on (the entry IS
	// the block pointer, so shifting the bucket already moves it). Other positioned
	// entries — absolute/fixed boxes (incl. descendants of a relative block) — are not
	// distributed in this slice and ride the first page (a documented deferral).
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
		// Assign this page's share of the positioned layer (its top-level relative
		// blocks; page 0 additionally keeps the undistributed abs/fixed residual). The
		// out-of-flow float layer is not distributed and rides the first page only;
		// null it on later pages so a top-level float is not duplicated. The body owns
		// no positioned/float layer of its own (the root does), so its copies are
		// cleared. The body's own background/border still paints per page (a documented
		// approximation — a full per-page background model is a follow-up).
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
// across the buckets. A position:relative block — in flow (so it is bucketed) but painted
// from the positioned layer — is routed to the page its in-flow position landed on, so it
// paginates normally: either directly, when its *Fragment pointer is itself a top-level
// bucket block, or via its nearest TOP-LEVEL ANCESTOR block, when it is a relative block
// NESTED below a static wrapper (its pointer bubbled to root.Positioned but its in-flow
// box lives in that ancestor's subtree, which is shifted onto the ancestor's page). Routing
// it to the ancestor's page keeps the shift consistent (the entry IS the in-flow fragment,
// already moved by shiftFragments) — otherwise the nested relative block would paint on
// page 0 at a later page's local Y and vanish off the page. Every other entry
// (absolute/fixed, including descendants of a relative block) is undistributed and assigned
// to the first page — the documented deferral. The result has one entry per bucket; a
// parallel info slice is kept aligned so the flatten's z-index/clip metadata stays correct.
func splitPositionedByPage(root *Fragment, buckets []pageBucket) []pagePositioned {
	out := make([]pagePositioned, len(buckets))
	if root == nil || len(root.Positioned) == 0 || len(buckets) == 0 {
		return out
	}
	// Map each top-level block — AND every positioned (relative) descendant in its in-flow
	// subtree — to the block's bucket index. Walking the subtree (Children only: the
	// in-flow chain) catches a relative block nested under static wrappers whose pointer
	// bubbled to root.Positioned. Abs/fixed entries are out of flow and so are NOT in any
	// block's Children subtree → they fall through to page 0 below (the deferral).
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
	for i, frag := range root.Positioned {
		// PositionedInfo may be shorter than Positioned (a nil/short slice reads as the
		// zero value); guard the parallel index.
		var info PositionedInfo
		if i < len(root.PositionedInfo) {
			info = root.PositionedInfo[i]
		}
		page := 0 // default: undistributed (abs/fixed) → first page
		if p, ok := blockPage[frag]; ok {
			page = p // a top-level relative block, or a relative descendant of one → its page
		}
		out[page].frags = append(out[page].frags, frag)
		out[page].infos = append(out[page].infos, info)
	}
	return out
}

// bucketBlocks assigns top-level in-flow block fragments to pageH-tall pages,
// breaking between blocks on overflow and at forced page breaks. It is pure (no
// engine receiver) so it can be unit-tested in isolation; logf records the over-tall
// degradation. It always returns at least one bucket (an empty bucket{top:0} for
// empty input), and never emits a leading or trailing empty bucket for a forced
// break on the first / last block.
func bucketBlocks(blocks []*Fragment, pageH float64, logf func(string, ...any)) []pageBucket {
	if len(blocks) == 0 {
		return []pageBucket{{top: 0}}
	}
	var buckets []pageBucket
	var cur pageBucket
	for _, b := range blocks {
		forcedBefore := isForcedBreak(breakBefore(b))
		overflow := len(cur.blocks) > 0 && (b.Y+b.H)-cur.top > pageH
		// Close the current page only if it already has content: a forced-before on
		// the first block (cur empty) is a no-op, not a leading empty page.
		if (forcedBefore || overflow) && len(cur.blocks) > 0 {
			buckets = append(buckets, cur)
			cur = pageBucket{}
		}
		// The page top is the first block's own page-space Y (its BORDER-box top), taken
		// when the block is added to a fresh page. Reading b.Y here (not a provisional
		// previous-block bottom) keeps the page top correct even when a margin gaps the
		// next block below the prior one — so the page's first content always shifts to
		// local Y 0. Note this collapses any leading margin-top of the new page's first
		// block to 0: correct at a FORCED break (CSS truncates margins there), but at an
		// UNFORCED/overflow break CSS retains the leading margin — a documented
		// simplification (the block lands flush at the page top rather than margin-down).
		if len(cur.blocks) == 0 {
			cur.top = b.Y
			// A block taller than a page on an otherwise-empty page cannot be split in
			// this slice: keep it, it overflows the page bottom (clipped by the bitmap).
			if b.H > pageH {
				logf("css pagination: block taller than page (%.0fpt > %.0fpt); overflowing, not splitting", b.H, pageH)
			}
		}
		cur.blocks = append(cur.blocks, b)
		if isForcedBreak(breakAfter(b)) {
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

// warnNestedForcedBreaks logs once if any block NESTED below a top-level body block
// (i.e. not itself a direct body child) carries a forced break-before/after. This
// bounded pass breaks only between top-level blocks, so such a break is dropped; the
// log makes the omission visible instead of silent. It scans only block-level subtrees
// (the fragmentation unit) and stops at the first hit — a single warning, not spam.
func warnNestedForcedBreaks(topLevel []*Fragment, logf func(string, ...any)) {
	var scan func(f *Fragment) bool
	scan = func(f *Fragment) bool {
		if isForcedBreak(breakBefore(f)) || isForcedBreak(breakAfter(f)) {
			return true
		}
		for _, c := range f.Children {
			if scan(c) {
				return true
			}
		}
		return false
	}
	// Only the DESCENDANTS of each top-level block are nested; the top-level blocks
	// themselves are handled by bucketBlocks.
	for _, b := range topLevel {
		for _, c := range b.Children {
			if scan(c) {
				logf("css pagination: forced break on a nested (non-top-level) block is not honored in this slice; break ignored")
				return
			}
		}
	}
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
