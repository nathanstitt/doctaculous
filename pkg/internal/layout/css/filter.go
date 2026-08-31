package css

import (
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/nathanstitt/omnidoc/pkg/internal/css"
	"github.com/nathanstitt/omnidoc/pkg/internal/filtereffects"
	"github.com/nathanstitt/omnidoc/pkg/internal/layout"
	"github.com/nathanstitt/omnidoc/pkg/internal/layout/cssbox"
)

// filterChain parses b's CSS `filter` declaration into its function list, or
// returns nil when the box is unfiltered.
//
// It returns nil in three distinct cases, all of which mean "paint this box
// exactly as it would have painted before the property existed":
//
//   - no declaration, or `filter: none` (the cascade normalizes both to "");
//   - a value the grammar rejects — CSS error handling makes an invalid
//     declaration ignored ENTIRELY, so `grayscale() hue-rotate(oops)` filters
//     nothing at all rather than applying the half that parsed (see
//     filtereffects.Parse's contract);
//   - a syntactically valid list that lowers to nothing, e.g. a list of only
//     url() references, which this path does not resolve (see below).
//
// A url(#id) entry references an SVG <filter> element in the document, which
// the HTML box tree has no way to resolve — pkg/svg owns that machinery and
// resolves it against an SVG document, not an HTML one. Such an entry is
// DROPPED from the chain rather than invalidating it, matching how the SVG side
// treats an unresolvable reference: the surrounding shorthand functions still
// apply. Dropping it is logged once per layout, because it is the one path here
// that silently produces less filtering than the author wrote.
//
// shadows is positionally aligned with the returned functions and carries
// drop-shadow()'s resolved colour (see layout.FilterItem.ShadowColors); it is
// resolved here because an omitted colour means the box's own `color` property,
// which only the cascade knows.
//
// The result is parsed once here, at layout time, and carried on the fragment —
// so the paint stage never re-parses per page or per render worker.
func (e *Engine) filterChain(b *cssbox.Box) (funcs []filtereffects.Function, shadows []color.RGBA) {
	if b == nil || b.Style.Filter == "" {
		return nil, nil
	}
	parsed, ok := filtereffects.Parse(b.Style.Filter, boxLengthResolver(b.Style.FontSizePt, e.rootFontSizePt))
	if !ok {
		return nil, nil
	}
	out := parsed[:0:0] // fresh backing array: never alias the parser's slice
	for _, f := range parsed {
		if f.Kind == filtereffects.FuncURL {
			e.warnOnce("css-filter-url",
				"css: filter: %s references an SVG <filter> element, which an HTML box cannot resolve; "+
					"that entry was dropped and the rest of the list still applies", f.Ref)
			continue
		}
		out = append(out, f)
		shadows = append(shadows, dropShadowColor(f, b))
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, shadows
}

// dropShadowColor resolves drop-shadow()'s colour argument against b.
//
// An OMITTED colour, or the `currentColor` keyword, means the element's own
// `color` property — NOT black. An unrecognized colour token likewise falls back
// to `color`, matching how the SVG side treats the same argument.
// Every non-drop-shadow function returns opaque black, which nothing reads.
func dropShadowColor(f filtereffects.Function, b *cssbox.Box) color.RGBA {
	if f.Kind != filtereffects.FuncDropShadow {
		return color.RGBA{A: 255}
	}
	// The cascade initializes `color` to opaque black (pkg/css's initialStyle), so
	// a fully-zero value here can only come from a hand-built box that never went
	// through it. Treat that as unset rather than as a legitimately transparent
	// shadow, which would silently render nothing.
	cur := b.Style.Color
	if cur == (color.RGBA{}) {
		cur = color.RGBA{A: 255}
	}
	raw := strings.TrimSpace(f.Color)
	if raw == "" || strings.EqualFold(raw, "currentcolor") {
		return cur
	}
	if c, ok := css.ParseColorValue(raw); ok {
		return c
	}
	return cur
}

// filtered reports whether b carries a filter that will actually be applied. A
// filtered box establishes a block formatting context (see establishesNewBFC)
// and a stacking context, which is what makes its whole rendering — own
// decorations included — flatten through one AppendItems call and therefore sit
// inside ONE balanced bracket.
//
// It is a FREE function, not an Engine method, because establishesNewBFC and
// establishesStackingContext are: the box-tree predicates are pure functions of
// the box. It must agree with Engine.filterChain about emptiness or a box would
// establish a BFC without getting a bracket (or the reverse), and it does: the
// only Engine state filterChain adds is the rem basis, which scales a length and
// can never change whether the list parses or how many entries survive.
func filtered(b *cssbox.Box) bool {
	if b == nil || b.Style.Filter == "" {
		return false
	}
	// A zero rem basis is fine here: see boxLengthResolver's fallback, and the
	// validity argument above.
	parsed, ok := filtereffects.Parse(b.Style.Filter, boxLengthResolver(b.Style.FontSizePt, 0))
	if !ok {
		return false
	}
	for _, f := range parsed {
		if f.Kind != filtereffects.FuncURL {
			return true
		}
	}
	return false
}

// boxLengthResolver is the filtereffects.LengthResolver for a CSS box: it
// resolves the absolute units plus em against fontSizePt and rem against
// rootFontSizePt.
//
// rem is the ROOT element's font size, per CSS Values. Resolving it against the
// box's own size instead is wrong by exactly the ratio between them: blur(2rem)
// on a font-size:8px box under a 16px root gives std 16 rather than the required
// 32 — a plausible-looking wrong number, not an obvious failure. Engine records
// the root size once per layout (see Engine.rootFontSizePt and layoutTree); a
// non-positive rootFontSizePt means "unknown" and falls back to the box's own
// size rather than collapsing every rem to nothing.
//
// A PERCENTAGE is rejected, which invalidates the whole declaration. CSS
// `blur()` takes a <length>, and a percentage is not one — the same rule the SVG
// resolver enforces, and the reason `blur(10%)` renders unfiltered rather than
// blurred by some resolved amount.
func boxLengthResolver(fontSizePt, rootFontSizePt float64) filtereffects.LengthResolver {
	remPt := rootFontSizePt
	if remPt <= 0 {
		remPt = fontSizePt
	}
	return func(token string) (float64, bool) {
		token = strings.TrimSpace(token)
		if token == "" || strings.HasSuffix(token, "%") {
			return 0, false
		}
		lower := strings.ToLower(token)
		// Longest suffix first: "rem" ends in "em", so testing "em" first would
		// parse 2rem as 2r em and reject it.
		for _, u := range []struct {
			suffix string
			scale  float64
		}{
			{"rem", remPt},
			{"em", fontSizePt},
			{"ex", fontSizePt / 2}, // the half-the-font-size approximation used throughout the engine
			{"px", 1},              // px is treated 1:1 as pt engine-wide (see ComputedStyle.FontSizePt)
			{"pt", 1},
			{"pc", 12},
			{"in", 72},
			{"cm", 72 / 2.54},
			{"mm", 72 / 25.4},
		} {
			if strings.HasSuffix(lower, u.suffix) {
				n, ok := parseFilterNumber(lower[:len(lower)-len(u.suffix)])
				if !ok {
					return 0, false
				}
				return n * u.scale, true
			}
		}
		// A bare number is a valid <length> only when it is zero (CSS allows a
		// unitless zero for every dimension).
		n, ok := parseFilterNumber(lower)
		if !ok || n != 0 {
			return 0, false
		}
		return 0, true
	}
}

// parseFilterNumber parses a plain number, rejecting NaN and infinities so they
// can never reach downstream pixel math (mirroring filtereffects' own guard).
func parseFilterNumber(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// hasSpatialFilter reports whether f or any fragment in its subtree carries a
// SPATIAL filter — blur or drop-shadow, whose output at a pixel depends on
// neighbouring pixels.
//
// The paginator calls it on a subtree it is about to SPLIT across a page break.
// Brackets are page-local by design (see layout.FilterPushKind), which is exact
// for the per-pixel colour adjustments but an approximation for a spatial
// function: a blur applied per page-slice cannot sample the content that fell on
// the other page, so the seam differs from an unbroken render. That single case
// is worth a log; a warning on every split grayscale() would be pure noise,
// which is why the distinction is drawn here rather than logging any split
// filter.
//
// It walks Children, Floats, and Positioned so a filter anywhere inside the
// split subtree is found, not just on its root.
func hasSpatialFilter(f *Fragment) bool {
	if f == nil {
		return false
	}
	fi := layout.FilterItem{Funcs: f.Filter}
	if fi.Spatial() {
		return true
	}
	for _, c := range f.Children {
		if hasSpatialFilter(c) {
			return true
		}
	}
	for _, c := range f.Floats {
		if hasSpatialFilter(c) {
			return true
		}
	}
	for _, c := range f.Positioned {
		if hasSpatialFilter(c) {
			return true
		}
	}
	return false
}
