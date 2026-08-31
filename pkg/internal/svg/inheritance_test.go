package svg

import (
	"image/color"
	"testing"
)

// rootStyleOf parses a document and returns the style its root's children inherit.
func rootStyleOf(t *testing.T, src string) Style {
	t.Helper()
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	_ = doc
	root, err := parseXML([]byte(src), func(string, ...any) {})
	if err != nil {
		t.Fatalf("parseXML: %v", err)
	}
	idx := buildIndex(root, func(key, msg string) {})
	return rootStyle(root, &cascadeCtx{idx: idx, logf: func(string, ...any) {}})
}

// A presentation attribute on the root <svg> reaches its children. This used to copy
// back only the font and text properties, so `<svg stroke="…">` with a path relying on
// the inherited stroke painted NOTHING — an icon reduced to its filled parts, which
// reads as "the icon is not rendering" rather than "the strokes are gone".
func TestRootPaintPropertiesInherit(t *testing.T) {
	s := rootStyleOf(t, `<svg xmlns="http://www.w3.org/2000/svg" fill="#f5a623" stroke="#3a4a5a" stroke-width="7"/>`)
	if want := (color.RGBA{0xf5, 0xa6, 0x23, 0xff}); s.fill != want {
		t.Errorf("inherited fill = %v, want %v", s.fill, want)
	}
	if !s.hasFill {
		t.Error("inherited hasFill = false")
	}
	if want := (color.RGBA{0x3a, 0x4a, 0x5a, 0xff}); s.stroke != want {
		t.Errorf("inherited stroke = %v, want %v", s.stroke, want)
	}
	if !s.hasStroke {
		t.Error("inherited hasStroke = false")
	}
	if s.strokeWidth != 7 {
		t.Errorf("inherited stroke-width = %v, want 7", s.strokeWidth)
	}
}

// The stroke DETAIL properties inherit too — cap, join, dashes — since an icon that
// sets them on the root expects its paths to pick them up.
func TestRootStrokeDetailInherits(t *testing.T) {
	s := rootStyleOf(t, `<svg xmlns="http://www.w3.org/2000/svg" stroke="#000" stroke-linecap="round" stroke-linejoin="round" stroke-dasharray="4 2"/>`)
	if s.cap == 0 && s.join == 0 && len(s.dashes) == 0 {
		t.Error("no stroke detail property inherited from the root")
	}
	if len(s.dashes) == 0 {
		t.Error("stroke-dasharray did not inherit")
	}
}

// NON-inherited properties must NOT reach the children, or the root's value would be
// applied twice: once to the root's own group and again to each child. Opacity is the
// visible case — a 0.5 root over a black child would composite to ~0.75 rather than
// 0.5 — and clip-path/mask/filter would each re-apply.
func TestRootNonInheritedPropertiesAreReset(t *testing.T) {
	s := rootStyleOf(t, `<svg xmlns="http://www.w3.org/2000/svg" opacity="0.5" clip-path="url(#c)" mask="url(#m)" filter="url(#f)"/>`)
	if s.opacity != 1 {
		t.Errorf("inherited opacity = %v, want 1 (non-inherited)", s.opacity)
	}
	if s.clipPathRef != "" {
		t.Errorf("inherited clip-path = %q, want empty (non-inherited)", s.clipPathRef)
	}
	if s.maskRef != "" {
		t.Errorf("inherited mask = %q, want empty (non-inherited)", s.maskRef)
	}
	if s.filterRef != "" {
		t.Errorf("inherited filter = %q, want empty (non-inherited)", s.filterRef)
	}
}

// The font and text properties that used to be the ONLY thing copied back still are.
func TestRootFontPropertiesStillInherit(t *testing.T) {
	s := rootStyleOf(t, `<svg xmlns="http://www.w3.org/2000/svg" font-family="Noto Sans" font-size="48" text-anchor="middle"/>`)
	if s.fontFamily != "Noto Sans" {
		t.Errorf("inherited font-family = %q, want %q", s.fontFamily, "Noto Sans")
	}
	if s.fontSizePt != 48 {
		t.Errorf("inherited font-size = %v, want 48", s.fontSizePt)
	}
	if s.textAnchor == "" {
		t.Error("text-anchor did not inherit")
	}
}

// A root with no attributes yields the defaults, so an ordinary document is unchanged.
func TestBareRootYieldsDefaults(t *testing.T) {
	s := rootStyleOf(t, `<svg xmlns="http://www.w3.org/2000/svg"/>`)
	d := defaultStyle()
	if s.fill != d.fill || s.hasFill != d.hasFill {
		t.Errorf("bare root fill = %v/%v, want the default %v/%v", s.fill, s.hasFill, d.fill, d.hasFill)
	}
	if s.hasStroke != d.hasStroke {
		t.Errorf("bare root hasStroke = %v, want %v", s.hasStroke, d.hasStroke)
	}
	if s.opacity != d.opacity {
		t.Errorf("bare root opacity = %v, want %v", s.opacity, d.opacity)
	}
}
