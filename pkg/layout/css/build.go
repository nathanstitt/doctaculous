// Package css is the box-generation stage: it walks an html.Document, drives the
// pkg/css cascade per element, and emits a cssbox tree. Box generation stores the
// computed style on each box and normalizes the tree with anonymous-box fixups,
// so the layout engine receives a well-formed tree (a block container's children
// are either all block-level or all inline-level). It produces no pixels.
package css

import (
	"context"
	"strconv"
	"strings"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// replacedTags are elements treated as replaced content (leaf boxes carrying
// their attributes; no decoded media in this sub-project).
var replacedTags = map[string]bool{"img": true}

// Build generates a cssbox tree from a parsed HTML document (see BuildWithFonts;
// this form discards the collected @font-face table for callers that do not need
// it). loader resolves <link rel=stylesheet> refs (may be nil → links skipped);
// logf receives degradation messages (may be nil). Signature unchanged for
// existing callers.
func Build(ctx context.Context, doc *html.Document, loader resource.ResourceLoader, logf func(string, ...any)) (*cssbox.Box, error) {
	root, _, err := BuildWithFonts(ctx, doc, loader, logf)
	return root, err
}

// BuildWithFonts is Build plus the aggregated @font-face rules collected from every
// origin sheet (UA + <style> + <link>), so the caller can hand them to the face
// cache. It discards the aggregated @page rules (see BuildWithFontsAndPages). It never
// panics on malformed input: a recover at the entry boundary returns whatever tree was
// built so far (and the faces collected so far).
func BuildWithFonts(ctx context.Context, doc *html.Document, loader resource.ResourceLoader, logf func(string, ...any)) (root *cssbox.Box, faces []gcss.FontFace, err error) {
	root, faces, _, err = BuildWithFontsAndPages(ctx, doc, loader, logf)
	return root, faces, err
}

// BuildWithFontsAndPages is BuildWithFonts plus the aggregated @page rules collected
// from every origin sheet (so the caller can resolve paged-media geometry). The @page
// rules are returned as a single Stylesheet (only its Pages are populated) ready for
// ResolvePage. Like BuildWithFonts it never panics on malformed input.
func BuildWithFontsAndPages(ctx context.Context, doc *html.Document, loader resource.ResourceLoader, logf func(string, ...any)) (root *cssbox.Box, faces []gcss.FontFace, pages gcss.Stylesheet, err error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	defer func() {
		if r := recover(); r != nil {
			logf("box generation recovered from panic: %v", r)
			if root == nil {
				root = &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
			}
			err = nil
		}
	}()

	var sheets []gcss.OriginSheet
	sheets, faces, pages.Pages = assembleSheets(ctx, doc, loader, logf)
	resolver := gcss.NewResolver(sheets, logf)

	root = generate(doc.Root, resolver, resolver.ComputeRoot(doc.Root))
	if root == nil {
		// The root itself computed to display:none (e.g. html{display:none}).
		// Degrade to an empty block root rather than falling through to the
		// panic-recover path via normalize(nil); the result is a renderable
		// empty document.
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC}, faces, pages, nil
	}
	resolveCounters(root) // list-item markers + CSS counter()/counters() content (counters.go)
	normalize(root)       // anonymous-box fixups + whitespace handling (anon.go)
	fixupTables(root)     // anonymous TABLE-box fixups (CSS 17.2.1, tablefix.go)
	fixupFlex(root)       // anonymous FLEX-item fixups (CSS Flexbox 4, flexfix.go)
	fixupGrid(root)       // anonymous GRID-item fixups (CSS Grid §6, gridfix.go)
	return root, faces, pages, nil
}

// assembleSheets returns the origin-ordered sheets AND the aggregated @font-face and
// @page rules across all of them (UA + <style> + resolvable <link>). Faces and pages
// are collected here because this is where every sheet is parsed.
func assembleSheets(ctx context.Context, doc *html.Document, loader resource.ResourceLoader, logf func(string, ...any)) ([]gcss.OriginSheet, []gcss.FontFace, []gcss.PageRule) {
	sheets := []gcss.OriginSheet{{Sheet: html.UAStylesheet, Origin: gcss.OriginUA}}
	var faces []gcss.FontFace
	var pages []gcss.PageRule
	faces = append(faces, html.UAStylesheet.FontFaces...)
	pages = append(pages, html.UAStylesheet.Pages...)
	for _, s := range doc.StyleSheets {
		sheets = append(sheets, gcss.OriginSheet{Sheet: s, Origin: gcss.OriginAuthor})
		faces = append(faces, s.FontFaces...)
		pages = append(pages, s.Pages...)
	}
	if loader != nil {
		for _, ref := range doc.LinkRefs {
			data, _, err := loader.Load(ctx, ref)
			if err != nil {
				logf("link stylesheet %q: %v (skipped)", ref, err)
				continue
			}
			parsed := gcss.Parse(string(data))
			sheets = append(sheets, gcss.OriginSheet{Sheet: parsed, Origin: gcss.OriginAuthor})
			faces = append(faces, parsed.FontFaces...)
			pages = append(pages, parsed.Pages...)
		}
	}
	return sheets, faces, pages
}

// generate recursively builds the box for element e (whose computed style is cs)
// and its descendants. Returns nil for a display:none subtree.
func generate(e *html.Element, r *gcss.Resolver, cs gcss.ComputedStyle) *cssbox.Box {
	if cs.Display == "none" {
		return nil
	}

	b := &cssbox.Box{Style: cs}
	classifyDisplay(b, cs.Display)
	b.Float = floatOf(cs)
	b.Position = positionOf(cs)
	// CSS 9.7: an absolutely/fixed-positioned element computes float to none, so
	// it is taken out of flow as positioned, not placed as a float.
	if b.Position == cssbox.PosAbsolute || b.Position == cssbox.PosFixed {
		b.Float = cssbox.FloatNone
	}
	applyBlockify(b, cs) // CSS 9.7: a float OR an abs/fixed box blockifies an inline-level box

	// HTML presentational span attributes onto table-part boxes (CSS does not carry
	// these). colspan/rowspan apply to a cell; <col span>/<colgroup span> reuse ColSpan.
	switch b.Display {
	case cssbox.DisplayTableCell:
		b.ColSpan = attrSpan(e, "colspan")
		b.RowSpan = attrSpan(e, "rowspan")
	case cssbox.DisplayTableColumn, cssbox.DisplayTableColumnGroup:
		b.ColSpan = attrSpan(e, "span")
	}

	if kind, skip := classifyControl(e.Tag(), elemAttrs(e)); kind != cssbox.CtrlNone || skip {
		if skip {
			return nil // <input type=hidden>: no box
		}
		b.Kind = cssbox.BoxReplaced
		b.Replaced = &cssbox.ReplacedContent{
			Tag:     e.Tag(),
			Attrs:   controlAttrSnapshot(e),
			Control: kind,
			Text:    controlText(e, kind),
		}
		return b // controls are leaves — no child boxes (prevents text leakage)
	}

	if replacedTags[e.Tag()] {
		b.Kind = cssbox.BoxReplaced
		b.Replaced = &cssbox.ReplacedContent{Tag: e.Tag(), Attrs: attrSnapshot(e)}
		return b // replaced elements are leaves
	}

	for _, child := range e.Children() {
		switch c := child.(type) {
		case *html.Element:
			childCS := r.Compute(c, cs)
			if cb := generate(c, r, childCS); cb != nil {
				b.Children = append(b.Children, cb)
			}
		case *html.Text:
			if t := makeTextBox(c.Data, cs); t != nil {
				b.Children = append(b.Children, t)
			}
		}
	}
	return b
}

// makeTextBox creates a text box for raw character data, or nil if the data is
// empty. Whitespace collapsing/stripping is applied during normalization
// (Task 9); here we keep the raw text but skip a wholly-empty string. The text
// inherits the parent's computed style for font/color, but a text run is always
// inline-level, so its carried Display is forced to "inline" rather than the
// parent element's (display is not a CSS-inherited property).
//
// Only the inherited (font/color/line-height/text-align) fields of the carried
// Style are meaningful on a text box; the parent's box-level fields (width,
// margins, borders) are copied along but have no meaning for a text leaf and
// should not be read by the layout engine.
//
// The non-inherited counter properties (counter-reset/increment/set and content)
// are explicitly cleared: they are box-level and belong to the parent element, not
// to its text. Leaving them would make the counter pass (resolveCounters) re-apply
// the parent's counter-reset for every text node — e.g. a whitespace node between
// <li>s would reset list-item, restarting list numbering at each item.
func makeTextBox(data string, parent gcss.ComputedStyle) *cssbox.Box {
	if data == "" {
		return nil
	}
	style := parent
	style.Display = "inline"
	style.CounterReset = nil
	style.CounterIncrement = nil
	style.CounterSet = nil
	style.Content = nil
	return &cssbox.Box{Kind: cssbox.BoxText, Text: data, Style: style, Display: cssbox.DisplayInline}
}

// attrSpan reads an HTML span attribute (colspan/rowspan/span) as a positive
// integer, defaulting to 1 when absent, non-numeric, or < 1 (HTML clamps these to
// at least 1). The box stores the resolved value (never 0) on a span-bearing box.
func attrSpan(e *html.Element, name string) int {
	v, ok := e.Attr(name)
	if !ok {
		return 1
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// elemAttrs returns the attributes classifyControl consults (currently just type).
func elemAttrs(e *html.Element) map[string]string {
	m := map[string]string{}
	if v, ok := e.Attr("type"); ok {
		m["type"] = v
	}
	return m
}

// controlAttrSnapshot copies the attributes a form control's sizing/paint consults.
func controlAttrSnapshot(e *html.Element) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"type", "value", "placeholder", "checked", "disabled",
		"size", "cols", "rows", "width", "height"} {
		if v, ok := e.Attr(k); ok {
			out[k] = v
		}
	}
	return out
}

// attrSnapshot copies the relevant attributes of a replaced element.
func attrSnapshot(e *html.Element) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"src", "alt", "width", "height"} {
		if v, ok := e.Attr(k); ok {
			out[k] = v
		}
	}
	return out
}

// floatOf maps a computed style's float keyword to a FloatKind. An empty or
// unrecognized value is FloatNone.
func floatOf(cs gcss.ComputedStyle) cssbox.FloatKind {
	switch cs.Float {
	case "left":
		return cssbox.FloatLeft
	case "right":
		return cssbox.FloatRight
	default:
		return cssbox.FloatNone
	}
}

// applyBlockify promotes an inline-level box to block-level when it is floated or
// absolutely/fixed positioned, per CSS 2.1 §9.7 (both compute display to a
// block-level value). Only a box that is still inline-level when this runs is
// promoted — in practice a display:inline element (Kind=BoxInline) and the inline
// anonymous/text kinds; a display:block or display:inline-block element already has
// Kind=BoxBlock (set by classifyDisplay) and is left unchanged. The box's Formatting
// is deliberately preserved: a floated/positioned display:inline element keeps
// InlineFC so its text/inline children still lay out in an inline formatting context
// inside the now block-level box. A floated/positioned <img> stays BoxReplaced —
// replaced sizing handles a block-level replaced box.
//
// It re-reads cs.Float (not b.Float) so it stays a pure function of the computed
// style: even though generate has already cleared b.Float to none for an abs/fixed
// box (a layout concern), the box should still be block-level here, which the
// absPos branch ensures independently of the now-cleared float.
//
// The BoxReplaced guard is defensive: in generate, the replacedTags override sets
// Kind=BoxReplaced AFTER this call, so a box is never BoxReplaced when applyBlockify
// runs today; the guard protects any future caller that inverts that order.
func applyBlockify(b *cssbox.Box, cs gcss.ComputedStyle) {
	floated := floatOf(cs) != cssbox.FloatNone
	posKind := positionOf(cs)
	absPos := posKind == cssbox.PosAbsolute || posKind == cssbox.PosFixed
	if !floated && !absPos {
		return
	}
	if b.Kind == cssbox.BoxReplaced {
		return // replaced stays replaced; replaced sizing handles block-level
	}
	if b.Kind.IsInlineLevel() {
		b.Kind, b.Display = cssbox.BoxBlock, cssbox.DisplayBlock
	}
}

// positionOf maps a computed style's position keyword to a PositionKind. An empty
// or unrecognized value is PosStatic.
func positionOf(cs gcss.ComputedStyle) cssbox.PositionKind {
	switch cs.Position {
	case "relative":
		return cssbox.PosRelative
	case "absolute":
		return cssbox.PosAbsolute
	case "fixed":
		return cssbox.PosFixed
	default:
		return cssbox.PosStatic
	}
}

// classifyDisplay sets the box's Kind, Display, and Formatting from a computed
// display string. Recognized layout modes not yet implemented (flex/grid/table)
// keep their true DisplayKind/FormattingContext; the layout engine does the
// block fallback later. Genuinely unknown values normalize to block.
func classifyDisplay(b *cssbox.Box, display string) {
	switch display {
	case "inline":
		b.Kind, b.Display, b.Formatting = cssbox.BoxInline, cssbox.DisplayInline, cssbox.InlineFC
	case "inline-block":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayInlineBlock, cssbox.BlockFC
	case "list-item":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayListItem, cssbox.BlockFC
	case "table":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTable, cssbox.TableFC
	case "table-row-group":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableRowGroup, cssbox.TableFC
	case "table-header-group":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableHeaderGroup, cssbox.TableFC
	case "table-footer-group":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableFooterGroup, cssbox.TableFC
	case "table-column":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableColumn, cssbox.TableFC
	case "table-column-group":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableColumnGroup, cssbox.TableFC
	case "table-caption":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableCaption, cssbox.BlockFC
	case "table-row":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableRow, cssbox.TableFC
	case "table-cell":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableCell, cssbox.BlockFC
	case "flex":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayFlex, cssbox.FlexFC
	case "inline-flex":
		// BoxBlock interior, inline-level outer: isBlockLevelOuter + gatherInlineRuns
		// treat it as an atom like inline-block, but with FlexFC interior layout.
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayInlineFlex, cssbox.FlexFC
	case "grid":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayGrid, cssbox.GridFC
	case "inline-grid":
		// BoxBlock interior, inline-level outer: isBlockLevelOuter + gatherInlineRuns
		// treat it as an atom like inline-block, but with GridFC interior layout.
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayInlineGrid, cssbox.GridFC
	case "block":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayBlock, cssbox.BlockFC
	default:
		// unknown display value -> block normal flow.
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayBlock, cssbox.BlockFC
	}
}
