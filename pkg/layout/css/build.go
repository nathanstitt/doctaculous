// Package css is the box-generation stage: it walks an html.Document, drives the
// pkg/css cascade per element, and emits a cssbox tree. Box generation stores the
// computed style on each box and normalizes the tree with anonymous-box fixups,
// so the layout engine receives a well-formed tree (a block container's children
// are either all block-level or all inline-level). It produces no pixels.
package css

import (
	"context"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// replacedTags are elements treated as replaced content (leaf boxes carrying
// their attributes; no decoded media in this sub-project).
var replacedTags = map[string]bool{"img": true}

// Build generates a cssbox tree from a parsed HTML document. loader resolves
// <link rel=stylesheet> refs (may be nil → links skipped); logf receives
// degradation messages (may be nil). It never panics on malformed input: a
// recover at the entry boundary returns whatever tree was built so far.
func Build(ctx context.Context, doc *html.Document, loader resource.ResourceLoader, logf func(string, ...any)) (root *cssbox.Box, err error) {
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

	sheets := assembleSheets(ctx, doc, loader, logf)
	resolver := gcss.NewResolver(sheets, logf)

	root = generate(doc.Root, resolver, resolver.ComputeRoot(doc.Root))
	normalize(root) // anonymous-box fixups + whitespace handling (anon.go)
	return root, nil
}

// assembleSheets returns the origin-ordered sheets: the UA sheet first, then the
// document's <style> sheets and any resolvable <link> sheets (all author).
func assembleSheets(ctx context.Context, doc *html.Document, loader resource.ResourceLoader, logf func(string, ...any)) []gcss.OriginSheet {
	sheets := []gcss.OriginSheet{{Sheet: html.UAStylesheet, Origin: gcss.OriginUA}}
	for _, s := range doc.StyleSheets {
		sheets = append(sheets, gcss.OriginSheet{Sheet: s, Origin: gcss.OriginAuthor})
	}
	if loader != nil {
		for _, ref := range doc.LinkRefs {
			data, _, err := loader.Load(ctx, ref)
			if err != nil {
				logf("link stylesheet %q: %v (skipped)", ref, err)
				continue
			}
			sheets = append(sheets, gcss.OriginSheet{Sheet: gcss.Parse(string(data)), Origin: gcss.OriginAuthor})
		}
	}
	return sheets
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
func makeTextBox(data string, parent gcss.ComputedStyle) *cssbox.Box {
	if data == "" {
		return nil
	}
	style := parent
	style.Display = "inline"
	return &cssbox.Box{Kind: cssbox.BoxText, Text: data, Style: style, Display: cssbox.DisplayInline}
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

// floatOf maps a computed style to a FloatKind. The float property is not on
// ComputedStyle's normal-flow subset yet, so this returns FloatNone until float
// support lands in pkg/css.
func floatOf(_ gcss.ComputedStyle) cssbox.FloatKind { return cssbox.FloatNone }

// positionOf maps a computed style to a PositionKind. Like floatOf, position is
// not yet on ComputedStyle's subset; returns PosStatic until it is.
func positionOf(_ gcss.ComputedStyle) cssbox.PositionKind { return cssbox.PosStatic }

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
	case "table-row":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableRow, cssbox.TableFC
	case "table-cell":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableCell, cssbox.BlockFC
	case "flex":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayFlex, cssbox.FlexFC
	case "grid":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayGrid, cssbox.GridFC
	case "block":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayBlock, cssbox.BlockFC
	default:
		// unknown display value -> block normal flow.
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayBlock, cssbox.BlockFC
	}
}
