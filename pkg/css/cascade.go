package css

import (
	"image/color"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// inlineImportantIDs is the synthetic specificity IDs value given to inline
// !important declarations. CSS places inline !important above all author
// !important rules regardless of selector specificity; we model that with an IDs
// count (2^20) far larger than any specificity reachable from parsed CSS (which
// would need 2^20 id qualifiers — impossible in practice).
const inlineImportantIDs = 1 << 20

// Origin is a cascade origin. CSS orders declarations by origin first:
// UA-normal < author-normal < author-important < UA-important. Origin is the
// outermost cascade key, dominating specificity and source order.
type Origin int

const (
	// OriginUA is the user-agent default stylesheet.
	OriginUA Origin = iota
	// OriginPresentationalHint is a legacy presentational attribute mapped to a CSS
	// property (HTML §15 "presentational hints" — e.g. bgcolor → background-color). It
	// cascades above the UA stylesheet but below all author CSS, so an explicit author
	// rule or inline style always wins. Hints carry zero specificity and never use
	// !important. Derived per element from its attributes (see presentationalHints).
	OriginPresentationalHint
	// OriginAuthor is page-supplied CSS: <style>, <link>, and style="".
	OriginAuthor
)

// OriginSheet pairs a parsed stylesheet with its cascade origin.
type OriginSheet struct {
	Sheet  Stylesheet
	Origin Origin
}

// ComputedStyle is the resolved style of one element: the normal-flow property
// subset this sub-project supports, with every value concrete. Lengths remain in
// their CSS unit here (px/pt/em/%); the layout engine resolves em/% to absolute
// points against a containing context. Raw, unrecognized declarations are not on
// this struct — they are retained on the Rule for later sub-projects.
//
// Inherited properties (CSS) carry over from the parent in inheritFrom, which is
// the single source of truth for which fields inherit.
type ComputedStyle struct {
	Display string // "block" | "inline" | "none" | "list-item" | raw value

	// CustomProps holds the element's computed custom properties (--*), kept as
	// raw token streams and substituted into other properties by var(). They are
	// INHERITED, which is what makes the common `:root { --brand: … }` pattern
	// reach every descendant. See customprop.go.
	CustomProps CustomProps

	Color           color.RGBA
	BackgroundColor color.RGBA // zero-alpha means transparent / not set

	// Filter is the CSS `filter` shorthand's RAW declaration text ("" or "none"
	// = no filter, the initial value). It is kept unparsed here, exactly as
	// BackgroundImage keeps its url() ref raw, and handed to
	// pkg/filtereffects.Parse at use time by the consumer that also owns the
	// length resolver (a CSS box resolves em/% against its own font size and
	// containing block, which this package has no access to). NOT inherited,
	// per spec.
	//
	// A filtered box BRACKETS its subtree's emitted items the way Clips does
	// with ClipPushKind/ClipPopKind — see layout.FilterPushKind — so the
	// property costs nothing for an unfiltered box.
	Filter string

	// Background image (CSS Backgrounds 3). None are CSS-inherited. BackgroundImage is
	// the resolved url() ref ("" = none); the rest carry the initial value when unset.
	BackgroundImage string
	// BackgroundLayers is the full comma-separated layer list, FIRST LAYER ON TOP
	// (CSS Backgrounds §3.10). The single-value BackgroundImage/BackgroundGradient
	// fields above mirror layer 0, so a consumer that only understands one background
	// keeps working; the paint pass walks this to draw them all.
	//
	// Only the IMAGE varies per layer here. background-size/-repeat/-position and the
	// rest are single-valued in this engine, and layout applies the computed longhand
	// to every layer — so a layer record left zero means "use the element's value",
	// not "use the initial value". Making those per-layer too is a separate slice.
	//
	// Nil means no list was given, and the single-value fields are the whole story.
	BackgroundLayers []BackgroundLayer
	// BackgroundGradient is set INSTEAD of BackgroundImage when background-image
	// is a <gradient> function rather than a url(). The two are mutually
	// exclusive: whichever form the declaration produced clears the other, so a
	// later `background-image: url(x)` correctly replaces an earlier gradient
	// and vice versa. nil means "no gradient", the initial state.
	//
	// It holds a PARSED *Gradient rather than raw declaration text (the way
	// Filter keeps its value raw) because a gradient's grammar needs no length
	// resolution at parse time — only its stop POSITIONS and radii do, and those
	// stay as Lengths inside the struct until layout resolves them against the
	// gradient box. Parsing here means a malformed gradient is dropped by the
	// cascade like any other bad declaration, rather than failing later where the
	// original text is no longer available to report.
	BackgroundGradient *Gradient
	BackgroundRepeat   string         // "repeat" (initial) | "repeat-x" | "repeat-y" | "no-repeat"
	BackgroundPosition BackgroundPos  // initial 0% 0% (top-left)
	BackgroundSize     BackgroundSize // initial auto
	BackgroundOrigin   string         // "padding-box" (initial) | "border-box" | "content-box"
	BackgroundClip     string         // "border-box" (initial) | "padding-box" | "content-box"
	BackgroundAttach   string         // "scroll" (initial) | "fixed" (degraded to scroll)

	// BoxShadow is the parsed `box-shadow` list (CSS Backgrounds 3 §6), nil for
	// the initial `none`. Entries are in SOURCE order, which is the reverse of
	// paint order — the first shadow paints on TOP (see parseBoxShadow). NOT
	// inherited, per spec.
	//
	// It is parsed HERE rather than kept raw the way Filter is, because unlike
	// the `filter` shorthand every context-dependent piece of the grammar is
	// either a Length (which ComputedStyle already carries unresolved
	// everywhere else) or the omitted-colour case, which BoxShadow.HasColor
	// records. Nothing is left that would need a resolver this package lacks,
	// so re-parsing per fragment would buy nothing.
	BoxShadow []BoxShadow

	FontFamily    string
	FontSizePt    float64 // resolved to an absolute size (px treated 1:1 as pt for now)
	Bold          bool
	Italic        bool
	LineHeight    Length // UnitAuto = "normal"
	LineHeightMin Length // "at least" line-height floor (DOCX lineRule=atLeast). Zero = no floor. Inherited.

	// TextAlign is "start" (initial) | "end" | "left" | "right" | "center" |
	// "justify". "start"/"end" are direction-relative and resolve against the used
	// Direction at layout time (see effectiveTextAlign in pkg/layout/css); the
	// physical keywords never flip. Inherited.
	TextAlign string

	TextIndent Length // first-line indent (signed; negative = hanging). Zero length = none. Inherited.

	// TextDecorationLine is the supported subset of CSS text-decoration: "none"
	// (initial), "underline", or "line-through". Modeled as inherited (like Color) so it
	// propagates to inline descendants of a decorating box — the pragmatic approximation
	// the engine uses for text styling. The remaining keyword (overline) and the
	// colors/styles are not modeled (parsed-and-ignored). Painted by the CSS inline
	// formatting context (underline below the baseline, line-through at mid-glyph).
	TextDecorationLine string

	// TextTransform is the CSS text-transform: "none" (initial) | "uppercase" |
	// "lowercase" | "capitalize". Inherited. Applied to a text run's string at shaping
	// time by the inline formatting context. small-caps is approximated upstream as
	// uppercase (true small-caps needs synthesized small capitals — deferred).
	TextTransform string
	// Transform is the resolved CSS `transform` matrix, or the identity when the
	// property is absent or "none". Not inherited. It is a PAINT-time effect: it does
	// not change layout, and the box keeps the space it occupied untransformed
	// (CSS Transforms 1 §3).
	Transform Transform

	// WhiteSpace is the CSS white-space property: "normal" | "nowrap" | "pre" |
	// "pre-wrap" | "pre-line". Inherited; initial "normal". Decomposed into three
	// behaviors by WhiteSpaceFlags (collapse spaces, preserve newlines, wrap).
	WhiteSpace string

	// OverflowWrap is the CSS overflow-wrap property: "normal" (initial) |
	// "break-word" | "anywhere". Inherited. The legacy alias `word-wrap` — still
	// extremely common in real stylesheets, and the name IE shipped the feature under —
	// sets the same field. It permits breaking WITHIN a word as a LAST RESORT: only when
	// the word would overflow the line box even on a line of its own.
	//
	// "break-word" and "anywhere" differ in exactly one respect: "anywhere" also affects
	// intrinsic (min-content) sizing, so a box sized from its content can shrink to a
	// single grapheme cluster, while "break-word" leaves min-content at the widest word.
	// See WordBreakMode in pkg/layout/inline.
	OverflowWrap string

	// WordBreak is the CSS word-break property: "normal" (initial) | "break-all" |
	// "keep-all". Inherited. Unlike overflow-wrap it is EAGER: "break-all" makes every
	// grapheme-cluster boundary an ordinary break opportunity, so a word is chopped at
	// the line edge even when it would have fitted on the following line by itself.
	//
	// "keep-all" forbids the implicit between-ideograph opportunities CJK text would
	// otherwise get. This engine generates no such opportunities (its only implicit ones
	// are spaces), so keep-all's observable effect here is that it suppresses
	// overflow-wrap breaking of the affected text.
	WordBreak string

	// LetterSpacing and WordSpacing are the CSS Text 3 spacing properties, both
	// inherited, both initial "normal" (modeled as the zero Length — see
	// SpacingLength — since `normal` and `0` are indistinguishable for a
	// non-justified line and this engine never applies the extra latitude
	// `normal` nominally grants a justifier).
	//
	// LetterSpacing is added after every typographic character unit; WordSpacing is
	// added at every word-separator character (U+0020 / U+00A0). Both accept
	// NEGATIVE lengths, which tighten. They are resolved to points against the
	// run's own font size and folded into each glyph's advance at shaping time
	// (pkg/layout/inline.Run.LetterSpacingPt/WordSpacingPt), so line breaking,
	// intrinsic sizing, and alignment all see the adjusted widths without knowing
	// the properties exist.
	//
	// These two are ALSO implemented independently in pkg/svg (pkg/svg/style.go),
	// which applies them as a post-shaping advance adjustment on a flat glyph
	// slice. The duplication is real but the two paths differ in a way that is not
	// reconcilable by sharing code: SVG follows SVG 1.1's wording and adds NO
	// trailing letter-spacing after the last glyph of a chunk, while CSS Text 3 and
	// every browser DO add it after the last character (see Run.LetterSpacingPt).
	//
	// NOTE: an SVG element still does NOT inherit these from an enclosing HTML
	// ancestor, and adding these fields did not change that. Inline <svg> is
	// REPLACED content: box generation re-serializes the markup and pkg/svg parses
	// it in isolation via svg.Parse(data, logf), so NO computed property crosses
	// the boundary — not color, not font-family, not these. That is a
	// whole-boundary gap, not a missing-field gap; see docs/SVG.md.
	LetterSpacing Length
	WordSpacing   Length

	// List + counter properties. ListStyleType/ListStylePosition are inherited
	// (initial "disc"/"outside"); the counter ops and Content are not inherited.
	ListStyleType     string        // "disc" | "circle" | "square" | "decimal" | "lower-roman" | ... | "none"
	ListStylePosition string        // "outside" | "inside"
	CounterReset      []CounterOp   // counter-reset name+value pairs (default value 0)
	CounterIncrement  []CounterOp   // counter-increment name+value pairs (default value 1)
	CounterSet        []CounterOp   // counter-set name+value pairs (default value 0)
	Content           []ContentPart // parsed `content` pieces we render (strings + counter()/counters())

	MarginTop, MarginRight, MarginBottom, MarginLeft     Length
	PaddingTop, PaddingRight, PaddingBottom, PaddingLeft Length

	BorderTopWidth, BorderRightWidth, BorderBottomWidth, BorderLeftWidth Length
	BorderTopColor, BorderRightColor, BorderBottomColor, BorderLeftColor color.RGBA
	BorderTopStyle, BorderRightStyle, BorderBottomStyle, BorderLeftStyle string

	// Border corner radii (CSS Backgrounds 3 §5), one elliptical radius per corner.
	// Each is a PAIR of unresolved Lengths rather than one number because a corner's
	// two semi-axes resolve against different dimensions — horizontal against the
	// border box's width, vertical against its height — so they cannot be collapsed
	// before layout knows the box. A zero pair (the initial value) is a square
	// corner. Not inherited. See borderradius.go.
	BorderTopLeftRadius, BorderTopRightRadius       CornerRadius
	BorderBottomRightRadius, BorderBottomLeftRadius CornerRadius

	Width, Height Length // UnitAuto = "auto"

	MinWidth, MaxWidth   Length // MinWidth: UnitPx zero = no min; MaxWidth: UnitAuto = "none" (no max)
	MinHeight, MaxHeight Length // same convention as the width pair
	BoxSizing            string // "content-box" (default) | "border-box"

	// ObjectFit is the replaced-element fitting mode (CSS object-fit):
	// "fill" (default) | "contain" | "cover" | "none" | "scale-down".
	ObjectFit string
	// ObjectPositionX/Y are the CSS object-position as fractions of the content box's
	// free space (0 = left/top, 1 = right/bottom, 0.5 = centered — the initial value).
	// Resolved at parse time from keywords/percentages.
	ObjectPositionX, ObjectPositionY float64

	// Overflow is the CSS overflow shorthand: "visible" (default) | "hidden" |
	// "scroll" | "auto" | "clip". Not inherited. overflow≠visible establishes a block
	// formatting context and clips the box's content to its padding box. In this
	// no-scrollbars single-tall-page model, scroll/auto clip exactly like hidden
	// (there is no scroll position or scrollbar chrome), and clip likewise differs
	// only in forbidding programmatic scrolling and allowing overflow-clip-margin —
	// neither of which exists here.
	//
	// One field covers both axes: this engine has no per-axis clipping, so
	// overflow-x/overflow-y and the two-value shorthand fold into it, with the
	// CLIPPING keyword winning when they differ (a box clipping on either axis clips
	// its content here). That is a deliberate over-clip in the mixed
	// "visible hidden" case, which browsers resolve to auto on the visible axis;
	// dropping the clip entirely would be the worse error.
	Overflow string

	// TextOverflow is CSS Overflow 3 text-overflow: "clip" (the initial) | "ellipsis".
	// Not inherited. It only takes effect on a line the box actually CLIPS — an
	// overflowing line in an overflow:visible box still overflows visibly, matching
	// browsers, because there is nothing to hide the truncation behind.
	TextOverflow string

	// LineClamp is the -webkit-line-clamp / line-clamp line count: 0 means none (the
	// initial). Not inherited. A clamped box stops after N line boxes, shrinks its
	// height to them, and marks line N with an ellipsis — so it is a LAYOUT effect,
	// not only a paint one (a 2-line clamp on 5 lines of text makes the box 2 lines
	// tall, which is what browsers report as its height).
	LineClamp int

	// BoxOrient is the legacy -webkit-box-orient. It is stored so the
	// display:-webkit-box + -webkit-box-orient:vertical + -webkit-line-clamp idiom
	// parses as a unit, but layout implements only the vertical orientation that
	// idiom always uses. Not inherited.
	BoxOrient string

	// BreakBefore / BreakAfter are the CSS fragmentation break hints (break-before /
	// break-after, plus the legacy page-break-before / page-break-after aliases). Read
	// only by the pagination pass (never by layout); a forced value ("page"/"always"/
	// named page sides) starts the box on a new page. Initial "" (auto). Not inherited.
	BreakBefore string
	BreakAfter  string
	// BreakInside is the CSS break-inside hint ("auto"/"avoid"/"avoid-page"). Read only
	// by the pagination pass: "avoid" asks it to keep the box on one page (push it whole
	// to the next page rather than splitting it). Initial "" (auto). Not inherited.
	BreakInside string
	// Page is the CSS `page` property: the name of the @page rule whose geometry/chrome
	// the pages generated by this box use (CSS Paged Media §3.1). Lowercased. Inherited;
	// initial "". Read only by the pagination pass. (Named-page selection of the used
	// page from `page:` is captured here; full named-page propagation is a follow-up.)
	Page string
	// Widows / Orphans are the CSS widows / orphans counts: the minimum number of line
	// boxes a fragmentation break may leave at the TOP (widows) / BOTTOM (orphans) of a
	// page when splitting a block's inline content. Inherited; initial 2. Read only by
	// the pagination pass's line-level splitter.
	Widows  int
	Orphans int

	// StringSet is the CSS `string-set` assignments on this box (CSS GCPM): name→value
	// builders read in document order to feed the page-margin string() function.
	// Not inherited; initial nil. Read only by the pagination pass's string snapshot.
	StringSet []StringSetEntry

	// Float is the CSS float value: "none" (default) | "left" | "right". Not
	// inherited. The box generator maps it to cssbox.FloatKind.
	Float string
	// Clear is the CSS clear value: "none" (default) | "left" | "right" | "both".
	// Not inherited. The layout engine lowers a cleared box below matching floats.
	Clear string

	// Position is the CSS position value: "static" (default) | "relative" |
	// "absolute" | "fixed" | "running" (CSS GCPM running()). Not inherited. The box
	// generator maps it to cssbox.PositionKind.
	Position string
	// RunningName is the name from `position: running(name)` (CSS GCPM): the box is
	// removed from normal flow and re-placed into a @page margin box via element(name).
	// "" when position is not running(). Not inherited.
	RunningName string
	// Top/Right/Bottom/Left are the positioning offset properties (CSS 9.3.2),
	// UnitAuto = "auto" (the initial value). Not inherited. Meaningful only on a
	// positioned box (relative: paint offset; absolute/fixed: placement against
	// the containing block).
	Top, Right, Bottom, Left Length
	// ZIndex is the stack level of a positioned box; ZIndexAuto models the "auto"
	// initial value (ZIndex is read only when ZIndexAuto is false). Not inherited.
	// Parsed now; the minimal stacking pass does not yet sort on it (positioned
	// boxes paint in document order) — full z-index ordering is a later slice.
	ZIndex     int
	ZIndexAuto bool

	// Flexbox (CSS Flexbox L1). Container properties read on a display:flex box;
	// item properties read on each flex item. Defaults set in initialStyle.
	FlexDirection  string // row | row-reverse | column | column-reverse
	FlexWrap       string // nowrap | wrap | wrap-reverse (only nowrap acted on today)
	JustifyContent string // flex-start | flex-end | center | space-between | space-around | space-evenly
	AlignItems     string // stretch | flex-start | flex-end | center | baseline
	AlignSelf      string // auto | stretch | flex-start | flex-end | center | baseline
	ColumnGap      Length // main-axis gap for row, cross-axis gap for column
	RowGap         Length // cross-axis gap for row, main-axis gap for column
	FlexGrow       float64
	FlexShrink     float64
	FlexBasis      Length // length | percentage | UnitAuto ("auto") | UnitContent ("content")
	Order          int

	// Grid (CSS Grid L1). Container properties read on a display:grid box; item
	// properties read on each grid item. Defaults set in initialStyle.
	GridTemplateColumns TrackList
	GridTemplateRows    TrackList
	GridTemplateAreas   GridAreas
	GridAutoColumns     []TrackSize   // implicit column tracks (nil = one auto track)
	GridAutoRows        []TrackSize   // implicit row tracks (nil = one auto track)
	GridAutoFlow        string        // "row" | "column" | "row dense" | "column dense"
	JustifyItems        string        // start|end|center|stretch|baseline|flex-start|flex-end|normal
	JustifySelf         string        // auto|start|end|center|stretch|baseline|flex-start|flex-end|normal
	AlignContent        string        // start|end|center|space-between|space-around|space-evenly|stretch|flex-start|flex-end|normal
	GridPlacement       GridPlacement // an item's resolved col+row endpoints + optional area name

	// Table properties (CSS 2.1 §17).
	// BorderCollapse: "separate" (initial) | "collapse". Inherited.
	BorderCollapse string
	// BorderSpacingH/V: the two axes of border-spacing in points (initial 0,0).
	// Inherited; used only in border-collapse:separate.
	BorderSpacingH, BorderSpacingV float64
	// TableLayout: "auto" (initial) | "fixed". On the table box.
	TableLayout string
	// VerticalAlign: "baseline" (initial) | "top" | "middle" | "bottom" (+ sub/
	// super/text-top/text-bottom parsed, mapped to baseline for table-cell purposes).
	VerticalAlign string
	// CaptionSide: "top" (initial) | "bottom". Inherited.
	CaptionSide string
	// EmptyCells: "show" (initial) | "hide". Inherited. In separate-borders mode, an
	// empty cell with empty-cells:hide paints no border or background.
	EmptyCells string
	// Direction: "ltr" (initial) | "rtl". Inherited. Resolves direction-relative
	// text-align ("start"/"end") and the RTL text-indent edge. Box-level mirroring
	// (table column order, flex main axis, grid inline axis) is logged as
	// unsupported by the layout engine; reordering of text WITHIN a line is not
	// implemented either, so a right-to-left script renders in logical glyph order.
	Direction string
	// UnicodeBidi: "normal" (initial) | "embed" | "isolate" | "bidi-override" |
	// "isolate-override" | "plaintext". NOT inherited (per CSS Writing Modes) —
	// unlike Direction directly above, so do not add it to inheritFrom.
	//
	// Stored only: the value is parsed and carried so authored documents keep it,
	// but nothing acts on it until inline bidi reordering lands (the embedding
	// levels it controls have no meaning without the reordering pass).
	UnicodeBidi string
	// WritingMode: "horizontal-tb" (initial) | "vertical-rl" | "vertical-lr".
	// Inherited (CSS Writing Modes 4 §3.1) — it is in inheritFrom alongside Direction.
	//
	// Only horizontal-tb is honoured. A vertical value is parsed, carried, and
	// reported by the layout engine as unsupported, then laid out horizontally.
	// It is stored rather than dropped so the degradation can be detected and logged
	// once per box: the property previously did not reach the cascade at all, so an
	// author got a silent no-op — a correct stylesheet, a wrong page, and no
	// diagnostic. Vertical layout needs a vertical advance model in the inline layer
	// (the font metrics themselves are available); until that lands, saying so is the
	// honest behaviour. The SVG path reports the same limitation (pkg/svg/style.go).
	WritingMode string
}

// Resolver computes the ComputedStyle of any node against parsed stylesheets
// tagged by origin. Build one with NewResolver; it is read-only after
// construction and safe for concurrent use. logf may be nil.
type Resolver struct {
	sheets []OriginSheet
	logf   func(string, ...any)
	media  Media // active media context; only rules for this type (or MediaAll) apply
}

// NewResolver builds a Resolver over origin-tagged stylesheets. Sheets may be
// given in any order; the cascade applies origin/specificity/source-order rules.
// The media context defaults to MediaScreen (the interactive/HTML render); call
// SetMedia to switch to print for PDF output.
func NewResolver(sheets []OriginSheet, logf func(string, ...any)) *Resolver {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	reportUnsupportedSelectors(sheets, logf)
	return &Resolver{sheets: sheets, logf: logf, media: MediaScreen}
}

// reportUnsupportedSelectors logs ONE line per distinct unimplemented selector
// construct found across the AUTHOR sheets, quoting the first selector that used
// it. Parse itself cannot log (see Stylesheet.Unsupported); this is the first
// point where every sheet and a logger are in hand at once.
//
// UA sheets are skipped: the engine ships those, they are written to what the
// selector parser supports, and a diagnostic about them would blame the author
// for the engine's own stylesheet.
//
// Warn-once per construct, not per selector, is the whole point. A design-tool
// SVG export or a framework stylesheet can contain hundreds of `>` rules; one
// line naming the construct tells the author everything the hundred would, and a
// warning on every valid stylesheet — or a hundred on one invalid one — would be
// worse than the silence it replaces.
func reportUnsupportedSelectors(sheets []OriginSheet, logf func(string, ...any)) {
	seen := map[string]bool{}
	for _, os := range sheets {
		if os.Origin != OriginAuthor {
			continue
		}
		for _, u := range os.Sheet.Unsupported {
			if seen[u.Construct] {
				continue
			}
			seen[u.Construct] = true
			logf("css: %s is not supported; rules using it are ignored (first: %q)", u.Construct, u.Selector)
		}
	}
}

// SetMedia sets the active media context (MediaScreen or MediaPrint). Rules tagged
// with a different media type are excluded from the cascade; MediaAll rules (every
// top-level rule) always apply, so a document with no @media blocks is unaffected.
func (r *Resolver) SetMedia(m Media) { r.media = m }

// ComputeRoot returns the ComputedStyle of a root element (one with no parent),
// using the CSS initial values as the inheritance base. Box generation calls
// this for the document root, then threads each result down to children via
// Compute, so callers never need the CSS initial values themselves.
func (r *Resolver) ComputeRoot(n Node) ComputedStyle {
	return r.Compute(n, initialStyle())
}

// matchedDecl is one declaration that matched an element, carried with the three
// keys the cascade sorts on (origin, specificity, source order). It is package
// scope rather than local to Compute so the custom-property pass can range over
// the same already-sorted slices without copying them into a second type.
type matchedDecl struct {
	decl   Declaration
	origin Origin
	spec   Specificity
	order  int
}

// Compute returns node n's ComputedStyle. parentStyle is the already-computed
// style of n's parent; for a root element (no parent) call ComputeRoot, which
// supplies the CSS initial values as the base. The cascade orders matching
// declarations by origin first (UA-normal < author-normal < author-important <
// UA-important), then specificity, then source order, starting from the
// inheritance base; !important declarations are applied last.
func (r *Resolver) Compute(n Node, parentStyle ComputedStyle) ComputedStyle {
	cs := inheritFrom(parentStyle)

	var normal, important []matchedDecl

	order := 0
	for si := range r.sheets {
		origin := r.sheets[si].Origin
		sheet := &r.sheets[si].Sheet
		for ri := range sheet.Rules {
			rule := &sheet.Rules[ri]
			if rule.Media != MediaAll && rule.Media != r.media {
				continue // rule belongs to a different media context
			}
			spec, ok := bestMatch(rule.Selectors, n)
			if !ok {
				continue
			}
			for _, d := range rule.Declarations {
				m := matchedDecl{decl: d, origin: origin, spec: spec, order: order}
				if d.Important {
					important = append(important, m)
				} else {
					normal = append(normal, m)
				}
				order++
			}
		}
	}

	// Presentational hints: legacy attributes mapped to CSS properties (HTML §15).
	// They enter the normal pass at OriginPresentationalHint (ranked above UA, below
	// author) with zero specificity, so an author rule or inline style always wins. A
	// document with no such attributes contributes nothing here (byte-identical).
	for _, d := range presentationalHints(n) {
		normal = append(normal, matchedDecl{decl: d, origin: OriginPresentationalHint, order: order})
		order++
	}
	if dirAutoRequested(n) {
		// Resolving dir=auto means finding the first strong-directional character in
		// the element's text, which needs the bidi character database. Degrade to the
		// inherited/initial direction and say so rather than guessing.
		r.logf("css: dir=\"auto\" on <%s> not supported; using the inherited direction", n.Tag())
	}

	// normalRank/importantRank place each origin on the unified cascade ladder so
	// the same comparison works for both passes:
	//   UA-normal(0) < hint(1) < author-normal(2) < author-important(3) < UA-important(4)
	normalRank := func(o Origin) int {
		switch o {
		case OriginAuthor:
			return 2
		case OriginPresentationalHint:
			return 1
		default:
			return 0 // UA
		}
	}
	importantRank := func(o Origin) int {
		// Presentational hints have no !important; they never reach the important pass.
		if o == OriginUA {
			return 4
		}
		return 3 // author
	}

	lessBy := func(rank func(Origin) int) func(a, b matchedDecl) bool {
		return func(a, b matchedDecl) bool {
			ra, rb := rank(a.origin), rank(b.origin)
			if ra != rb {
				return ra < rb
			}
			if a.spec.Less(b.spec) {
				return true
			}
			if b.spec.Less(a.spec) {
				return false
			}
			return a.order < b.order
		}
	}

	// 1. normal declarations, lowest to highest.
	sort.SliceStable(normal, func(i, j int) bool { return lessBy(normalRank)(normal[i], normal[j]) })

	// Custom properties resolve in a SEPARATE, EARLIER pass over the same sorted
	// declarations (CSS Variables 1 §3). They must all be known before any var()
	// is substituted, because a var() may reference a custom property declared
	// later in the stylesheet than the property using it:
	//
	//	.a { color: var(--fg) }   /* uses --fg ... */
	//	:root { --fg: red }       /* ... declared after */
	//
	// Substituting during the single normal pass would resolve --fg against a
	// half-built map and drop the colour. The passes are ordered, not
	// interleaved: custom properties (normal, then inline, then important) settle
	// first, then every other property substitutes against the final map.
	applyCustomProps(&cs, normal, n, important)

	// Assemble the full application order — normal rules, then inline normal
	// declarations, then !important — as ONE list, so the var()-failure pass
	// below can see which declaration actually wins each property.
	ordered := make([]Declaration, 0, len(normal)+len(important))
	for _, m := range normal {
		ordered = append(ordered, m.decl)
	}

	// 2. inline style="" (author origin). Normal inline declarations overlay all
	//    normal rules; inline !important joins the important set with an outsized
	//    specificity and author origin.
	if styleAttr, ok := n.Attr("style"); ok {
		for _, d := range ParseDeclarations(styleAttr) {
			if d.Important {
				important = append(important, matchedDecl{
					decl: d, origin: OriginAuthor,
					spec: Specificity{IDs: inlineImportantIDs}, order: order,
				})
				order++
				continue
			}
			ordered = append(ordered, d)
		}
	}

	// 3. important declarations overlay last.
	sort.SliceStable(important, func(i, j int) bool { return lessBy(importantRank)(important[i], important[j]) })
	for _, m := range important {
		ordered = append(ordered, m.decl)
	}

	// A declaration whose var() cannot be substituted is invalid at
	// computed-value time: it wins the cascade and THEN fails, leaving the
	// property at its inherited/initial value rather than at any earlier
	// declaration's value (see resolveDeclValue for the spec text). Since the
	// last write to a property is the one that wins here, a property whose FINAL
	// declaration fails must have every earlier declaration suppressed too —
	// otherwise the loser would show through, which is precisely the outcome the
	// spec rules out.
	//
	// The set is keyed by property name, so `background-color` failing does not
	// suppress `background`; a shorthand and its longhands are distinct entries,
	// matching how applyDeclaration already treats them.
	var invalidated map[string]bool
	for _, d := range ordered {
		if IsCustomProperty(d.Property) || !containsVar(d.Value) {
			continue
		}
		if !declSurvivesSubstitution(d, cs.CustomProps) {
			if invalidated == nil {
				invalidated = make(map[string]bool)
			}
			invalidated[d.Property] = true
		} else if invalidated != nil {
			// A later VALID declaration for the same property re-establishes it:
			// only the winner's fate matters, and this one wins over the earlier
			// failure.
			delete(invalidated, d.Property)
		}
	}

	for _, d := range ordered {
		if invalidated[d.Property] {
			continue
		}
		resolved, ok := resolveDeclValue(d, cs.CustomProps)
		if !ok {
			continue
		}
		applyDeclaration(&cs, resolved)
	}
	return cs
}

// resolveDeclValue substitutes any var() in d's value, reporting whether the
// declaration survives. ok=false means the declaration is INVALID AT
// COMPUTED-VALUE TIME (CSS Variables 1 §3.1) and must not be applied.
//
// This failure mode is genuinely unlike every other parse failure in this
// engine, which is why Compute handles it in a dedicated pre-pass rather than by
// simply skipping the declaration:
//
//	div { background-color: red }              /* a valid declaration */
//	div { background-color: var(--undefined) } /* wins the cascade, THEN fails */
//
// The result is NOT red. A var() that cannot be substituted is not detectable at
// parse time — the declaration is syntactically valid — so it enters and WINS
// the cascade, and only then fails. The spec requires the computed value to be
//
//	"either the property's inherited value or its initial value depending on
//	 whether the property is inherited or not, respectively, as if the
//	 property's value had been specified as the unset keyword"
//
// and its own worked example gives transparent (background-color's initial
// value) for exactly the markup above. The spec draws the contrast explicitly: a
// PLAIN syntax error (`background-color: 20px`) is discarded at parse time and
// so DOES leave red in place. Verified against Chrome, which agrees on all three
// cases (var-undefined => transparent, syntax error => red, inherited property
// => the inherited value).
func resolveDeclValue(d Declaration, props CustomProps) (Declaration, bool) {
	if !containsVar(d.Value) {
		return d, true
	}
	substituted, ok := substituteVars(d.Value, props)
	if !ok {
		return d, false
	}
	d.Value = substituted
	return d, true
}

// probeContrastStyle returns a ComputedStyle that differs from initialStyle() in
// every field, for use as the second probe in declSurvivesSubstitution.
//
// It is built by reflection rather than by hand so it cannot drift as fields are
// added to ComputedStyle: a new field that this function forgot to perturb would
// silently agree between the two probes and make every declaration look valid.
// Correctness here needs only "differs from the initial value in every field",
// not any particular value, so the perturbation is deliberately crude.
func probeContrastStyle() ComputedStyle {
	cs := initialStyle()
	v := reflect.ValueOf(&cs).Elem()
	perturb(v)
	return cs
}

// perturb walks a value and changes every settable leaf field it finds,
// recursing into nested structs (Length, BackgroundPos, …) so their fields are
// perturbed individually rather than left at a struct-level zero.
func perturb(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if f.CanSet() {
				perturb(f)
			}
		}
	case reflect.String:
		v.SetString(v.String() + "\x00probe")
	case reflect.Bool:
		v.SetBool(!v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(v.Int() ^ 0x5f5f)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(v.Uint() ^ 0x5f)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(v.Float() + 1234.5)
	}
	// Slices, maps and interfaces are left alone: no property writes one from a
	// declaration value, so they cannot distinguish applied from dropped.
}

// declSurvivesSubstitution reports whether a var()-bearing declaration ends up
// valid, covering BOTH ways CSS Variables 1 §3.1 makes one invalid at
// computed-value time:
//
//	--x: 20px;  background-color: var(--x)   /* substitutes fine, 20px is no colour */
//	            background-color: var(--nope) /* does not substitute at all */
//
// The spec's own worked example is the first form, so handling only the second
// would leave `background-color: red` showing where the spec requires
// transparent.
//
// Whether the substituted value parses is answered by applying it: a value the
// engine cannot parse leaves the style untouched (every branch of
// applyDeclaration drops a malformed value rather than writing a zero), so
// applying to a probe and comparing detects the failure without duplicating any
// property's grammar here. The probe is seeded with a value the declaration
// cannot itself produce, so "unchanged" unambiguously means "not applied".
func declSurvivesSubstitution(d Declaration, props CustomProps) bool {
	resolved, ok := resolveDeclValue(d, props)
	if !ok {
		return false
	}
	// An empty substitution result (`--x: ; color: var(--x)`) is a valid
	// substitution of nothing, which leaves the property with no value at all —
	// invalid at computed-value time, per the same section.
	if strings.TrimSpace(resolved.Value) == "" {
		return false
	}

	// Two probes seeded differently in every field the cascade can write. A
	// successful apply overwrites the property's fields in both, so the probes
	// AGREE on them afterwards; a dropped value leaves both probes at their
	// distinct seeds. Testing for agreement rather than for change is what makes
	// this correct when the substituted value happens to equal one probe's seed
	// (`color: var(--x)` with `--x: black`, black being the initial colour) —
	// a single-probe "did anything change?" test reports that valid declaration
	// as invalid.
	// Apply to a probe whose every field differs from the initial style, and ask
	// whether ANY field moved. A value the engine cannot parse is dropped by
	// every branch of applyDeclaration, leaving the probe untouched; a value it
	// can parse writes at least one field, and — because the probe's fields all
	// start at deliberately non-initial values — that write is visible even when
	// the parsed value happens to equal the CSS initial value. (Seeding from
	// initialStyle() instead would make `color: var(--x)` with `--x: black`
	// indistinguishable from a dropped declaration.)
	probe := probeContrastStyle()
	before := probe
	applyDeclaration(&probe, resolved)

	// DeepEqual rather than ==: ComputedStyle holds slice fields (counters,
	// content, grid track lists) and the custom-property map, so it is not a
	// comparable type.
	return !reflect.DeepEqual(probe, before)
}

// applyCustomProps runs the custom-property pass of the cascade, writing every
// declared custom property onto cs.CustomProps before any var() is substituted.
//
// It mirrors Compute's own three-stage ordering (sorted normal rules, then
// inline style, then sorted !important rules) so a custom property cascades by
// exactly the same rules as every other property. The declaration slices arrive
// ALREADY SORTED from the caller; re-sorting here would be redundant work on the
// hot path.
//
// Custom-property VALUES are stored raw. They are token streams, not typed
// values, so there is nothing to parse until substitution — and a value that
// itself contains var() is resolved lazily at substitution time, which is what
// lets `--a: var(--b)` work regardless of declaration order.
func applyCustomProps(cs *ComputedStyle, normal []matchedDecl, n Node, important []matchedDecl) {
	for _, m := range normal {
		if IsCustomProperty(m.decl.Property) {
			cs.CustomProps.set(m.decl.Property, m.decl.Value)
		}
	}
	if styleAttr, ok := n.Attr("style"); ok {
		for _, d := range ParseDeclarations(styleAttr) {
			// Inline !important custom properties are collected into the
			// important slice by Compute and applied below, so skip them here to
			// avoid applying them a stage too early.
			if IsCustomProperty(d.Property) && !d.Important {
				cs.CustomProps.set(d.Property, d.Value)
			}
		}
	}
	for _, m := range important {
		if IsCustomProperty(m.decl.Property) {
			cs.CustomProps.set(m.decl.Property, m.decl.Value)
		}
	}
}

// bestMatch returns the highest specificity among a rule's selectors that match
// n, and whether any matched.
func bestMatch(sels []Selector, n Node) (Specificity, bool) {
	var best Specificity
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

// inheritFrom builds an element's base style: inherited properties carry over
// from the parent's computed style; everything else resets to initial.
func inheritFrom(parent ComputedStyle) ComputedStyle {
	cs := initialStyle()
	// This function is the single source of truth for which fields inherit; a
	// property added to ComputedStyle but omitted here silently resets to initial
	// instead of inheriting.
	// Custom properties inherit. The map is handed over BY REFERENCE and
	// CustomProps.set clones before mutating, so a subtree that declares no new
	// variable shares one map with its ancestor instead of copying it per element.
	cs.CustomProps = parent.CustomProps
	cs.Color = parent.Color
	cs.FontFamily = parent.FontFamily
	cs.FontSizePt = parent.FontSizePt
	cs.Bold = parent.Bold
	cs.Italic = parent.Italic
	cs.LineHeight = parent.LineHeight
	cs.LineHeightMin = parent.LineHeightMin
	cs.TextAlign = parent.TextAlign
	cs.TextIndent = parent.TextIndent
	cs.TextDecorationLine = parent.TextDecorationLine
	cs.TextTransform = parent.TextTransform
	cs.WhiteSpace = parent.WhiteSpace
	cs.OverflowWrap = parent.OverflowWrap // CSS Text 3: overflow-wrap is inherited
	cs.WordBreak = parent.WordBreak       // CSS Text 3: word-break is inherited
	// CSS Text 3: letter-spacing and word-spacing are inherited. The inherited value
	// is the SPECIFIED length, so an em value re-resolves against each descendant's
	// own font size (a 0.1em tracking set on <body> tracks a larger heading more
	// widely, which is what an author writing em intends and what browsers do).
	cs.LetterSpacing = parent.LetterSpacing
	cs.WordSpacing = parent.WordSpacing
	cs.ListStyleType = parent.ListStyleType
	cs.ListStylePosition = parent.ListStylePosition
	cs.BorderCollapse = parent.BorderCollapse
	cs.BorderSpacingH = parent.BorderSpacingH
	cs.BorderSpacingV = parent.BorderSpacingV
	cs.CaptionSide = parent.CaptionSide
	cs.EmptyCells = parent.EmptyCells
	cs.Direction = parent.Direction
	cs.WritingMode = parent.WritingMode // CSS Writing Modes 4: writing-mode is inherited
	cs.Page = parent.Page               // CSS Paged Media: `page` is inherited
	cs.Widows = parent.Widows           // CSS: widows is inherited
	cs.Orphans = parent.Orphans         // CSS: orphans is inherited
	// table-layout, vertical-align, break-*, break-inside, filter are NOT inherited
	// (per CSS). filter in particular must not inherit: it applies once to the box's
	// whole rendered subtree, so inheriting it would re-apply the effect at every
	// descendant.
	return cs
}

// InitialStyle returns a ComputedStyle holding the CSS initial values (auto/none
// lengths, the initial keywords, etc.). Reflow frontends that synthesize
// ComputedStyle values directly instead of running the cascade (e.g. the DOCX
// lowering) MUST start from this base — a bare ComputedStyle{} literal leaves
// Width/Height/MaxWidth as the zero Length ({0, UnitPx}), which the layout engine
// reads as an explicit 0px, collapsing every block to zero size.
func InitialStyle() ComputedStyle { return initialStyle() }

// initialStyle returns a ComputedStyle holding the CSS initial values, used as
// the base for the root element before any rule or inheritance is applied.
func initialStyle() ComputedStyle {
	black := color.RGBA{0, 0, 0, 255}
	return ComputedStyle{
		Display:            "inline",
		Color:              black,
		FontFamily:         "serif",
		FontSizePt:         16,
		LineHeight:         Length{Unit: UnitAuto},
		TextAlign:          "start",
		TextDecorationLine: "none",
		TextTransform:      "none",
		Transform:          identityTransform(),
		WhiteSpace:         "normal",
		OverflowWrap:       "normal",
		WordBreak:          "normal",
		ListStyleType:      "disc",
		ListStylePosition:  "outside",
		BackgroundRepeat:   "repeat",
		BackgroundPosition: initialBackgroundPosition(),
		BackgroundOrigin:   "padding-box",
		BackgroundClip:     "border-box",
		BackgroundAttach:   "scroll",
		Width:              Length{Unit: UnitAuto},
		Height:             Length{Unit: UnitAuto},
		MinWidth:           Length{Unit: UnitPx},   // CSS initial min-width is 0
		MaxWidth:           Length{Unit: UnitAuto}, // models CSS "none" (no maximum)
		MinHeight:          Length{Unit: UnitPx},   // CSS initial min-height is 0
		MaxHeight:          Length{Unit: UnitAuto}, // models CSS "none" (no maximum)
		BoxSizing:          "content-box",
		ObjectFit:          "fill", // CSS initial object-fit
		ObjectPositionX:    0.5,    // CSS initial object-position: 50% 50%
		ObjectPositionY:    0.5,
		Overflow:           "visible", // CSS initial overflow
		Float:              "none",    // CSS initial float
		Clear:              "none",    // CSS initial clear
		Position:           "static",  // CSS initial position
		Top:                Length{Unit: UnitAuto},
		Right:              Length{Unit: UnitAuto},
		Bottom:             Length{Unit: UnitAuto},
		Left:               Length{Unit: UnitAuto},
		ZIndexAuto:         true, // CSS initial z-index is auto
		MarginTop:          Length{Unit: UnitPx},
		MarginRight:        Length{Unit: UnitPx},
		// remaining margins/paddings default to zero px (the zero value of Length is {0,UnitPx})
		FlexDirection:  "row",
		FlexWrap:       "nowrap",
		JustifyContent: "flex-start",
		AlignItems:     "stretch",
		AlignSelf:      "auto",
		FlexGrow:       0,
		FlexShrink:     1,
		FlexBasis:      Length{Unit: UnitAuto},
		Order:          0,
		// ColumnGap, RowGap default to the zero Length ({0, UnitPx}) = no gap.
		GridAutoFlow: "row",
		JustifyItems: "stretch",
		JustifySelf:  "auto",
		AlignContent: "start",
		// GridAutoColumns/GridAutoRows default to nil: layout treats nil as one auto track.
		// GridTemplateColumns/Rows/Areas default to zero value (empty = no explicit template).
		BorderCollapse: "separate",
		TableLayout:    "auto",
		VerticalAlign:  "baseline",
		CaptionSide:    "top",
		EmptyCells:     "show",
		Direction:      "ltr",
		WritingMode:    "horizontal-tb",
		UnicodeBidi:    "normal",
		// BorderSpacingH/V default to 0 (zero value).
		Widows:  2, // CSS initial widows
		Orphans: 2, // CSS initial orphans
	}
}

// applyDeclaration interprets one declaration and writes it onto cs. Properties
// outside the supported normal-flow subset are ignored (left for later
// sub-projects). Malformed values are dropped, leaving the prior value intact.
func applyDeclaration(cs *ComputedStyle, d Declaration) {
	// Custom properties were already applied by applyCustomProps, in their own
	// earlier pass. Returning here keeps them out of the property switch, where
	// they would otherwise fall through to the default no-op anyway.
	if IsCustomProperty(d.Property) {
		return
	}

	switch d.Property {
	case "display":
		cs.Display = d.Value
	case "color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.Color = c
		}
	case "background-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BackgroundColor = c
		}
	case "background-image":
		// Both fields are assigned together so the two <image> forms stay
		// mutually exclusive: a gradient replacing an earlier url() must clear
		// the url(), and vice versa. ok=false leaves BOTH untouched, which is
		// what makes an unimplemented <image> keep the prior value rather than
		// silently resetting the property to none.
		// background-image takes the same comma-separated layer list as the shorthand.
		//
		// It sets ONLY the image of each layer. background-size, -repeat, -position and
		// friends are separate longhands that may be declared either side of this one,
		// so the layer records leave them zero and the LAYOUT side reads the final
		// computed longhand values instead. Capturing them here would freeze whatever
		// they happened to be at this point in the declaration block, which broke
		// background-size when it was declared after background-image.
		if layers, ok := parseBackgroundImageList(d.Value); ok {
			cs.BackgroundLayers = layers
			if len(layers) > 0 {
				cs.BackgroundImage, cs.BackgroundGradient = layers[0].Image, layers[0].Gradient
			} else {
				cs.BackgroundImage, cs.BackgroundGradient = "", nil
			}
		}
	case "filter":
		// Kept RAW (see ComputedStyle.Filter): the grammar is parsed by
		// pkg/filtereffects at use time, where a length resolver exists. "none"
		// (the initial value) is normalized to "" so an unfiltered box is the
		// zero value and every downstream check is a single emptiness test.
		if v := strings.TrimSpace(d.Value); strings.EqualFold(v, "none") {
			cs.Filter = ""
		} else {
			cs.Filter = v
		}
	case "background-repeat":
		switch strings.ToLower(strings.TrimSpace(d.Value)) {
		case "repeat", "repeat-x", "repeat-y", "no-repeat":
			cs.BackgroundRepeat = strings.ToLower(strings.TrimSpace(d.Value))
		}
	case "background-position":
		if p, ok := parseBackgroundPosition(d.Value); ok {
			cs.BackgroundPosition = p
		}
	case "background-size":
		if s, ok := parseBackgroundSize(d.Value); ok {
			cs.BackgroundSize = s
		}
	case "background-origin":
		if v, ok := normalizeBoxValue(d.Value); ok {
			cs.BackgroundOrigin = v
		}
	case "background-clip":
		if v, ok := normalizeBoxValue(d.Value); ok {
			cs.BackgroundClip = v
		}
	case "background-attachment":
		switch strings.ToLower(strings.TrimSpace(d.Value)) {
		case "scroll", "local", "fixed":
			cs.BackgroundAttach = strings.ToLower(strings.TrimSpace(d.Value))
		}
	case "box-shadow":
		// `none` and every malformed value both clear the property rather than
		// leaving the previous declaration standing. That is the CSS cascade's
		// own rule for a LATER declaration in the same origin: `box-shadow: 2px
		// 2px; box-shadow: none` must render no shadow, and a browser likewise
		// drops an invalid later declaration back to... the earlier valid one.
		//
		// Those two differ, and this takes the `none` side for BOTH. It is a
		// known, narrow divergence: recovering the earlier value would mean
		// keeping every superseded declaration around, which no other property
		// on this struct does. It only shows when a stylesheet writes a valid
		// box-shadow and then an invalid one on the same element.
		if shadows, ok := parseBoxShadow(d.Value); ok {
			cs.BoxShadow = shadows
		} else {
			cs.BoxShadow = nil
		}
	case "background":
		applyBackground(cs, d.Value)
	case "font":
		// The `font` shorthand: [style||variant||weight]? size[/line-height] family.
		expandFont(cs, d.Value)
	case "font-family":
		cs.FontFamily = cleanFamilyList(d.Value)
	case "font-size":
		// "auto" is not a valid font-size, so the UnitAuto guard drops it.
		if l, ok := parseLength(newTokenizer(d.Value).next()); ok && l.Unit != UnitAuto {
			cs.FontSizePt = l.Value // px:pt 1:1 for now; em/% resolution is the engine's job
		}
	case "font-weight":
		cs.Bold = d.Value == "bold" || d.Value == "700" || d.Value == "800" || d.Value == "900"
	case "font-style":
		cs.Italic = d.Value == "italic" || d.Value == "oblique"
	case "line-height":
		// Accepts a unitless NUMBER (the commonest spelling, and a multiplier of the
		// font size) as well as a length or "normal".
		//
		// An em or % value is COMPUTED HERE against this element's own font size, so
		// what descendants inherit is a fixed length — CSS 2.1 §10.8.1. A number is
		// deliberately left as a number, because it inherits as one and re-multiplies
		// against each descendant's own font size. That difference is the whole reason
		// the two units stay distinct: `line-height: 2` on a 10px parent gives a 40px
		// child an 80px line box, while `line-height: 2em` gives it a 20px one.
		if l, ok := parseNumberOrLength(newTokenizer(d.Value).next()); ok {
			switch l.Unit {
			case UnitEm:
				cs.LineHeight = Length{Value: l.Value * cs.FontSizePt, Unit: UnitPt}
			case UnitPercent:
				cs.LineHeight = Length{Value: l.Value / 100 * cs.FontSizePt, Unit: UnitPt}
			default:
				cs.LineHeight = l
			}
		} else if d.Value == "normal" {
			cs.LineHeight = Length{Unit: UnitAuto}
		}
	case "text-align":
		switch d.Value {
		case "left", "right", "center", "justify", "start", "end":
			cs.TextAlign = d.Value
		case "match-parent":
			// CSS Text: match-parent resolves against the PARENT's direction, which
			// the cascade has already folded into the inherited value. Treating it as
			// "start" (the initial) yields the same used value for every case the
			// engine can express, since a child's direction equals its parent's
			// unless the child itself sets `direction` — and then start/end are
			// resolved against the child anyway.
			cs.TextAlign = "start"
		}
	case "text-indent":
		// A single length token (px/pt/em/%); may be signed (negative = hanging).
		setLength(&cs.TextIndent, d.Value)
	case "text-decoration", "text-decoration-line":
		// Supported subset: underline / line-through / none. The shorthand may carry
		// color/style/thickness tokens too; we scan for the line keyword. "none" clears it.
		cs.TextDecorationLine = parseTextDecorationLine(d.Value)
	case "text-transform":
		switch d.Value {
		case "uppercase", "lowercase", "capitalize", "none":
			cs.TextTransform = d.Value
		}
	case "transform":
		// "none" and any unimplemented function (notably the 3D ones) leave the
		// previous value, per CSS error handling.
		if t, ok := parseTransform(d.Value, cs.FontSizePt); ok {
			cs.Transform = t
		} else if strings.EqualFold(strings.TrimSpace(d.Value), "none") {
			cs.Transform = identityTransform()
		}
	case "white-space":
		switch d.Value {
		case "normal", "nowrap", "pre", "pre-wrap", "pre-line":
			cs.WhiteSpace = d.Value
		}
	case "overflow-wrap", "word-wrap":
		// `word-wrap` is the legacy alias overflow-wrap was standardized from; CSS Text 3
		// requires user agents to treat it as a shorthand for the same property, and it is
		// still what a lot of real stylesheets say. Both spellings land in the same field,
		// so later declarations of either name override earlier ones — which is the
		// cascade behavior an author gets in a browser.
		switch d.Value {
		case "normal", "break-word", "anywhere":
			cs.OverflowWrap = d.Value
		}
	case "word-break":
		switch d.Value {
		case "normal", "break-all", "keep-all":
			cs.WordBreak = d.Value
		}
	case "letter-spacing":
		setSpacingLength(&cs.LetterSpacing, d.Value)
	case "word-spacing":
		setSpacingLength(&cs.WordSpacing, d.Value)
	case "list-style-type":
		cs.ListStyleType = strings.TrimSpace(d.Value)
	case "list-style-position":
		switch d.Value {
		case "outside", "inside":
			cs.ListStylePosition = d.Value
		}
	case "list-style":
		applyListStyleShorthand(cs, d.Value)
	case "counter-reset":
		cs.CounterReset = parseCounterOps(d.Value, 0)
	case "counter-increment":
		cs.CounterIncrement = parseCounterOps(d.Value, 1)
	case "counter-set":
		cs.CounterSet = parseCounterOps(d.Value, 0)
	case "content":
		cs.Content = parseContent(d.Value)
	case "margin-top":
		setLength(&cs.MarginTop, d.Value)
	case "margin-right":
		setLength(&cs.MarginRight, d.Value)
	case "margin-bottom":
		setLength(&cs.MarginBottom, d.Value)
	case "margin-left":
		setLength(&cs.MarginLeft, d.Value)
	case "margin":
		applyBoxLengths(d.Value, parseMarginComponent,
			&cs.MarginTop, &cs.MarginRight, &cs.MarginBottom, &cs.MarginLeft)
	case "padding-top":
		setLength(&cs.PaddingTop, d.Value)
	case "padding-right":
		setLength(&cs.PaddingRight, d.Value)
	case "padding-bottom":
		setLength(&cs.PaddingBottom, d.Value)
	case "padding-left":
		setLength(&cs.PaddingLeft, d.Value)
	case "padding":
		applyBoxLengths(d.Value, parsePaddingComponent,
			&cs.PaddingTop, &cs.PaddingRight, &cs.PaddingBottom, &cs.PaddingLeft)
	case "width":
		setLength(&cs.Width, d.Value)
	case "height":
		setLength(&cs.Height, d.Value)
	case "min-width":
		setLength(&cs.MinWidth, d.Value)
	case "max-width":
		setMaxLength(&cs.MaxWidth, d.Value)
	case "min-height":
		setLength(&cs.MinHeight, d.Value)
	case "max-height":
		setMaxLength(&cs.MaxHeight, d.Value)
	case "box-sizing":
		switch d.Value {
		case "content-box", "border-box":
			cs.BoxSizing = d.Value
		}
	case "object-fit":
		switch d.Value {
		case "fill", "contain", "cover", "none", "scale-down":
			cs.ObjectFit = d.Value
		}
	case "object-position":
		if x, y, ok := parseObjectPosition(d.Value); ok {
			cs.ObjectPositionX, cs.ObjectPositionY = x, y
		}
	case "overflow":
		// The shorthand takes one or two values ("overflow: hidden auto" sets x then
		// y). Every non-visible keyword clips identically in this model, so the
		// stronger of the two wins: "visible auto" must still clip, because a box that
		// clips on either axis clips its content here.
		if v, ok := parseOverflowShorthand(d.Value); ok {
			cs.Overflow = v
		}
	case "text-overflow":
		// Only the two keywords this engine can express. CSS Overflow 4 also allows a
		// custom <string> and a two-value form (start/end); both are rejected here
		// rather than approximated, so the declaration drops and the initial stands.
		switch d.Value {
		case "clip", "ellipsis":
			cs.TextOverflow = d.Value
		}
	case "-webkit-line-clamp", "line-clamp":
		// "none" is the initial (no clamp); a positive integer clamps to that many
		// lines. Zero and negatives are invalid, so the declaration drops.
		if d.Value == "none" {
			cs.LineClamp = 0
			break
		}
		if n, err := strconv.Atoi(strings.TrimSpace(d.Value)); err == nil && n > 0 {
			cs.LineClamp = n
		}
	case "-webkit-box-orient":
		// Accepted so the -webkit-box clamp idiom parses as a whole, but the engine
		// only implements the vertical orientation the idiom always uses; a horizontal
		// value is stored and ignored by layout rather than silently changing it.
		cs.BoxOrient = d.Value
	case "overflow-x", "overflow-y":
		// Modeled as the same single clip flag as the shorthand: this engine has no
		// per-axis clipping, and a box clipping on one axis still needs a clip rect
		// and a BFC. Ignoring them entirely meant "overflow-x: hidden" silently did
		// nothing, which is worse than clipping both axes.
		if v, ok := parseOverflowKeyword(d.Value); ok && clipsOverflow(v) {
			cs.Overflow = v
		}
	case "break-before", "page-break-before":
		switch d.Value {
		case "auto", "avoid", "avoid-page", "page", "always", "left", "right", "recto", "verso":
			cs.BreakBefore = d.Value
		}
	case "break-after", "page-break-after":
		switch d.Value {
		case "auto", "avoid", "avoid-page", "page", "always", "left", "right", "recto", "verso":
			cs.BreakAfter = d.Value
		}
	case "break-inside", "page-break-inside":
		switch d.Value {
		case "auto", "avoid", "avoid-page", "avoid-column", "avoid-region":
			cs.BreakInside = d.Value
		}
	case "page":
		// `page: auto` resets to no named page; any other identifier is a page name.
		if d.Value == "auto" {
			cs.Page = ""
		} else {
			cs.Page = strings.ToLower(d.Value)
		}
	case "string-set":
		cs.StringSet = parseStringSet(d.Value)
	case "widows":
		if n, err := strconv.Atoi(strings.TrimSpace(d.Value)); err == nil && n >= 1 {
			cs.Widows = n
		}
	case "orphans":
		if n, err := strconv.Atoi(strings.TrimSpace(d.Value)); err == nil && n >= 1 {
			cs.Orphans = n
		}
	case "border-collapse":
		switch d.Value {
		case "separate", "collapse":
			cs.BorderCollapse = d.Value
		}
	case "border-spacing":
		applyBorderSpacing(cs, d.Value)
	case "table-layout":
		switch d.Value {
		case "auto", "fixed":
			cs.TableLayout = d.Value
		}
	case "vertical-align":
		switch d.Value {
		case "baseline", "top", "middle", "bottom",
			"sub", "super", "text-top", "text-bottom":
			cs.VerticalAlign = d.Value
		}
	case "caption-side":
		switch d.Value {
		case "top", "bottom":
			cs.CaptionSide = d.Value
		}
	case "empty-cells":
		switch d.Value {
		case "show", "hide":
			cs.EmptyCells = d.Value
		}
	case "direction":
		switch d.Value {
		case "ltr", "rtl":
			cs.Direction = d.Value
		}
	case "writing-mode":
		// lr/lr-tb/rl/rl-tb are the deprecated SVG 1.1 spellings of horizontal-tb;
		// the SVG path accepts them (pkg/svg/style.go applyWritingMode) and the two
		// paths agreeing costs one line. sideways-rl/sideways-lr are NOT accepted:
		// they are distinct modes, and silently folding them into a vertical value
		// would misreport what the engine was asked to do.
		switch d.Value {
		case "horizontal-tb", "lr", "lr-tb", "rl", "rl-tb":
			cs.WritingMode = "horizontal-tb"
		case "vertical-rl", "vertical-lr":
			cs.WritingMode = d.Value
		}
	case "unicode-bidi":
		switch d.Value {
		case "normal", "embed", "isolate", "bidi-override", "isolate-override", "plaintext":
			cs.UnicodeBidi = d.Value
		}
	case "float":
		switch d.Value {
		case "left", "right", "none":
			cs.Float = d.Value
		}
	case "clear":
		switch d.Value {
		case "left", "right", "both", "none":
			cs.Clear = d.Value
		}
	case "position":
		v := strings.TrimSpace(d.Value)
		if name, ok := parseRunning(v); ok {
			cs.Position = "running"
			cs.RunningName = name
		} else {
			switch v {
			case "static", "relative", "absolute", "fixed":
				cs.Position = v
				cs.RunningName = ""
			}
		}
	case "top":
		setLength(&cs.Top, d.Value)
	case "right":
		setLength(&cs.Right, d.Value)
	case "bottom":
		setLength(&cs.Bottom, d.Value)
	case "left":
		setLength(&cs.Left, d.Value)
	case "z-index":
		applyZIndex(cs, d.Value)
	case "border-top-width":
		setLength(&cs.BorderTopWidth, d.Value)
	case "border-right-width":
		setLength(&cs.BorderRightWidth, d.Value)
	case "border-bottom-width":
		setLength(&cs.BorderBottomWidth, d.Value)
	case "border-left-width":
		setLength(&cs.BorderLeftWidth, d.Value)
	case "border-top-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BorderTopColor = c
		}
	case "border-right-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BorderRightColor = c
		}
	case "border-bottom-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BorderBottomColor = c
		}
	case "border-left-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BorderLeftColor = c
		}
	case "border-top-style":
		cs.BorderTopStyle = d.Value
	case "border-right-style":
		cs.BorderRightStyle = d.Value
	case "border-bottom-style":
		cs.BorderBottomStyle = d.Value
	case "border-left-style":
		cs.BorderLeftStyle = d.Value
	case "border-width":
		applyBoxLengths(d.Value, parseBorderWidthComponent,
			&cs.BorderTopWidth, &cs.BorderRightWidth, &cs.BorderBottomWidth, &cs.BorderLeftWidth)
	case "border-style":
		applyBorderStyle(cs, d.Value)
	case "border-color":
		applyBorderColor(cs, d.Value)
	case "border":
		// width||style||color applied to all four sides.
		applyBorderSide(cs, d.Value,
			borderSide{&cs.BorderTopWidth, &cs.BorderTopColor, &cs.BorderTopStyle},
			borderSide{&cs.BorderRightWidth, &cs.BorderRightColor, &cs.BorderRightStyle},
			borderSide{&cs.BorderBottomWidth, &cs.BorderBottomColor, &cs.BorderBottomStyle},
			borderSide{&cs.BorderLeftWidth, &cs.BorderLeftColor, &cs.BorderLeftStyle})
	case "border-top":
		applyBorderSide(cs, d.Value,
			borderSide{&cs.BorderTopWidth, &cs.BorderTopColor, &cs.BorderTopStyle})
	case "border-right":
		applyBorderSide(cs, d.Value,
			borderSide{&cs.BorderRightWidth, &cs.BorderRightColor, &cs.BorderRightStyle})
	case "border-bottom":
		applyBorderSide(cs, d.Value,
			borderSide{&cs.BorderBottomWidth, &cs.BorderBottomColor, &cs.BorderBottomStyle})
	case "border-left":
		applyBorderSide(cs, d.Value,
			borderSide{&cs.BorderLeftWidth, &cs.BorderLeftColor, &cs.BorderLeftStyle})
	case "border-radius":
		applyBorderRadius(cs, d.Value)
	case "border-top-left-radius":
		applyCornerRadius(&cs.BorderTopLeftRadius, d.Value)
	case "border-top-right-radius":
		applyCornerRadius(&cs.BorderTopRightRadius, d.Value)
	case "border-bottom-right-radius":
		applyCornerRadius(&cs.BorderBottomRightRadius, d.Value)
	case "border-bottom-left-radius":
		applyCornerRadius(&cs.BorderBottomLeftRadius, d.Value)
	case "flex-direction":
		switch d.Value {
		case "row", "row-reverse", "column", "column-reverse":
			cs.FlexDirection = d.Value
		}
	case "flex-wrap":
		switch d.Value {
		case "nowrap", "wrap", "wrap-reverse":
			cs.FlexWrap = d.Value
		}
	case "flex-flow":
		// Shorthand for flex-direction + flex-wrap, in either order; either may be
		// omitted, in which case it resets to its initial value (row / nowrap) per the
		// shorthand rules. An unrecognized token invalidates nothing on its own — the
		// recognized components still apply, matching how the other shorthands here
		// degrade.
		dir, wrap := "row", "nowrap"
		for _, tok := range strings.Fields(d.Value) {
			switch tok {
			case "row", "row-reverse", "column", "column-reverse":
				dir = tok
			case "nowrap", "wrap", "wrap-reverse":
				wrap = tok
			}
		}
		cs.FlexDirection, cs.FlexWrap = dir, wrap
	case "justify-content":
		switch d.Value {
		case "flex-start", "flex-end", "center", "space-between", "space-around", "space-evenly",
			"start", "end", "stretch", "normal":
			cs.JustifyContent = d.Value
		}
	case "align-items":
		switch d.Value {
		case "stretch", "flex-start", "flex-end", "center", "baseline",
			"start", "end", "normal":
			cs.AlignItems = d.Value
		}
	case "align-self":
		switch d.Value {
		case "auto", "stretch", "flex-start", "flex-end", "center", "baseline",
			"start", "end", "normal":
			cs.AlignSelf = d.Value
		}
	case "column-gap":
		if l, ok := parseGapLength(d.Value); ok {
			cs.ColumnGap = l
		}
	case "row-gap":
		if l, ok := parseGapLength(d.Value); ok {
			cs.RowGap = l
		}
	case "flex-grow":
		if v, ok := parseNonNegNumber(d.Value); ok {
			cs.FlexGrow = v
		}
	case "flex-shrink":
		if v, ok := parseNonNegNumber(d.Value); ok {
			cs.FlexShrink = v
		}
	case "flex-basis":
		if l, ok := parseFlexBasis(d.Value); ok {
			cs.FlexBasis = l
		}
	case "order":
		if n, ok := parseInt(d.Value); ok {
			cs.Order = n
		}
	case "flex":
		applyFlexShorthand(cs, d.Value)
	case "gap":
		applyGapShorthand(cs, d.Value)
	case "grid-template-columns":
		if tl, ok := parseTrackList(d.Value); ok {
			cs.GridTemplateColumns = tl
		}
		// "none" or "subgrid" leave the zero value (empty list = no explicit tracks).
	case "grid-template-rows":
		if tl, ok := parseTrackList(d.Value); ok {
			cs.GridTemplateRows = tl
		}
	case "grid-template-areas":
		if ga, ok := parseTemplateAreas(d.Value); ok {
			cs.GridTemplateAreas = ga
		}
	case "grid-auto-columns":
		if tl, ok := parseTrackList(d.Value); ok {
			cs.GridAutoColumns = tl.Expand(0)
		}
	case "grid-auto-rows":
		if tl, ok := parseTrackList(d.Value); ok {
			cs.GridAutoRows = tl.Expand(0)
		}
	case "grid-auto-flow":
		if v := normalizeAutoFlow(d.Value); v != "" {
			cs.GridAutoFlow = v
		}
	case "grid-column":
		if s, e, ok := parseGridColumnRow(d.Value); ok {
			cs.GridPlacement.ColStart, cs.GridPlacement.ColEnd = s, e
		}
	case "grid-row":
		if s, e, ok := parseGridColumnRow(d.Value); ok {
			cs.GridPlacement.RowStart, cs.GridPlacement.RowEnd = s, e
		}
	case "grid-area":
		if p, ok := parseGridArea(d.Value); ok {
			cs.GridPlacement = p
		}
	case "justify-items":
		switch d.Value {
		case "start", "end", "center", "stretch", "baseline", "flex-start", "flex-end", "normal":
			cs.JustifyItems = d.Value
		}
	case "justify-self":
		switch d.Value {
		case "auto", "start", "end", "center", "stretch", "baseline", "flex-start", "flex-end", "normal":
			cs.JustifySelf = d.Value
		}
	case "align-content":
		switch d.Value {
		case "start", "end", "center", "space-between", "space-around", "space-evenly", "stretch",
			"flex-start", "flex-end", "normal":
			cs.AlignContent = d.Value
		}
	case "place-items":
		applyPlacePair(cs, d.Value, "align-items", "justify-items")
	case "place-content":
		applyPlacePair(cs, d.Value, "align-content", "justify-content")
	case "place-self":
		applyPlacePair(cs, d.Value, "align-self", "justify-self")
	case "grid-template":
		applyGridTemplate(cs, d.Value)
	case "grid":
		applyGridShorthand(cs, d.Value)
	}
	// default: unsupported property — ignored on purpose.
}

// parseRunning parses a `running(name)` position value, returning the name and ok=true.
// ok is false for any non-running() value. The name is lowercased so element(name)
// references match case-insensitively.
func parseRunning(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "running(") || !strings.HasSuffix(v, ")") {
		return "", false
	}
	name := strings.TrimSpace(v[len("running(") : len(v)-1])
	if name == "" {
		return "", false
	}
	return strings.ToLower(name), true
}

// normalizeAutoFlow canonicalizes a grid-auto-flow value to one of the four valid
// forms: "row", "column", "row dense", "column dense". It is order-insensitive:
// "dense" alone → "row dense"; "dense column"/"column dense" → "column dense";
// "dense row"/"row dense" → "row dense"; "row"/"column" → unchanged.
// Returns "" for any unrecognized value so the caller can skip the assignment.
func normalizeAutoFlow(val string) string {
	fields := splitComponents(strings.TrimSpace(val))
	if len(fields) == 0 || len(fields) > 2 {
		return ""
	}
	hasRow, hasColumn, hasDense := false, false, false
	for _, f := range fields {
		switch strings.ToLower(f) {
		case "row":
			hasRow = true
		case "column":
			hasColumn = true
		case "dense":
			hasDense = true
		default:
			return "" // unrecognized keyword
		}
	}
	// Ambiguous: both row and column not allowed.
	if hasRow && hasColumn {
		return ""
	}
	dir := "row"
	if hasColumn {
		dir = "column"
	}
	if hasDense {
		return dir + " dense"
	}
	return dir
}

// setLength parses val as a length and writes it to dst when valid.
func setLength(dst *Length, val string) {
	if l, ok := parseLength(newTokenizer(val).next()); ok {
		*dst = l
	}
}

// setMaxLength parses val as a max-* length and writes it to dst. The CSS keyword
// "none" (no maximum) is stored as a UnitAuto length; other values parse as
// ordinary lengths. Invalid values leave dst unchanged.
func setMaxLength(dst *Length, val string) {
	if val == "none" {
		*dst = Length{Unit: UnitAuto}
		return
	}
	setLength(dst, val)
}

// setSpacingLength parses a letter-spacing / word-spacing value into dst. Both
// properties are `normal | <length>`, and `normal` is stored as the ZERO length
// rather than a distinct keyword.
//
// That collapse is deliberate and worth stating, because it is the one place these
// properties are not modeled literally. CSS Text 3 defines `normal` as "no
// additional spacing, but the user agent MAY alter the spacing to justify text",
// i.e. it differs from `0` only in whether a justifier is permitted extra latitude
// to stretch between letters. This engine's justification distributes slack at
// inter-word gaps only (inline.Place's ExtraPerSpace), never between letters, so it
// never takes that latitude and `normal` and `0` are indistinguishable in every
// rendering this engine can produce. Modeling `normal` as a separate keyword would
// add a state to every consumer that no consumer could ever branch on.
//
// The practical consequence is that `normal` RESETS an inherited value, which is the
// behavior authors rely on to cancel tracking inherited from an ancestor.
//
// Negative lengths are valid (they tighten) and are passed through unclamped here;
// the layout side floors the resulting per-glyph advance at zero rather than
// rejecting the value, so a large negative tracking overlaps glyphs the way a
// browser does instead of producing negative advances the breaker cannot handle.
// A value that is neither `normal` nor a parsable length leaves dst unchanged, so
// the declaration is dropped and the cascaded/inherited value survives.
func setSpacingLength(dst *Length, val string) {
	if strings.TrimSpace(val) == "normal" {
		*dst = Length{}
		return
	}
	setLength(dst, val)
}

// applyZIndex parses a z-index value: "auto" sets ZIndexAuto; an integer sets
// ZIndex (ZIndexAuto=false). A non-integer value is dropped, leaving the prior
// value. (Parsed now for the cascade; the minimal stacking pass does not yet sort
// on it.)
func applyZIndex(cs *ComputedStyle, val string) {
	if val == "auto" {
		cs.ZIndexAuto = true
		return
	}
	n, ok := parseInt(val)
	if !ok {
		return
	}
	cs.ZIndex, cs.ZIndexAuto = n, false
}

// applyBorderSpacing parses border-spacing: one length sets both axes, two lengths
// set horizontal then vertical. Percentages/auto are invalid here and dropped. A
// malformed value leaves the prior spacing intact.
func applyBorderSpacing(cs *ComputedStyle, value string) {
	tz := newTokenizer(value)
	var lens []Length
	for {
		tok := tz.next()
		if tok.Kind == TokenEOF {
			break
		}
		if tok.Kind == TokenWhitespace {
			continue
		}
		l, ok := parseLength(tok)
		if !ok || l.Unit == UnitAuto || l.Unit == UnitPercent {
			return // invalid component: drop the whole declaration
		}
		lens = append(lens, l)
	}
	switch len(lens) {
	case 1:
		cs.BorderSpacingH = lens[0].Value
		cs.BorderSpacingV = lens[0].Value
	case 2:
		cs.BorderSpacingH = lens[0].Value
		cs.BorderSpacingV = lens[1].Value
	}
}

// parseInt parses an optionally-signed base-10 integer, returning ok=false for any
// non-integer (including empty, a float, or trailing junk). Used for z-index.
func parseInt(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	neg := false
	i := 0
	if s[0] == '+' || s[0] == '-' {
		neg = s[0] == '-'
		i = 1
		if i == len(s) {
			return 0, false
		}
	}
	n := 0
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	if neg {
		n = -n
	}
	return n, true
}

// cleanFamilyList normalizes a font-family value into a comma-joined fallback
// list, preserving order so the face resolver can try each candidate in turn
// (e.g. `"Helvetica Neue", Arial , sans-serif` -> `Helvetica Neue, Arial, sans-serif`).
// Each name is unquoted and whitespace-trimmed; empty entries are dropped. The
// raw value is returned only if it contains no usable name.
func cleanFamilyList(val string) string {
	parts := splitComma(val)
	cleaned := parts[:0]
	for _, part := range parts {
		if part = unquote(strings.TrimSpace(part)); part != "" {
			cleaned = append(cleaned, part)
		}
	}
	if len(cleaned) == 0 {
		return val
	}
	return strings.Join(cleaned, ", ")
}

// WhiteSpaceFlags decomposes a CSS white-space value into its three independent
// behaviors (CSS Text §3): whether runs of spaces/tabs collapse to one space,
// whether newlines are preserved (as forced line breaks) rather than collapsed, and
// whether lines wrap at the available width. An empty or unrecognized value maps to
// "normal" (collapse spaces, collapse newlines, wrap) — the engine's prior behavior.
//
//	value     collapseSpaces  preserveNewlines  wrap
//	normal    true            false             true
//	nowrap    true            false             false
//	pre       false           true              false
//	pre-wrap  false           true              true
//	pre-line  true            true              true
func WhiteSpaceFlags(ws string) (collapseSpaces, preserveNewlines, wrap bool) {
	switch ws {
	case "nowrap":
		return true, false, false
	case "pre":
		return false, true, false
	case "pre-wrap":
		return false, true, true
	case "pre-line":
		return true, true, true
	default: // "normal" and any unknown value
		return true, false, true
	}
}

// splitComma splits a comma-separated CSS value list (e.g. a font-family list).
func splitComma(s string) []string { return strings.Split(s, ",") }

// parseNonNegNumber parses a unitless non-negative number (flex-grow/flex-shrink).
// A negative or non-numeric value yields ok=false (the property keeps its prior value).
func parseNonNegNumber(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

// parseGapLength parses a row-gap/column-gap value. "normal" is the initial value
// and means zero gap. Lengths/percentages parse normally; "auto" is invalid for gap.
func parseGapLength(s string) (Length, bool) {
	if strings.TrimSpace(s) == "normal" {
		return Length{0, UnitPx}, true
	}
	l, ok := parseLength(newTokenizer(s).next())
	if !ok || l.Unit == UnitAuto {
		return Length{}, false
	}
	return l, true
}

// parseFlexBasis parses a flex-basis value: "auto", "content", or a length/percentage.
func parseFlexBasis(s string) (Length, bool) {
	switch strings.TrimSpace(s) {
	case "auto":
		return Length{Unit: UnitAuto}, true
	case "content":
		return Length{Unit: UnitContent}, true
	}
	l, ok := parseLength(newTokenizer(s).next())
	if !ok || l.Unit == UnitAuto {
		return Length{}, false
	}
	return l, true
}
