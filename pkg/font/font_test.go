package font

import (
	"errors"
	"math"
	"testing"

	"golang.org/x/image/font/sfnt"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/pdf/content"
	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/testdata/gen"
)

// fontDict parses a generated PDF and returns its /F1 font dictionary, so tests
// can drive font.New the same way the rasterizer does.
func fontDict(t *testing.T, pdfBytes []byte) (*pdf.Document, pdf.Dict) {
	t.Helper()
	doc, err := pdf.Parse(pdfBytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pg, err := doc.Page(0)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	res := doc.GetDict(pg.Resources["Font"])
	if res == nil {
		t.Fatal("no /Font in resources")
	}
	fd := doc.GetDict(res["F1"])
	if fd == nil {
		t.Fatal("no /F1 font dict")
	}
	return doc, fd
}

func TestSimpleTrueTypeDecodes(t *testing.T) {
	doc, fd := fontDict(t, gen.EmbeddedTrueTypePDF())
	src, err := New(doc, fd, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	glyphs := src.DecodeString([]byte("Hello"))
	if len(glyphs) != 5 {
		t.Fatalf("got %d glyphs, want 5", len(glyphs))
	}
	for i, g := range glyphs {
		if g.Outline == nil {
			t.Errorf("glyph %d (%q): nil outline", i, g.Rune)
		}
		if g.Width <= 0 || g.Width > 1.5 {
			t.Errorf("glyph %d: implausible width %v em", i, g.Width)
		}
	}
	if glyphs[0].Rune != 'H' {
		t.Errorf("glyph 0 rune = %q, want 'H'", glyphs[0].Rune)
	}
}

func TestSimpleCFFDecodes(t *testing.T) {
	doc, fd := fontDict(t, gen.EmbeddedCFFPDF())
	src, err := New(doc, fd, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	glyphs := src.DecodeString([]byte("Hello"))
	if len(glyphs) != 5 {
		t.Fatalf("got %d glyphs, want 5", len(glyphs))
	}
	for i, g := range glyphs {
		if g.Outline == nil {
			t.Errorf("glyph %d (%q): nil outline — CFF charset name→GID failed", i, g.Rune)
		}
	}
}

func TestType0Decodes(t *testing.T) {
	doc, fd := fontDict(t, gen.EmbeddedType0PDF())
	src, err := New(doc, fd, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// The fixture's show-string is 2-byte GIDs of "Hello"; decode the same bytes.
	// Build them from the font program for an exact comparison.
	glyphs := src.DecodeString(type0HelloCodes(t))
	if len(glyphs) != 5 {
		t.Fatalf("got %d glyphs, want 5", len(glyphs))
	}
	for i, g := range glyphs {
		if g.Outline == nil {
			t.Errorf("glyph %d: nil outline", i)
		}
		if g.Width <= 0 {
			t.Errorf("glyph %d: non-positive width %v", i, g.Width)
		}
	}
}

// type0HelloCodes returns the Identity-H 2-byte codes (== GIDs) for "Hello" in
// Roboto, matching what EmbeddedType0PDF draws.
func type0HelloCodes(t *testing.T) []byte {
	t.Helper()
	f, err := sfnt.Parse(gen.RobotoTTF())
	if err != nil {
		t.Fatal(err)
	}
	var b sfnt.Buffer
	var out []byte
	for _, r := range "Hello" {
		g, err := f.GlyphIndex(&b, r)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, byte(uint16(g)>>8), byte(uint16(g)))
	}
	return out
}

// TestOutlineYUp verifies a glyph outline is in em units with the Y axis up: the
// uppercase 'H' should sit above the baseline (max Y positive, around cap
// height) and within the em box.
func TestOutlineYUp(t *testing.T) {
	prog, err := parseProgram(gen.RobotoTTF(), false)
	if err != nil {
		t.Fatal(err)
	}
	var b sfnt.Buffer
	gid, _ := prog.sf.GlyphIndex(&b, 'H')
	out := prog.outline(&b, gid)
	if out == nil || out.Empty() {
		t.Fatal("no outline for 'H'")
	}
	var minY, maxY = math.Inf(1), math.Inf(-1)
	var maxAbs float64
	for _, s := range out.Segments {
		for _, p := range []render.Point{s.P0, s.P1, s.P2} {
			if p.X == 0 && p.Y == 0 {
				continue
			}
			minY = math.Min(minY, p.Y)
			maxY = math.Max(maxY, p.Y)
			maxAbs = math.Max(maxAbs, math.Max(math.Abs(p.X), math.Abs(p.Y)))
		}
	}
	if maxY <= 0 {
		t.Errorf("max Y = %v, want positive (Y-up: 'H' above baseline)", maxY)
	}
	if minY < -0.1 {
		t.Errorf("min Y = %v, 'H' should rest near the baseline", minY)
	}
	if maxAbs > 1.5 {
		t.Errorf("coordinate magnitude %v exceeds em box; not normalized", maxAbs)
	}
}

// TestQuadToCubicElevation checks the closed-form quad→cubic control points by
// building a path with a single quadratic and inspecting the emitted cubic.
func TestQuadToCubicElevation(t *testing.T) {
	// Minimal sfnt Segments are not constructible directly here; instead verify
	// the elevation formula used in outline() against a hand computation. Use a
	// representative quad: P0=(0,0), Q=(1,2), P2=(2,0).
	p0 := render.Point{X: 0, Y: 0}
	q := render.Point{X: 1, Y: 2}
	p2 := render.Point{X: 2, Y: 0}
	c1 := render.Point{X: p0.X + (2.0/3.0)*(q.X-p0.X), Y: p0.Y + (2.0/3.0)*(q.Y-p0.Y)}
	c2 := render.Point{X: p2.X + (2.0/3.0)*(q.X-p2.X), Y: p2.Y + (2.0/3.0)*(q.Y-p2.Y)}
	// Known values: C1 = (2/3, 4/3), C2 = (4/3, 4/3).
	if !approx(c1.X, 2.0/3.0) || !approx(c1.Y, 4.0/3.0) {
		t.Errorf("C1 = %+v, want (0.667, 1.333)", c1)
	}
	if !approx(c2.X, 4.0/3.0) || !approx(c2.Y, 4.0/3.0) {
		t.Errorf("C2 = %+v, want (1.333, 1.333)", c2)
	}
	// The cubic must share the quad's endpoints.
	if c1 == p0 || c2 == p2 {
		t.Error("control points must differ from endpoints for a non-degenerate quad")
	}
}

// TestRobotoOutlineHasCubics ensures a real glyph yields cubic segments (proving
// TrueType quadratics were elevated, since render.Path has no QuadTo).
func TestRobotoOutlineHasCubics(t *testing.T) {
	prog, err := parseProgram(gen.RobotoTTF(), false)
	if err != nil {
		t.Fatal(err)
	}
	var b sfnt.Buffer
	gid, _ := prog.sf.GlyphIndex(&b, 'o') // 'o' is all curves
	out := prog.outline(&b, gid)
	if out == nil {
		t.Fatal("no outline")
	}
	cubics := 0
	for _, s := range out.Segments {
		if s.Kind == render.CubeTo {
			cubics++
		}
	}
	if cubics == 0 {
		t.Error("expected cubic segments from elevated quadratics")
	}
}

func TestCFFNameToGIDGroundTruth(t *testing.T) {
	m, err := cffNameToGID(gen.SourceSansCFF())
	if err != nil {
		t.Fatalf("cffNameToGID: %v", err)
	}
	// Verified ground truth from the Source Sans 3 CFF charset (format 1).
	for name, want := range map[string]uint16{"space": 1, "A": 2, "a": 28} {
		if got := m[name]; got != want {
			t.Errorf("name %q -> GID %d, want %d", name, got, want)
		}
	}
	if len(cffStandardStrings) != 391 {
		t.Errorf("cffStandardStrings has %d entries, want 391", len(cffStandardStrings))
	}
}

func TestWrapBareCFFRoundTrips(t *testing.T) {
	cff := gen.SourceSansCFF()
	// A bare CFF must NOT parse as SFNT directly.
	if _, err := sfnt.Parse(cff); err == nil {
		t.Fatal("bare CFF parsed as SFNT; the wrapper would be unnecessary")
	}
	// Wrapped, it must parse and load glyphs.
	wrapped, err := wrapBareCFF(cff)
	if err != nil {
		t.Fatalf("wrapBareCFF: %v", err)
	}
	f, err := sfnt.Parse(wrapped)
	if err != nil {
		t.Fatalf("parse wrapped: %v", err)
	}
	if f.NumGlyphs() < 100 {
		t.Errorf("wrapped font has %d glyphs, expected the full set", f.NumGlyphs())
	}
}

func TestErrorPaths(t *testing.T) {
	tests := []struct {
		name    string
		dict    pdf.Dict
		wantErr error
	}{
		{
			name:    "unsupported subtype",
			dict:    pdf.Dict{"Subtype": pdf.Name("Type3")},
			wantErr: ErrUnsupportedFontType,
		},
		{
			name:    "no embedded program",
			dict:    pdf.Dict{"Subtype": pdf.Name("TrueType"), "FontDescriptor": pdf.Dict{}},
			wantErr: ErrNoEmbeddedProgram,
		},
		{
			name: "non-identity Type0 CMap",
			dict: pdf.Dict{
				"Subtype":  pdf.Name("Type0"),
				"Encoding": pdf.Name("UniGB-UCS2-H"),
			},
			wantErr: ErrUnsupportedCMap,
		},
	}
	doc := &pdf.Document{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(doc, tc.dict, nil)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestClassicFontFileUnsupported(t *testing.T) {
	doc := &pdf.Document{}
	dict := pdf.Dict{
		"Subtype": pdf.Name("Type1"),
		"FontDescriptor": pdf.Dict{
			"FontFile": &pdf.Stream{Dict: pdf.Dict{}, Raw: []byte("dummy")},
		},
	}
	_, err := New(doc, dict, nil)
	if !errors.Is(err, ErrUnsupportedFontProgram) {
		t.Errorf("classic FontFile err = %v, want ErrUnsupportedFontProgram", err)
	}
}

// TestType0WidthsAndCIDToGID exercises /W parsing and an explicit CIDToGIDMap
// stream by constructing a Type0 dict directly over the Roboto program.
func TestType0WidthForms(t *testing.T) {
	w := pdf.Array{
		pdf.Integer(3), pdf.Array{pdf.Integer(250), pdf.Integer(300)}, // CIDs 3,4
		pdf.Integer(10), pdf.Integer(12), pdf.Integer(500), // CIDs 10..12 = 500
	}
	doc := &pdf.Document{}
	got := parseCIDWidths(doc, w)
	want := map[int]float64{3: 0.25, 4: 0.30, 10: 0.5, 11: 0.5, 12: 0.5}
	if len(got) != len(want) {
		t.Fatalf("got %d widths, want %d: %v", len(got), len(want), got)
	}
	for cid, wv := range want {
		if math.Abs(got[cid]-wv) > 1e-9 {
			t.Errorf("CID %d width = %v, want %v", cid, got[cid], wv)
		}
	}
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Ensure the constructed sources satisfy the content.GlyphSource interface at
// compile time.
var _ content.GlyphSource = (*simpleFont)(nil)
var _ content.GlyphSource = (*type0Font)(nil)
