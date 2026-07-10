// Package pptxwrite renders a cssbox tree (the shared box model produced by
// the HTML, DOCX, Markdown, and PDF-extraction frontends) to a PresentationML
// (.pptx) deck. Like the Markdown, DOCX, and RTF writers it is a STRUCTURE
// writer: it walks the box tree directly and emits native PowerPoint
// constructs. It is deliberately not layout-faithful — a document reflows into
// slides, it is not paginated onto them.
//
// The slide mapping: every <h1>/<h2> starts a NEW slide with that heading as
// the title placeholder; the blocks that follow become the slide's body — one
// text box accumulating paragraphs and (nested, bullet/numbered) list items,
// with each table emitted as a native a:tbl graphic frame and each image as a
// p:pic media part. Content before the first heading yields an untitled
// slide. Every mapping is chosen so this repo's own PPTX reader (pkg/pptx)
// round-trips it: titles on the title placeholder, list kind/depth on
// buChar/buAutoNum + lvl, emphasis on rPr b/i, table spans on
// gridSpan/rowSpan + hMerge/vMerge continuation cells.
//
// Documented degrades (logged): h3–h6 become bold body paragraphs (PPTX has
// one title kind per slide); blockquote/code semantics flatten to plain
// paragraphs (no PPTX construct); hyperlink targets drop (text kept); hr is
// skipped; over-tall slides overflow their frame (clipped visually, content
// preserved for conversion).
package pptxwrite

import (
	"context"
	"fmt"
	"io"
	"strings"

	// The image decoders imageRun's DecodeConfig relies on, registered here so
	// the writer is self-sufficient.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// Options configures PPTX output.
type Options struct {
	// SlideWidthPt, SlideHeightPt set the slide size in points; default 16:9
	// (960x540, PowerPoint's default) when zero.
	SlideWidthPt, SlideHeightPt float64
	// Loader resolves image refs (an <img> src) to bytes for embedding as
	// media parts. data: URIs decode without a loader; for the rest, nil means
	// images degrade to their alt text (logged).
	Loader resource.ResourceLoader
	// Logf receives degradation diagnostics (e.g. a construct the writer
	// cannot represent). nil -> no-op.
	Logf func(string, ...any)
}

// Write renders the cssbox tree rooted at root to w as a complete .pptx
// package. root is expected to be a finalized tree (post box-generation
// fixups). A nil root writes a valid one-empty-slide deck.
func Write(ctx context.Context, root *cssbox.Box, w io.Writer, opts Options) error {
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	if opts.SlideWidthPt <= 0 || opts.SlideHeightPt <= 0 {
		opts.SlideWidthPt, opts.SlideHeightPt = 960, 540
	}
	wr := &writer{opts: opts, ctx: ctx, media: map[string]int{}}
	if root != nil {
		if err := wr.block(ctx, root); err != nil {
			return err
		}
	}
	wr.flushText()
	if len(wr.slides) == 0 {
		wr.slides = append(wr.slides, &slideAcc{})
	}
	pkg, err := wr.assemble()
	if err != nil {
		return fmt.Errorf("pptxwrite: %w", err)
	}
	if _, err := w.Write(pkg); err != nil {
		return fmt.Errorf("pptxwrite: %w", err)
	}
	return nil
}

// writer accumulates the deck model during the walk.
type writer struct {
	opts Options
	// ctx is the Write call's context, held for the walk-driven image fetches
	// (the CollectRuns callback has no context parameter). The walk is
	// single-goroutine and the writer does not outlive Write.
	ctx context.Context

	slides []*slideAcc
	// paras accumulates the pending body text shape's paragraphs.
	paras []para
	// pendingPics are picture shapes queued by the inline-image callback,
	// emitted after the text shape that references them.
	pendingPics []picShape

	// media dedupes embedded images: src ref -> media index.
	media      map[string]int
	mediaParts []mediaPart
	// mediaSizes holds each media part's intrinsic pixel size, parallel to
	// mediaParts.
	mediaSizes [][2]int
}

// slideAcc is one slide being built.
type slideAcc struct {
	// title is the slide's title paragraph runs (nil = untitled slide).
	title []boxwalk.StyledRun
	// shapes are the body shapes in reading order.
	shapes []any // textShape | tableShape | picShape
}

// textShape is one body text box.
type textShape struct{ paras []para }

// para is one a:p: its indent level, bullet kind ("", "char", "auto"),
// alignment, and runs.
type para struct {
	level  int
	bullet string
	align  string
	runs   []boxwalk.StyledRun
}

// tableShape is one a:tbl grid (from the occupancy grid, native spans).
type tableShape struct{ grid boxwalk.Grid }

// picShape is one embedded picture.
type picShape struct {
	mediaIdx int
	alt      string
	pxW, pxH int
}

// mediaPart is one ppt/media/* part.
type mediaPart struct {
	name string // e.g. "image1.png"
	data []byte
}

// cur returns the slide being built, opening an untitled one if none is.
func (w *writer) cur() *slideAcc {
	if len(w.slides) == 0 {
		w.slides = append(w.slides, &slideAcc{})
	}
	return w.slides[len(w.slides)-1]
}

// flushText closes the pending body paragraphs into a text shape (and drains
// the pictures they queued).
func (w *writer) flushText() {
	if len(w.paras) > 0 {
		s := w.cur()
		s.shapes = append(s.shapes, textShape{paras: w.paras})
		w.paras = nil
	}
	if len(w.pendingPics) > 0 {
		s := w.cur()
		for _, p := range w.pendingPics {
			s.shapes = append(s.shapes, p)
		}
		w.pendingPics = nil
	}
}

// block dispatches one block-level box, mirroring the other structure
// writers' walk.
func (w *writer) block(ctx context.Context, b *cssbox.Box) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch {
	case b.HeadingLvl == 1 || b.HeadingLvl == 2:
		// A new slide, this heading as its title.
		w.flushText()
		w.slides = append(w.slides, &slideAcc{title: w.inlineRuns(b)})
	case b.HeadingLvl >= 3 && b.HeadingLvl <= 6:
		// No sub-title concept: a bold body paragraph stands in.
		w.opts.Logf("pptxwrite: heading level %d becomes a bold body paragraph", b.HeadingLvl)
		runs := w.inlineRuns(b)
		for i := range runs {
			runs[i].Bold = true
		}
		w.addPara(para{bullet: "", align: alignOf(b), runs: runs})
	case b.SemTag == "blockquote":
		w.opts.Logf("pptxwrite: blockquote flattens to plain paragraphs")
		return w.children(ctx, b)
	case b.SemTag == "pre":
		w.codeBlock(b)
	case b.SemTag == "hr":
		w.opts.Logf("pptxwrite: hr has no slide construct; skipped")
	case b.Display == cssbox.DisplayTable:
		return w.table(b)
	case boxwalk.IsListContainer(b):
		return w.list(ctx, b, 0)
	case b.Display == cssbox.DisplayListItem:
		return w.list(ctx, &cssbox.Box{Children: []*cssbox.Box{b}}, 0)
	case boxwalk.IsBlockContainer(b):
		if boxwalk.HasInlineContent(b) {
			w.addPara(para{align: alignOf(b), runs: w.inlineRuns(b)})
		} else {
			return w.children(ctx, b)
		}
	default:
		return w.children(ctx, b)
	}
	return nil
}

// children recurses over a box's block-level children.
func (w *writer) children(ctx context.Context, b *cssbox.Box) error {
	for _, c := range b.Children {
		if err := w.block(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

// addPara appends a body paragraph (dropping an entirely empty one).
func (w *writer) addPara(p para) {
	if len(p.runs) == 0 {
		return
	}
	w.paras = append(w.paras, p)
}

// codeBlock emits a <pre> box one paragraph per line (PPTX has no code-block
// construct; the text content is what survives — logged).
func (w *writer) codeBlock(b *cssbox.Box) {
	w.opts.Logf("pptxwrite: code block flattens to plain paragraphs")
	text := strings.TrimRight(boxwalk.RawText(b), "\n")
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		w.addPara(para{runs: []boxwalk.StyledRun{{Text: line}}})
	}
}

// list emits a list container's items as bulleted/numbered paragraphs, depth
// being the a:pPr lvl.
func (w *writer) list(ctx context.Context, container *cssbox.Box, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	bullet := "char"
	if containerIsOrdered(container) {
		bullet = "auto"
	}
	for _, item := range container.Children {
		if item.Display != cssbox.DisplayListItem {
			continue
		}
		runs := w.inlineRuns(boxwalk.WithoutNestedLists(item))
		runs = stripMarkerRuns(runs, item.Marker)
		if checked, ok := boxwalk.LeadingCheckbox(item); ok {
			box := "☐"
			if checked {
				box = "☒"
			}
			runs = append([]boxwalk.StyledRun{{Text: box + " "}}, runs...)
		}
		w.addPara(para{level: depth, bullet: bullet, runs: runs})
		for _, c := range item.Children {
			if boxwalk.IsListContainer(c) {
				if err := w.list(ctx, c, depth+1); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// table closes the pending text and appends a native table shape. A caption
// becomes a bold body paragraph above it (the Markdown writer's
// bold-caption-line shape).
func (w *writer) table(b *cssbox.Box) error {
	if cap := captionOf(b); cap != nil {
		runs := w.inlineRuns(cap)
		for i := range runs {
			runs[i].Bold = true
		}
		w.addPara(para{align: alignOf(cap), runs: runs})
	}
	grid := boxwalk.BuildOccupancyGrid(b)
	if grid.Cols == 0 || len(grid.Slots) == 0 {
		return nil
	}
	w.flushText()
	s := w.cur()
	s.shapes = append(s.shapes, tableShape{grid: grid})
	return nil
}

// inlineRuns collects and normalizes a box's inline content into styled runs
// (whitespace-collapsed, coalesced, ends trimmed). Inline images queue
// picture shapes via the callback and contribute no text.
func (w *writer) inlineRuns(b *cssbox.Box) []boxwalk.StyledRun {
	var raw []boxwalk.StyledRun
	boxwalk.CollectRuns(b, boxwalk.InlineState{}, w.imageRun, &raw)
	raw = boxwalk.Coalesce(raw)

	runs := raw[:0]
	for _, r := range raw {
		if r.Literal != "" {
			// The image callback returns alt text (or "") as a literal; it is
			// plain text here, not markup.
			r.Text, r.Literal = r.Literal, ""
		} else {
			r.Text = collapseWS(r.Text)
		}
		if r.Text == "" {
			continue
		}
		if r.Href != "" {
			// pkg/pptx does not model hyperlinks; the text survives.
			w.opts.Logf("pptxwrite: hyperlink target %q dropped (text kept)", r.Href)
			r.Href = ""
		}
		runs = append(runs, r)
	}
	if len(runs) > 0 {
		runs[0].Text = strings.TrimLeft(runs[0].Text, " ")
		last := len(runs) - 1
		runs[last].Text = strings.TrimRight(runs[last].Text, " ")
		if runs[last].Text == "" {
			runs = runs[:last]
		}
	}
	return runs
}

// alignOf maps the box's computed text-align to the model's alignment name.
func alignOf(b *cssbox.Box) string {
	switch b.Style.TextAlign {
	case "center", "right", "justify":
		return b.Style.TextAlign
	}
	return ""
}

// containerIsOrdered reports whether a list container holds ordered items,
// judged from the first item's resolved marker text.
func containerIsOrdered(container *cssbox.Box) bool {
	for _, item := range container.Children {
		if item.Display != cssbox.DisplayListItem {
			continue
		}
		if item.Marker != nil {
			return boxwalk.IsOrderedMarker(strings.TrimSpace(item.Marker.Text))
		}
		return false
	}
	return false
}

// stripMarkerRuns removes the item's resolved marker text from the front of
// its runs (bullet properties re-synthesize it).
func stripMarkerRuns(runs []boxwalk.StyledRun, marker *cssbox.MarkerContent) []boxwalk.StyledRun {
	if marker == nil || len(runs) == 0 {
		return runs
	}
	m := strings.TrimSpace(marker.Text)
	if m == "" {
		return runs
	}
	first := strings.TrimLeft(runs[0].Text, " ")
	if !strings.HasPrefix(first, m) {
		return runs
	}
	runs[0].Text = strings.TrimLeft(strings.TrimPrefix(first, m), " ")
	if runs[0].Text == "" {
		return runs[1:]
	}
	return runs
}

// captionOf returns the table's caption box, if any.
func captionOf(table *cssbox.Box) *cssbox.Box {
	for _, c := range table.Children {
		if c.Display == cssbox.DisplayTableCaption {
			return c
		}
	}
	return nil
}

// collapseWS maps every whitespace run to a single space, keeping at most one
// leading/trailing space so word boundaries between adjacent runs survive.
func collapseWS(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' {
			inSpace = true
			continue
		}
		if inSpace {
			sb.WriteByte(' ')
			inSpace = false
		}
		sb.WriteRune(r)
	}
	if inSpace {
		sb.WriteByte(' ')
	}
	return sb.String()
}
