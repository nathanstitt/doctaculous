package paint

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/internal/filtereffects"
	"github.com/nathanstitt/omnidoc/pkg/internal/layout"
	"github.com/nathanstitt/omnidoc/pkg/internal/raster"
	"github.com/nathanstitt/omnidoc/pkg/internal/render"
)

// captureLogf returns a Logf that appends each formatted line to lines, so a
// test can assert on what was actually EMITTED rather than merely on the code
// path being taken. Asserting the path would pass just as happily against a
// logger wired to nothing, which is exactly the bug these tests exist to catch.
func captureLogf(lines *[]string) func(string, ...any) {
	return func(format string, args ...any) {
		*lines = append(*lines, fmt.Sprintf(format, args...))
	}
}

// paintWithLogf paints page onto a w x h rasterizer with logf attached, and
// returns the image plus every line logged.
func paintWithLogf(w, h int, page *layout.Page) (*image.RGBA, []string) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xff // opaque white background, matching newRasterPage
	}
	var lines []string
	PaintPageWithOptions(raster.New(img), page, render.Scale(1, 1), Options{Logf: captureLogf(&lines)})
	return img, lines
}

// filteredBox builds a one-bracket page: a solid rule of the given geometry
// wrapped in a filter carrying funcs.
func filteredBox(pageW, pageH float64, fi layout.FilterItem, rule layout.RuleItem) *layout.Page {
	return &layout.Page{WidthPt: pageW, HeightPt: pageH, Items: []layout.Item{
		{Kind: layout.FilterPushKind, Filter: fi},
		{Kind: layout.BackgroundKind, Rule: rule},
		{Kind: layout.FilterPopKind},
	}}
}

// nestedFilterPage builds n nested opacity(0.5) brackets around a black rule —
// the same construction TestPaintFilterNestingIsCapped uses, so the pixel
// assertions there and the log assertions here describe one behavior.
func nestedFilterPage(n int) *layout.Page {
	var items []layout.Item
	for i := 0; i < n; i++ {
		items = append(items, layout.Item{Kind: layout.FilterPushKind, Filter: layout.FilterItem{
			Funcs: []filtereffects.Function{{Kind: filtereffects.FuncOpacity, Amount: 0.5}},
			XPt:   10, YPt: 10, WPt: 40, HPt: 40,
		}})
	}
	items = append(items, layout.Item{
		Kind: layout.BackgroundKind,
		Rule: layout.RuleItem{XPt: 10, YPt: 10, WPt: 40, HPt: 40, Color: color.RGBA{0, 0, 0, 0xff}},
	})
	for i := 0; i < n; i++ {
		items = append(items, layout.Item{Kind: layout.FilterPopKind})
	}
	return &layout.Page{WidthPt: 100, HeightPt: 100, Items: items}
}

// TestFilterRegionCapLogs: a filter whose surface exceeds maxCSSFilterPixels
// degrades to painting its content unfiltered, and — with a logger attached —
// SAYS SO. This is the degradation a real document hits, because the 4M-pixel
// cap is below a 300 DPI A4 page (~8.7M): the same document filters at 72 and
// 150 DPI and silently stops filtering at 300, which is unexplainable from the
// output alone.
//
// The device here is 2500x2000 (5M pixels, past the cap) and the filtered box
// covers it, reproducing that shape at test speed.
func TestFilterRegionCapLogs(t *testing.T) {
	probe := color.RGBA{R: 0xff, G: 0xcc, B: 0x66, A: 0xff}
	page := filteredBox(2500, 2000,
		layout.FilterItem{
			Funcs: []filtereffects.Function{{Kind: filtereffects.FuncGrayscale, Amount: 1}},
			XPt:   0, YPt: 0, WPt: 2500, HPt: 2000,
		},
		layout.RuleItem{XPt: 0, YPt: 0, WPt: 2500, HPt: 2000, Color: probe})
	img, lines := paintWithLogf(2500, 2000, page)

	if len(lines) != 1 {
		t.Fatalf("logged %d lines, want exactly 1: %q", len(lines), lines)
	}
	// The line must name the CAP, not merely report "something went wrong": a
	// reader has to be able to tell an over-cap page from a degenerate box, and
	// the number is what tells them a lower DPI would filter it.
	line := lines[0]
	for _, want := range []string{"filter", "unfiltered", fmt.Sprintf("%d-pixel cap", maxCSSFilterPixels), "DPI"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q does not mention %q", line, want)
		}
	}
	// And the content is still THERE, unfiltered — the log describes a real
	// degradation, not a dropped bracket.
	if got := img.RGBAAt(1200, 1000); !isColor(got, probe, 1) {
		t.Errorf("centre = %v, want the UNFILTERED source %v", got, probe)
	}
}

// TestFilterNestingCapLogs: nesting past maxFilterNestingDepth degrades to
// unfiltered and logs, with a message DISTINCT from the region cap's. Reporting
// a nesting overflow as "region exceeded N pixels" would send a reader looking
// at a region that was perfectly fine, which is why the two flags and the two
// messages are separate.
func TestFilterNestingCapLogs(t *testing.T) {
	_, lines := paintWithLogf(100, 100, nestedFilterPage(maxFilterNestingDepth+1))
	if len(lines) != 1 {
		t.Fatalf("logged %d lines, want exactly 1: %q", len(lines), lines)
	}
	line := lines[0]
	for _, want := range []string{"nesting", fmt.Sprintf("%d levels", maxFilterNestingDepth), "unfiltered"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q does not mention %q", line, want)
		}
	}
	if strings.Contains(line, "pixel cap") {
		t.Errorf("the nesting notice %q reads as a region-cap overflow; the two causes must not be conflated", line)
	}

	// AT the cap nothing degrades, so nothing is logged. A notice that fired one
	// level early would be a false report of a degradation that did not happen.
	if _, atCap := paintWithLogf(100, 100, nestedFilterPage(maxFilterNestingDepth)); len(atCap) != 0 {
		t.Errorf("nesting exactly at the cap logged %q; nothing degraded there", atCap)
	}
}

// TestFilterDegradationsLogOncePerPage: a page full of over-cap filtered boxes
// must produce ONE line per distinct cause, not one per box. A per-bracket log
// on a document that filters every paragraph is a log flood, which is how a
// genuine diagnostic gets ignored.
func TestFilterDegradationsLogOncePerPage(t *testing.T) {
	gray := []filtereffects.Function{{Kind: filtereffects.FuncGrayscale, Amount: 1}}
	var items []layout.Item
	for i := 0; i < 5; i++ {
		items = append(items,
			layout.Item{Kind: layout.FilterPushKind, Filter: layout.FilterItem{
				Funcs: gray, XPt: 0, YPt: 0, WPt: 2500, HPt: 2000,
			}},
			layout.Item{Kind: layout.BackgroundKind, Rule: layout.RuleItem{
				XPt: 0, YPt: float64(i * 10), WPt: 2500, HPt: 8, Color: color.RGBA{A: 0xff},
			}},
			layout.Item{Kind: layout.FilterPopKind},
		)
	}
	_, lines := paintWithLogf(2500, 2000, &layout.Page{WidthPt: 2500, HeightPt: 2000, Items: items})
	if len(lines) != 1 {
		t.Fatalf("five over-cap brackets logged %d lines, want 1 (warn-once per cause): %q", len(lines), lines)
	}
}

// TestFilterDistinctCausesEachLog: the once-per-page suppression is per CAUSE,
// not a single global "already warned" latch. A page that hits both caps must
// report both, or the second degradation is invisible whenever the first
// happens to come first in the item stream.
func TestFilterDistinctCausesEachLog(t *testing.T) {
	gray := []filtereffects.Function{{Kind: filtereffects.FuncGrayscale, Amount: 1}}
	opacity := []filtereffects.Function{{Kind: filtereffects.FuncOpacity, Amount: 0.5}}
	items := []layout.Item{
		// An over-cap bracket (the box covers a 5M-pixel device).
		{Kind: layout.FilterPushKind, Filter: layout.FilterItem{Funcs: gray, WPt: 2500, HPt: 2000}},
		{Kind: layout.BackgroundKind, Rule: layout.RuleItem{WPt: 100, HPt: 100, Color: color.RGBA{A: 0xff}}},
		{Kind: layout.FilterPopKind},
	}
	// ...followed by a too-deeply-nested one, whose own boxes are small enough
	// to build a surface, so the ONLY thing it can trip is the nesting cap.
	for i := 0; i < maxFilterNestingDepth+1; i++ {
		items = append(items, layout.Item{Kind: layout.FilterPushKind, Filter: layout.FilterItem{
			Funcs: opacity, XPt: 10, YPt: 10, WPt: 40, HPt: 40,
		}})
	}
	items = append(items, layout.Item{Kind: layout.BackgroundKind, Rule: layout.RuleItem{
		XPt: 10, YPt: 10, WPt: 40, HPt: 40, Color: color.RGBA{A: 0xff},
	}})
	for i := 0; i < maxFilterNestingDepth+1; i++ {
		items = append(items, layout.Item{Kind: layout.FilterPopKind})
	}

	_, lines := paintWithLogf(2500, 2000, &layout.Page{WidthPt: 2500, HeightPt: 2000, Items: items})
	if len(lines) != 2 {
		t.Fatalf("logged %d lines, want 2 (one per distinct cause): %q", len(lines), lines)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "pixel cap") {
		t.Errorf("the region-cap notice is missing from %q", lines)
	}
	if !strings.Contains(joined, "nesting") {
		t.Errorf("the nesting-cap notice is missing from %q", lines)
	}
}

// TestFilterDegenerateBoxLogsItsOwnReason: a degenerate box degrades for a
// completely different reason than an over-cap one, and must say so. Both paint
// unfiltered, so the log is the only way to tell "your page is too big to
// filter at this DPI" from "your box has no extent".
func TestFilterDegenerateBoxLogsItsOwnReason(t *testing.T) {
	page := filteredBox(100, 100,
		layout.FilterItem{
			Funcs: []filtereffects.Function{{Kind: filtereffects.FuncGrayscale, Amount: 1}},
			XPt:   10, YPt: 10, WPt: 0, HPt: 0,
		},
		layout.RuleItem{XPt: 10, YPt: 10, WPt: 40, HPt: 40, Color: color.RGBA{A: 0xff}})
	_, lines := paintWithLogf(100, 100, page)
	if len(lines) != 1 {
		t.Fatalf("logged %d lines, want 1: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "degenerate") {
		t.Errorf("log line %q should name the degenerate box, not a cap the page never reached", lines[0])
	}
	if strings.Contains(lines[0], "pixel cap") {
		t.Errorf("a degenerate box was reported as a pixel-cap overflow: %q", lines[0])
	}
}

// TestNoLoggerIsSafeAndSilent: PaintPage (no logger at all) and
// PaintPageWithOptions with a nil Logf must both survive every degradation
// without panicking on the nil *warnFlags the logger-less path deliberately
// leaves nil.
func TestNoLoggerIsSafeAndSilent(t *testing.T) {
	pages := map[string]*layout.Page{
		"over-cap region": filteredBox(2500, 2000,
			layout.FilterItem{
				Funcs: []filtereffects.Function{{Kind: filtereffects.FuncGrayscale, Amount: 1}},
				WPt:   2500, HPt: 2000,
			},
			layout.RuleItem{WPt: 2500, HPt: 2000, Color: color.RGBA{R: 0xff, A: 0xff}}),
		"degenerate box": filteredBox(100, 100,
			layout.FilterItem{
				Funcs: []filtereffects.Function{{Kind: filtereffects.FuncGrayscale, Amount: 1}},
				XPt:   10, YPt: 10, WPt: 0, HPt: 0,
			},
			layout.RuleItem{XPt: 10, YPt: 10, WPt: 40, HPt: 40, Color: color.RGBA{A: 0xff}}),
		"nesting past the cap": nestedFilterPage(maxFilterNestingDepth + 1),
	}
	for name, page := range pages {
		w, h := int(page.WidthPt), int(page.HeightPt)
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		PaintPage(raster.New(img), page, render.Scale(1, 1)) // must not panic
		nilOpts := image.NewRGBA(image.Rect(0, 0, w, h))
		PaintPageWithOptions(raster.New(nilOpts), page, render.Scale(1, 1), Options{Logf: nil})
		if !bytes.Equal(img.Pix, nilOpts.Pix) {
			t.Errorf("%s: PaintPage and PaintPageWithOptions{Logf:nil} rendered differently", name)
		}
	}
}

// TestLoggerChangesNoPixels is the guarantee the whole change rests on:
// attaching a logger must alter DIAGNOSTICS only, never output. If a logger
// could move a pixel, every golden image would depend on whether the caller
// happened to pass one.
//
// It covers the degrading pages (where the new code runs) AND an ordinary
// filtered page (where it must stay entirely out of the way).
func TestLoggerChangesNoPixels(t *testing.T) {
	probe := color.RGBA{R: 0xff, G: 0xcc, B: 0x66, A: 0xff}
	pages := map[string]*layout.Page{
		"ordinary filtered box": filteredBox(100, 100,
			layout.FilterItem{
				Funcs: []filtereffects.Function{{Kind: filtereffects.FuncBlur, StdDeviation: 2}},
				XPt:   10, YPt: 10, WPt: 40, HPt: 40,
			},
			layout.RuleItem{XPt: 10, YPt: 10, WPt: 40, HPt: 40, Color: probe}),
		"over-cap region": filteredBox(2500, 2000,
			layout.FilterItem{
				Funcs: []filtereffects.Function{{Kind: filtereffects.FuncGrayscale, Amount: 1}},
				WPt:   2500, HPt: 2000,
			},
			layout.RuleItem{WPt: 2500, HPt: 2000, Color: probe}),
		"nesting past the cap": nestedFilterPage(maxFilterNestingDepth + 1),
	}
	for name, page := range pages {
		w, h := int(page.WidthPt), int(page.HeightPt)
		silent := image.NewRGBA(image.Rect(0, 0, w, h))
		PaintPage(raster.New(silent), page, render.Scale(1, 1))

		logged := image.NewRGBA(image.Rect(0, 0, w, h))
		var lines []string
		PaintPageWithOptions(raster.New(logged), page, render.Scale(1, 1), Options{Logf: captureLogf(&lines)})

		if !bytes.Equal(silent.Pix, logged.Pix) {
			t.Errorf("%s: attaching a logger changed the rendered pixels", name)
		}
	}
}
