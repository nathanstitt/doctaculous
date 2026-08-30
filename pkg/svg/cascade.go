package svg

import (
	"sort"

	"github.com/nathanstitt/omnidoc/pkg/css"
)

// inlineImportantIDs is the synthetic specificity given to an inline
// style="" !important declaration, mirroring pkg/css's
// inlineImportantIDs: CSS ranks inline !important above every author
// !important rule regardless of selector specificity, modeled here as an ID
// count far beyond anything a parsed selector can reach.
const inlineImportantIDs = 1 << 20

// cascadeCtx carries the per-document styling inputs through the scene walk:
// the document's author stylesheets (and id/defs tables, unused here) plus a
// logger for warn-once diagnostics. Built once per document and threaded
// through the scene builder; resolve is called once per element.
type cascadeCtx struct {
	idx  *docIndex
	logf func(string, ...any)
	// host carries a HOST document's stylesheets and inherited values when the SVG is
	// embedded (an inline <svg> in HTML). Nil for a standalone SVG document, which is
	// the path every existing caller takes.
	host *HostContext
}

// hostParentNode returns the css.Node for the <svg> element's parent in the host
// document, or nil when standalone.
func (c *cascadeCtx) hostParentNode() css.Node {
	if h := c.hostContext(); h != nil {
		return h.Parent
	}
	return nil
}

// hostContext returns the embedding host's context, or nil when the SVG is a
// standalone document (including a nil ctx, which resolve tolerates).
func (c *cascadeCtx) hostContext() *HostContext {
	if c == nil || c.host.empty() {
		return nil
	}
	return c.host
}

// matchedDecl is one declaration that matched el, tagged with enough to order
// it against every other matched declaration: its rank source (sheet rule,
// inline style), specificity, and source order (sheets in document order,
// rules within a sheet in source order, declarations within a rule in
// source order).
type matchedDecl struct {
	decl   css.Declaration
	inline bool // true for a style="" declaration, false for a sheet rule
	spec   css.Specificity
	order  int
}

// resolve computes the winning value for every styling property on el,
// applying the SVG cascade: presentation attributes, then author sheet rules
// by (importance, specificity, source order), then the style="" attribute.
// The returned lookup is what Style.apply reads instead of raw attributes.
//
// A nil ctx (or one with no sheets and no inline style) falls back to
// presentation attributes alone, which is exactly PR 1's behavior: a
// document with no <style> element and no style="" attributes anywhere
// costs nothing beyond building the hint map. A nil el yields a lookup that
// always reports not-found, rather than panicking.
func (c *cascadeCtx) resolve(el *element) func(name string) (string, bool) {
	if el == nil {
		return func(string) (string, bool) { return "", false }
	}

	// Presentation hints: OriginPresentationalHint-equivalent. Zero
	// specificity, never important, so any sheet rule or inline
	// declaration for the same property always outranks them. Building
	// this once up front (rather than per matching pass) keeps resolve
	// allocation-light: it is the one hint slice for the whole call.
	resolved := make(map[string]string, len(svgPresentationAttrs))
	for _, d := range svgPresentationHints(el) {
		setResolved(resolved, d.Property, d.Value)
	}

	if c == nil || c.idx == nil {
		return lookupFrom(resolved)
	}

	var normal, important []matchedDecl
	order := 0

	node := &cssNode{el: el, hostParent: c.hostParentNode()}
	// Host sheets first, so they sit BELOW the SVG's own <style> rules in source
	// order and lose a specificity tie to them: the SVG's internal rules are the more
	// specific context for its own content. Within each group, document order holds.
	sheets := c.idx.sheets
	if c.host != nil && len(c.host.Sheets) > 0 {
		sheets = make([]css.Stylesheet, 0, len(c.host.Sheets)+len(c.idx.sheets))
		sheets = append(sheets, c.host.Sheets...)
		sheets = append(sheets, c.idx.sheets...)
	}
	for si := range sheets {
		sheet := &sheets[si]
		for ri := range sheet.Rules {
			rule := &sheet.Rules[ri]
			// TODO: the media context is hardcoded to screen because SVG only
			// opens as a standalone document today, where OpenSVGBytes
			// documents the media option as inert. When SVG gains a print
			// path (SVG-in-PDF, or <img src="*.svg"> inside a paged HTML
			// document), thread the caller's media through cascadeCtx and
			// filter against it here instead — this line is the seam.
			if rule.Media != css.MediaAll && rule.Media != css.MediaScreen {
				continue // rule belongs to a different media context
			}
			if len(rule.Selectors) == 0 {
				// pkg/css.Parse never emits a rule with zero selectors (an
				// entirely-unparseable selector list drops the whole rule
				// before it reaches Stylesheet.Rules), but docIndex.sheets
				// is a plain []css.Stylesheet another caller could
				// construct directly, so guard defensively rather than
				// assume the invariant forever holds.
				c.warn("svg: a style rule matched no selectors (skipped)")
				continue
			}
			spec, ok := bestMatch(rule.Selectors, node)
			if !ok {
				order += len(rule.Declarations)
				continue
			}
			for _, d := range rule.Declarations {
				m := matchedDecl{decl: d, spec: spec, order: order}
				if d.Important {
					important = append(important, m)
				} else {
					normal = append(normal, m)
				}
				order++
			}
		}
	}

	// 1. Normal declarations, lowest to highest rank, overlaying the hints.
	//    Sheet rules always outrank hints (hints never entered this slice),
	//    so a stable sort by (specificity, source order) alone reproduces
	//    the presentation-hint < author-sheet ladder for the normal pass.
	sort.SliceStable(normal, func(i, j int) bool { return lessNormal(normal[i], normal[j]) })
	for _, m := range normal {
		setResolved(resolved, m.decl.Property, m.decl.Value)
	}

	// 2. Inline style="" (author origin, normal declarations only here).
	//    Normal inline declarations overlay all normal sheet rules
	//    unconditionally, matching pkg/css's cascade; inline !important
	//    joins the important set below with an outsized specificity.
	if styleAttr, ok := el.attrs["style"]; ok {
		for _, d := range css.ParseDeclarations(styleAttr) {
			if d.Important {
				important = append(important, matchedDecl{
					decl: d, inline: true,
					spec: css.Specificity{IDs: inlineImportantIDs}, order: order,
				})
				order++
				continue
			}
			setResolved(resolved, d.Property, d.Value)
		}
	}

	// 3. Important declarations overlay last. All important declarations
	//    outrank all normal ones (they are applied after, unconditionally);
	//    among themselves, author-sheet-important < inline-important — there
	//    is no UA sheet in the SVG path, so pkg/css's UA-important-wins-all
	//    rule has no analogue to port here.
	sort.SliceStable(important, func(i, j int) bool { return lessImportant(important[i], important[j]) })
	for _, m := range important {
		setResolved(resolved, m.decl.Property, m.decl.Value)
	}

	return lookupFrom(resolved)
}

// lessNormal orders two normal-pass matches: higher specificity wins; a
// specificity tie breaks by source order (later wins).
func lessNormal(a, b matchedDecl) bool {
	if a.spec.Less(b.spec) {
		return true
	}
	if b.spec.Less(a.spec) {
		return false
	}
	return a.order < b.order
}

// lessImportant orders two important-pass matches: an inline !important
// always outranks a sheet !important regardless of specificity (modeled by
// inlineImportantIDs dwarfing any parsed specificity); otherwise the same
// specificity-then-source-order rule as the normal pass applies.
func lessImportant(a, b matchedDecl) bool {
	if a.inline != b.inline {
		return b.inline // b is inline and a is not => a < b
	}
	return lessNormal(a, b)
}

// bestMatch returns the highest specificity among a rule's selectors that
// match n, and whether any matched.
func bestMatch(sels []css.Selector, n css.Node) (css.Specificity, bool) {
	var best css.Specificity
	found := false
	for _, s := range sels {
		if s.Matches(n) {
			if !found || best.Less(s.Specificity()) {
				best = s.Specificity()
				found = true
			}
		}
	}
	return best, found
}

// markerLonghands are the three properties the "marker" shorthand expands
// to, in the order CSS specifies them.
var markerLonghands = [3]string{"marker-start", "marker-mid", "marker-end"}

// setResolved records one cascade declaration into the resolved-property
// map, EXPANDING a shorthand into its longhands as it goes.
//
// The expansion has to happen here, not in Style.apply, to get shorthand vs.
// longhand precedence right in BOTH directions. resolve collapses the whole
// cascade into a single map keyed by property, so by the time apply() reads
// it, the origin and source order that decided each winner are gone: a
// fixed "shorthand first, then longhands" call order in apply() would make a
// longhand beat a shorthand unconditionally, which is wrong whenever the
// shorthand outranks it. Writing the longhands at the moment the shorthand
// is applied instead makes both compete as the same three properties, in
// the one ordering the cascade already established — so
// `style="marker:url(#a)"` correctly beats a marker-start="url(#b)"
// presentation attribute (inline author style outranks a presentational
// hint), and `style="marker-start:url(#b); marker:url(#a)"` correctly
// yields url(#a) (the shorthand is later in source order).
//
// Only "marker" needs this today: it is the sole shorthand among the
// properties this cascade resolves whose longhands are also cascaded
// properties. The shorthand's own name is still recorded in the map (for a
// future reader and for cheap debugging), but nothing consumes it —
// Style.apply reads only the three longhands.
//
// Note this is expansion, not substitution: a LATER longhand still wins
// over an EARLIER shorthand, because the later write simply overwrites the
// longhand key the shorthand had set.
func setResolved(resolved map[string]string, prop, val string) {
	resolved[prop] = val
	if prop == "marker" {
		for _, lh := range markerLonghands {
			resolved[lh] = val
		}
	}
}

// lookupFrom adapts a resolved property map to the func(name) (string, bool)
// shape Style.apply consumes.
func lookupFrom(resolved map[string]string) func(name string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := resolved[name]
		return v, ok
	}
}

// warn logs msg via c.logf, or is a silent no-op when the context was built
// without one (e.g. in tests). c is guaranteed non-nil by every call site in
// resolve (the nil-ctx path returns before reaching them).
//
// This does not dedupe repeated calls the way sceneBuilder.warnOnceMsg does:
// cascadeCtx has no warned map of its own, and the brief's struct shape
// (idx + logf only) intentionally doesn't add one. In practice this never
// matters — the one call site (a rule with zero selectors) is unreachable
// through pkg/css.Parse's own output, see the call site comment — so it
// cannot actually fire once per element across a real document. If Task 8
// wires cascadeCtx.logf to sceneBuilder.logf directly (not through
// warnOnceMsg), that dedup responsibility stays with the caller.
func (c *cascadeCtx) warn(msg string) {
	if c.logf == nil {
		return
	}
	c.logf("%s", msg)
}
