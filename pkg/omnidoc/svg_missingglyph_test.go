package omnidoc

import (
	"context"
	"strings"
	"testing"
)

// cjkSVG is an SVG whose text no bundled face can shape: U+6F22 is Han, and the
// bundled set is Latin/Arabic/Hebrew only. It renders as .notdef — an empty box.
const cjkSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100" width="200" height="100">` +
	`<text x="10" y="50" font-size="30">&#28450;</text></svg>`

// missingGlyphLogs filters a log slice down to the missing-glyph diagnostics.
func missingGlyphLogs(msgs []string) []string {
	var got []string
	for _, m := range msgs {
		if strings.Contains(m, "no glyph") {
			got = append(got, m)
		}
	}
	return got
}

// The bug this pins was NOT that the diagnostic did not exist — pkg/layout/inline
// has emitted it all along, and the CSS path has always shown it. It was that
// every site constructing an SVG renderer left its logger nil, so the message was
// produced and thrown away.
//
// That makes this an end-to-end test on purpose. A unit test on the renderer
// proves the message can be emitted; only going through the real entry point
// proves the caller actually asked for it, which is the half that was broken.
//
// Standalone .svg: pkg/omnidoc/svg_frontend.go. Note it already passed cfg.logf
// to svg.Parse on the line above, so parse-time diagnostics worked and only
// draw-time ones vanished — which is why the gap read as "SVG logs some things".
func TestStandaloneSVGReportsMissingGlyphs(t *testing.T) {
	opt, logs := recordLogf()
	doc, err := OpenSVGBytes([]byte(cjkSVG), opt)
	if err != nil {
		t.Fatalf("OpenSVGBytes: %v", err)
	}
	if _, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 96, BundledFonts: true}); err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}

	got := missingGlyphLogs(logs())
	if len(got) == 0 {
		t.Fatalf("a standalone SVG rendered .notdef with no diagnostic; logs = %v", logs())
	}
	if !strings.Contains(got[0], "U+6F22") {
		t.Errorf("diagnostic does not name the code point: %q", got[0])
	}
}

// Inline <svg> inside HTML takes a DIFFERENT construction site
// (pkg/layout/css/svg.go, via the engine rather than the frontend), so it needs
// its own assertion — fixing one said nothing about the other.
func TestInlineSVGInHTMLReportsMissingGlyphs(t *testing.T) {
	html := []byte(`<html><body style="margin:0"><svg width="200" height="100" viewBox="0 0 200 100">` +
		`<text x="10" y="50" font-size="30">&#28450;</text></svg></body></html>`)

	opt, logs := recordLogf()
	doc, err := OpenHTMLBytes(html, opt, WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	if _, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 96, BundledFonts: true}); err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}

	if got := missingGlyphLogs(logs()); len(got) == 0 {
		t.Errorf("an inline <svg> rendered .notdef with no diagnostic; logs = %v", logs())
	}
}

// An SVG that shapes cleanly must stay quiet, or the diagnostic becomes noise on
// every ordinary document and stops being read.
func TestShapeableSVGTextIsQuiet(t *testing.T) {
	opt, logs := recordLogf()
	doc, err := OpenSVGBytes([]byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100" width="200" height="100">`+
			`<text x="10" y="50" font-size="30">Text</text></svg>`), opt)
	if err != nil {
		t.Fatalf("OpenSVGBytes: %v", err)
	}
	if _, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 96, BundledFonts: true}); err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}

	if got := missingGlyphLogs(logs()); len(got) != 0 {
		t.Errorf("ordinary Latin SVG text produced missing-glyph diagnostics: %v", got)
	}
}

// No logger at all must not panic: WithLogf is optional and every degradation
// path has to tolerate its absence. Rendering unshapeable text is the case that
// would crash if a log site forgot its nil check.
func TestSVGWithoutLoggerDoesNotPanic(t *testing.T) {
	doc, err := OpenSVGBytes([]byte(cjkSVG))
	if err != nil {
		t.Fatalf("OpenSVGBytes: %v", err)
	}
	if _, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 96, BundledFonts: true}); err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
}
