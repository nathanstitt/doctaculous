package pdfwrite

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/filtereffects"
	"github.com/nathanstitt/omnidoc/pkg/layout"
	"github.com/nathanstitt/omnidoc/pkg/layout/paint"
	"github.com/nathanstitt/omnidoc/pkg/render"
)

// filteredRectPage is a layout page holding one filled rectangle wrapped in a CSS
// filter bracket, plus the same rectangle unwrapped for comparison.
func filteredRectPage(filtered bool) *layout.Page {
	rule := layout.RuleItem{XPt: 10, YPt: 10, WPt: 40, HPt: 20, Color: color.RGBA{R: 0x33, G: 0x66, B: 0x99, A: 0xff}}
	if !filtered {
		return &layout.Page{WidthPt: 100, HeightPt: 50, Items: []layout.Item{
			{Kind: layout.BackgroundKind, Rule: rule},
		}}
	}
	return &layout.Page{WidthPt: 100, HeightPt: 50, Items: []layout.Item{
		{Kind: layout.FilterPushKind, Filter: layout.FilterItem{
			Funcs: []filtereffects.Function{{Kind: filtereffects.FuncBlur, StdDeviation: 4}},
			XPt:   10, YPt: 10, WPt: 40, HPt: 20,
		}},
		{Kind: layout.BackgroundKind, Rule: rule},
		{Kind: layout.FilterPopKind},
	}}
}

// TestFilteredContentStaysVectorInPDF is the PDF-side proof of the documented
// degradation: this writer has no offscreen raster surface (RenderOffscreen returns
// nil), so a CSS filter cannot run its pixel math. The content must still PAINT, at
// the right coordinates, and it must stay VECTOR — no image XObject may appear.
//
// Rasterizing the filtered region into a bitmap would produce the visual effect at
// the cost of turning a vector PDF into a picture of one. That trade is deliberately
// refused; see logFilterDegradation.
func TestFilteredContentStaysVectorInPDF(t *testing.T) {
	dev := newPageDevice(100, 50)
	paint.PaintPage(dev, filteredRectPage(true), render.Scale(1, 1))

	if len(dev.images) != 0 {
		t.Errorf("filtered content recorded %d image XObject(s), want 0 (PDF output must stay vector)", len(dev.images))
	}
	content := decompress(t, dev.contentStream())
	// The rectangle's own fill operators must be present: the group's Form XObject
	// carries them, and the outer stream paints that form.
	if !bytes.Contains(content, []byte("Do")) {
		t.Errorf("content stream has no form paint (`Do`); the filtered content vanished:\n%s", content)
	}
	if len(dev.forms) != 1 {
		t.Fatalf("forms recorded = %d, want 1 (the filter group's transparency form)", len(dev.forms))
	}
	// The group form itself must carry the fill, at the source coordinates.
	form := string(dev.forms[0].content)
	// The painter emits a rect as an explicit path (moveto/lineto/close/fill) rather
	// than `re`, so assert on the actual corner coordinates and the fill operator.
	for _, want := range []string{"10 10 m", "50 10 l", "50 30 l", "10 30 l", "h", "f"} {
		if !strings.Contains(form, want) {
			t.Errorf("group form missing %q (the rect's own operators):\n%s", want, form)
		}
	}
	if len(dev.forms[0].images) != 0 {
		t.Errorf("the filter group recorded %d image(s), want 0", len(dev.forms[0].images))
	}
}

// TestFilteredAndUnfilteredPaintTheSameGeometry: the chain never RUNS on this
// backend (RenderOffscreen declines), so the filtered page's painted geometry must
// match the unfiltered one's exactly — the bracket adds a group wrapper, never a
// coordinate shift. This is what makes "the content is present and correctly
// placed" in the degradation log a checked claim rather than an assurance.
func TestFilteredAndUnfilteredPaintTheSameGeometry(t *testing.T) {
	plain := newPageDevice(100, 50)
	paint.PaintPage(plain, filteredRectPage(false), render.Scale(1, 1))
	wrapped := newPageDevice(100, 50)
	paint.PaintPage(wrapped, filteredRectPage(true), render.Scale(1, 1))

	plainOps := decompress(t, plain.contentStream())
	// The wrapped page's rect operators live inside the group form, not the page
	// stream, so compare the form's body against the unwrapped page stream.
	if len(wrapped.forms) != 1 {
		t.Fatalf("forms recorded = %d, want 1", len(wrapped.forms))
	}
	if got, want := strings.TrimSpace(string(wrapped.forms[0].content)), strings.TrimSpace(string(plainOps)); got != want {
		t.Errorf("filtered geometry differs from unfiltered:\n got: %s\nwant: %s", got, want)
	}
}

// TestFilterDegradationLogsOnce: the writer must SAY that the filter was dropped
// rather than implying it away, and must say it once per document rather than once
// per page.
func TestFilterDegradationLogsOnce(t *testing.T) {
	page := *filteredRectPage(true)
	pages := &layout.Pages{Pages: []layout.Page{page, page, page}}
	var logs []string
	opts := Options{
		PageWidthPt: 100, PageHeightPt: 50, MarginPt: 0,
		Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) },
	}
	var buf bytes.Buffer
	if err := WriteDocument(context.Background(), &buf, pages, opts); err != nil {
		t.Fatalf("WriteDocument: %v", err)
	}
	n := 0
	for _, m := range logs {
		if strings.Contains(m, "CSS filter painted UNFILTERED") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("filter degradation logged %d times, want exactly 1 (logs: %v)", n, logs)
	}
}

// TestUnfilteredDocumentDoesNotLog: the degradation notice must not fire for a
// document that uses no filter — the guard against a warning on every PDF.
func TestUnfilteredDocumentDoesNotLog(t *testing.T) {
	pages := &layout.Pages{Pages: []layout.Page{*filteredRectPage(false)}}
	var logs []string
	opts := Options{
		PageWidthPt: 100, PageHeightPt: 50, MarginPt: 0,
		Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) },
	}
	var buf bytes.Buffer
	if err := WriteDocument(context.Background(), &buf, pages, opts); err != nil {
		t.Fatalf("WriteDocument: %v", err)
	}
	for _, m := range logs {
		if strings.Contains(m, "CSS filter") {
			t.Errorf("unfiltered document logged a filter degradation: %q", m)
		}
	}
}

// TestRenderOffscreenStillDeclines pins the capability this degradation rests on: if
// pdfwrite ever GAINS an offscreen surface, this test fails and the degradation path
// (and its log) must be revisited rather than left silently dead.
func TestRenderOffscreenStillDeclines(t *testing.T) {
	dev := newPageDevice(100, 50)
	if img := dev.RenderOffscreen(image.Pt(10, 10), func(render.Device) {}); img != nil {
		t.Error("pdfwrite.RenderOffscreen returned a surface; the filter degradation path is now stale")
	}
}
