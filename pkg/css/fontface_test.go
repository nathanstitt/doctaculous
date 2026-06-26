package css

import "testing"

func TestParseFontFaceSrcList(t *testing.T) {
	// A full src list: local() first, then two url()s with format hints, then a
	// bare url() with no format. Order and per-entry fields must be preserved.
	srcs := parseSrcList(`local("My Face"), url(my.woff2) format("woff2"), url('my.woff') format(woff), url(my.ttf)`)
	if len(srcs) != 4 {
		t.Fatalf("got %d sources, want 4: %+v", len(srcs), srcs)
	}
	if srcs[0].Local != "My Face" || srcs[0].URL != "" {
		t.Errorf("src[0] = %+v, want Local=\"My Face\"", srcs[0])
	}
	if srcs[1].URL != "my.woff2" || srcs[1].Format != "woff2" {
		t.Errorf("src[1] = %+v, want URL=my.woff2 Format=woff2", srcs[1])
	}
	if srcs[2].URL != "my.woff" || srcs[2].Format != "woff" {
		t.Errorf("src[2] = %+v, want URL=my.woff Format=woff", srcs[2])
	}
	if srcs[3].URL != "my.ttf" || srcs[3].Format != "" {
		t.Errorf("src[3] = %+v, want URL=my.ttf Format=\"\"", srcs[3])
	}
}

func TestParseSrcListSkipsMalformedEntry(t *testing.T) {
	// A garbage middle entry is skipped; the valid entries survive.
	srcs := parseSrcList(`url(a.ttf), not-a-source, url(b.ttf)`)
	if len(srcs) != 2 {
		t.Fatalf("got %d sources, want 2 (garbage entry skipped): %+v", len(srcs), srcs)
	}
	if srcs[0].URL != "a.ttf" || srcs[1].URL != "b.ttf" {
		t.Errorf("sources = %+v, want a.ttf then b.ttf", srcs)
	}
}

func TestParseStylesheetCapturesFontFace(t *testing.T) {
	src := `
		p { color: red }
		@font-face {
			font-family: "My Face";
			src: url(my.woff2) format("woff2"), url(my.ttf);
			font-weight: bold;
			font-style: italic;
		}
		@media print { p { color: black } }
	`
	sheet := Parse(src)
	// The normal rule still parses; @media is still skipped (regression guard).
	if len(sheet.Rules) != 1 {
		t.Fatalf("got %d rules, want 1: %+v", len(sheet.Rules), sheet.Rules)
	}
	if len(sheet.FontFaces) != 1 {
		t.Fatalf("got %d font faces, want 1: %+v", len(sheet.FontFaces), sheet.FontFaces)
	}
	ff := sheet.FontFaces[0]
	if ff.Family != "My Face" {
		t.Errorf("family = %q, want \"My Face\"", ff.Family)
	}
	if len(ff.Sources) != 2 || ff.Sources[0].URL != "my.woff2" || ff.Sources[0].Format != "woff2" || ff.Sources[1].URL != "my.ttf" {
		t.Errorf("sources = %+v, want [my.woff2(woff2), my.ttf]", ff.Sources)
	}
	if ff.Weight != "bold" || ff.Style != "italic" {
		t.Errorf("weight/style = %q/%q, want bold/italic", ff.Weight, ff.Style)
	}
}

func TestParseFontFaceDroppedWhenNoFamilyOrSrc(t *testing.T) {
	// No family -> dropped. No src -> dropped.
	sheet := Parse(`@font-face { src: url(x.ttf) } @font-face { font-family: Foo }`)
	if len(sheet.FontFaces) != 0 {
		t.Fatalf("got %d font faces, want 0 (both incomplete): %+v", len(sheet.FontFaces), sheet.FontFaces)
	}
}
