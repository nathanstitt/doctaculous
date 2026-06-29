# HTML → PDF Writer Device Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `pkg/render/pdfwrite` backend that implements `render.Device` to emit real, selectable-text PDF files from the HTML/DOCX reflow engine, exposed as `ConvertHTMLToPDF` / `(*Document).WritePDF`.

**Architecture:** A second `render.Device` implementation (sibling to `pkg/render/raster`) turns paint calls into PDF content-stream operators and serializes a PDF object tree. A new text-aware seam method `DrawGlyph(GlyphRef)` carries font identity (face + GID + runes + transform + advance) so text is embedded as a CIDFontType2 Identity-H subset with a `/ToUnicode` CMap. The laid-out pages are sliced into fixed-size PDF pages by a simple vertical fragmenter.

**Tech Stack:** Pure Go. Stdlib `compress/zlib` for stream compression. Existing `pkg/font` (textlayout), `pkg/layout`, `pkg/render`, `pkg/doctaculous`. The project's own `pkg/pdf` parser is reused as the test oracle.

---

## Background: key facts about the existing code

Read these before starting — every task depends on them.

- **`render.Device`** (`pkg/render/device.go`) is the seam. Methods: `Size`, `Fill`, `Stroke`, `DrawImage`, `FillGlyph`, `FillShading`, `PushClip`, `Save`, `Restore`. We ADD `DrawGlyph`. The raster backend (`pkg/render/raster`) is the only current implementer.
- **`render.Matrix`** is `{A,B,C,D,E,F float64}` with `Translate`, `Scale`, `Mul`, `Apply`. **`render.Path`** has `MoveTo`/`LineTo`/`CubeTo`/`Close`/`Empty`. **`render.FillColor`** is `{R,G,B,A uint8}`.
- **`render.Path` iteration:** check how the raster backend walks a `*Path`'s segments (look at `pkg/render/path.go` for the exported segment accessors) — the pdfwrite device emits path operators by walking the same segments.
- **Glyph origin chain:** `pkg/layout/inline/shape.go` `Shape()` has the `*font.Face` and rune in scope but drops them, keeping only `Outline`/`Advance` in `inline.Glyph`. That flows to `pkg/layout` `GlyphItem` (built in `pkg/layout/flow.go:~229` `emitLine`), painted by `pkg/layout/paint/paint.go` `paintGlyph` via `dev.FillGlyph`. We thread `Face`/`GID`/`Runes` down this chain.
- **`font.Face`** (`pkg/font/family.go`) wraps a private `*program` (no raw bytes retained). Both constructors — `LoadStandard` (`family.go`) and `LoadSFNT` (`pkg/font/sfnt.go`) — build `&Face{prog:..., names:...}`. `LoadSFNT` already holds decoded SFNT bytes (WOFF/WOFF2 → SFNT). We stash raw program bytes + kind on `Face` in both.
- **`program`** (`pkg/font/program.go`): `gp glyphProgram` + `upm float64`. `glyphProgram` exposes `advance(gid)`, `numGlyphs()`, `nominalGID(rune)`, `segments(gid)`. `program.advanceEm(gid)` = advance/upm.
- **Reflow render path:** `reflowRenderer{pages *layout.Pages}` (`pkg/doctaculous/reflow_backend.go`). `renderPage` paints one page: `dev := raster.New(img); paint.PaintPage(dev, pg, mat)`. HTML and DOCX both produce `*layout.Pages` and wrap it in `reflowRenderer`. `WritePDF` drives the same pages through a pdfwrite device.
- **Public API:** `Document{r renderer}`, `renderer` interface = `pageCount()` + `renderPage()`. `OpenHTML`/`OpenHTMLBytes`, `OpenDOCX`/`OpenDOCXBytes` return `*Document`.
- **`@media` today:** `pkg/css/parse.go` fully consumes but DISCARDS `@media` blocks (see `parse_test.go:10`, `fontface_test.go:46`). We capture `@media print/screen/all` and filter by active media type.
- **Conventions:** never panic on bad input; wrap errors `fmt.Errorf("...: %w", err)`; sentinel errors for branchable conditions; all exported identifiers documented; `gofmt`/`go vet`/`golangci-lint` clean; new feature ⇒ new fixture+test same PR; tests hermetic (no network).

## File structure

| File | Responsibility | New? |
|---|---|---|
| `pkg/render/device.go` | Add `DrawGlyph` method + `GlyphRef` type to the interface | modify |
| `pkg/render/raster/glyph.go` (or existing glyph file) | Implement `DrawGlyph` via outline lookup (pixels unchanged) | modify |
| `pkg/font/family.go` | Stash program bytes on `Face`; add `ProgramBytes`/`GlyphAdvance`/`UnitsPerEm`/`GID` | modify |
| `pkg/font/sfnt.go` | Stash program bytes on web-font `Face` | modify |
| `pkg/font/program.go` | Expose `programBytes`/`programKind`/`gid` helpers as needed | modify |
| `pkg/layout/inline/shape.go` | Carry `Face`/`GID`/`Runes` on `inline.Glyph` | modify |
| `pkg/layout/page.go` | Add `Face`/`GID`/`Runes` to `GlyphItem` | modify |
| `pkg/layout/flow.go` | Populate new `GlyphItem` fields in `emitLine` | modify |
| `pkg/layout/css/*` (inline emit) | Populate new `GlyphItem` fields on the CSS path | modify |
| `pkg/layout/paint/paint.go` | `paintGlyph` calls `DrawGlyph` when face/GID present, else `FillGlyph` | modify |
| `pkg/render/pdfwrite/object.go` | Writer-side PDF object model + serializer | new |
| `pkg/render/pdfwrite/device.go` | `render.Device` impl → content-stream operators | new |
| `pkg/render/pdfwrite/font.go` | CIDFontType2/Type0 subset + Identity-H + ToUnicode | new |
| `pkg/render/pdfwrite/page.go` | Fragmentation + parallel page render + sequential assembly | new |
| `pkg/render/pdfwrite/*_test.go` | Unit tests per file | new |
| `pkg/css/parse.go` + media file | Capture `@media`, tag rules by media | modify |
| `pkg/doctaculous/pdfwrite_backend.go` | `ConvertHTMLToPDF`, `WritePDF`, `PDFOptions` | new |
| `pkg/doctaculous/pdfwrite_golden_test.go` | HTML→PDF→raster round-trip + searchable-text tests | new |

---

## Task 1: Add the `DrawGlyph` seam and `GlyphRef` type

**Files:**
- Modify: `pkg/render/device.go`
- Modify: `pkg/render/raster/` (the file implementing glyph painting — find with `grep -rn "func.*FillGlyph" pkg/render/raster`)
- Test: `pkg/render/raster/drawglyph_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/render/raster/drawglyph_test.go`. It asserts that `DrawGlyph` paints the same pixels as the equivalent `FillGlyph` call (the raster backend must render `DrawGlyph` via the face's outline). Use a bundled face so no fixture is needed.

```go
package raster

import (
	"image"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

func TestDrawGlyphMatchesFillGlyph(t *testing.T) {
	face, ok := font.LoadStandard("Helvetica", font.Style{})
	if !ok {
		t.Fatal("LoadStandard Helvetica: not available")
	}
	gid, ok := face.GID('A')
	if !ok {
		t.Fatal("face has no glyph for 'A'")
	}
	outline := face.Outline(gid)
	if outline == nil {
		t.Fatal("nil outline for 'A'")
	}
	// em -> device: scale by 40, flip Y, translate down so the glyph is on-canvas.
	m := render.Scale(40, -40).Mul(render.Translate(5, 45))

	want := image.NewRGBA(image.Rect(0, 0, 50, 50))
	devWant := New(want)
	devWant.FillGlyph(transformForTest(outline, m), render.FillColor{A: 255}, "")

	got := image.NewRGBA(image.Rect(0, 0, 50, 50))
	devGot := New(got)
	devGot.DrawGlyph(render.GlyphRef{
		Face: face, GID: gid, Runes: []rune{'A'},
		Transform: m, Color: render.FillColor{A: 255},
	})

	for i := range want.Pix {
		if want.Pix[i] != got.Pix[i] {
			t.Fatalf("pixel %d differs: FillGlyph=%d DrawGlyph=%d", i, want.Pix[i], got.Pix[i])
		}
	}
}

// transformForTest applies m to every point of a path, mirroring how the raster
// backend transforms a glyph outline before filling.
func transformForTest(p *render.Path, m render.Matrix) *render.Path {
	return render.TransformPath(p, m) // add this helper in Task 1 if not present
}
```

Note: this test references `face.GID`, `face.Outline`, `render.GlyphRef`, `render.TransformPath` — defined in this task and Task 2. If `render.TransformPath` already exists under another name, use that and drop the helper.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/render/raster -run TestDrawGlyphMatchesFillGlyph`
Expected: FAIL — `render.GlyphRef` undefined, `DrawGlyph` not in interface, `face.GID`/`face.Outline` undefined.

- [ ] **Step 3: Add `GlyphRef` + `DrawGlyph` to the interface**

In `pkg/render/device.go`, add to the `Device` interface (after `FillGlyph`):

```go
	// DrawGlyph paints one shaped glyph already placed in device space via
	// g.Transform (em space, Y up, 1 em = 1 unit -> device space). Backends that
	// only rasterize render g.Face's outline for g.GID and may ignore g.Runes and
	// g.Advance; backends that emit text (PDF, text extraction) use g.Runes for a
	// ToUnicode mapping and g.Advance for spacing. g.Blend is the /BM blend mode
	// ("" = Normal), matching FillGlyph.
	DrawGlyph(g GlyphRef)
```

And add the type (after `FillColor`):

```go
// GlyphRef is one shaped glyph handed to a Device, carrying enough identity for a
// rasterizing backend (Face+GID outline), a PDF writer (Face+GID embed/subset,
// Runes for ToUnicode), and a future text-extraction backend (Runes+Transform+
// Advance for positioned text). It is format-neutral: both the reflow paint layer
// and the PDF content interpreter can populate it.
type GlyphRef struct {
	Face      glyphFace // font identity; see GlyphFace
	GID       uint16    // glyph id within Face
	Runes     []rune    // source characters this glyph represents (the cluster)
	Transform Matrix    // em space (Y up) -> device space; position, size, skew
	Advance   float64   // horizontal advance in device units
	Color     FillColor
	Blend     string // /BM blend mode ("" = Normal)
}

// glyphFace is the minimal view of a font face a Device needs: outline geometry
// for a GID (rasterizer) and raw program bytes for embedding (PDF writer). The
// concrete type is *font.Face; this interface keeps pkg/render from importing
// pkg/font (which would invert the layer dependency).
type glyphFace interface {
	// Outline returns gid's outline in em units (Y up), or nil if empty/missing.
	Outline(gid uint16) *Path
}
```

Note: `Face` is typed as the `glyphFace` interface to avoid `pkg/render` importing `pkg/font`. The PDF writer needs MORE than `Outline` (program bytes); it type-asserts the concrete `*font.Face` at the pdfwrite boundary (Task 7). Keep `glyphFace` minimal here.

Rename the struct field accordingly: in `GlyphRef`, `Face glyphFace`.

- [ ] **Step 4: Add `render.TransformPath` if missing**

Check `pkg/render/path.go` for an existing path-transform helper (the raster backend already transforms glyph outlines — it may live in raster). If there is no exported `render.TransformPath`, add one in `pkg/render/path.go`:

```go
// TransformPath returns a copy of p with every point mapped through m. It is used
// by backends that need a path in a different coordinate space (e.g. a glyph
// outline moved into device space).
func TransformPath(p *Path, m Matrix) *Path {
	if p == nil {
		return nil
	}
	out := &Path{Segments: make([]Segment, 0, len(p.Segments))}
	ap := func(pt Point) Point {
		x, y := m.Apply(pt.X, pt.Y)
		return Point{X: x, Y: y}
	}
	for _, s := range p.Segments {
		out.Segments = append(out.Segments, Segment{
			Kind: s.Kind, P0: ap(s.P0), P1: ap(s.P1), P2: ap(s.P2),
		})
	}
	return out
}
```

`pkg/layout/paint/paint.go` already has an unexported `transformPath` doing exactly this; move
its body here and have `paint.go` call `render.TransformPath` so there is one implementation.

- [ ] **Step 5: Implement `DrawGlyph` in the raster backend**

In the raster glyph file, add:

```go
// DrawGlyph renders g by filling g.Face's outline for g.GID, transformed into
// device space by g.Transform. Runes and Advance are ignored — they matter only
// to text-emitting backends. This produces pixels identical to the equivalent
// FillGlyph call, so existing goldens are unchanged.
func (d *Device) DrawGlyph(g render.GlyphRef) {
	if g.Face == nil {
		return
	}
	outline := g.Face.Outline(g.GID)
	if outline == nil || outline.Empty() {
		return
	}
	d.FillGlyph(render.TransformPath(outline, g.Transform), g.Color, g.Blend)
}
```

(Adjust the receiver name to match the raster `Device` type.)

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./pkg/render/raster -run TestDrawGlyphMatchesFillGlyph`
Expected: PASS (after Task 2 adds `face.GID`/`face.Outline`; if running before Task 2, expect the face-method compile errors — do Task 2 first if so).

Run: `go build ./...`
Expected: build fails only where other `Device` implementers (none yet besides raster) or mocks need `DrawGlyph`; fix any test mocks of `Device` by adding a `DrawGlyph` no-op.

- [ ] **Step 7: Commit**

```bash
git add pkg/render/device.go pkg/render/path.go pkg/render/raster
git commit -m "render: add DrawGlyph seam + GlyphRef; raster impl via outline"
```

---

## Task 2: Expose face identity + program bytes on `font.Face`

**Files:**
- Modify: `pkg/font/family.go`, `pkg/font/sfnt.go`, `pkg/font/program.go`
- Test: `pkg/font/face_embed_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/font/face_embed_test.go`:

```go
package font

import "testing"

func TestFaceProgramBytesAndGID(t *testing.T) {
	face, ok := LoadStandard("Helvetica", Style{})
	if !ok {
		t.Fatal("LoadStandard Helvetica: not available")
	}
	gid, ok := face.GID('A')
	if !ok || gid == 0 {
		t.Fatalf("GID('A') = %d, %v; want nonzero, true", gid, ok)
	}
	data, kind := face.ProgramBytes()
	if len(data) == 0 {
		t.Fatal("ProgramBytes returned empty data")
	}
	if kind == ProgramKindUnknown {
		t.Fatal("ProgramBytes returned unknown kind")
	}
	if upm := face.UnitsPerEm(); upm <= 0 {
		t.Fatalf("UnitsPerEm = %v; want > 0", upm)
	}
	if adv := face.GlyphAdvance(gid); adv <= 0 {
		t.Fatalf("GlyphAdvance(%d) = %v; want > 0", gid, adv)
	}
	if face.Outline(gid) == nil {
		t.Fatal("Outline returned nil for 'A'")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/font -run TestFaceProgramBytesAndGID`
Expected: FAIL — `face.GID`, `ProgramBytes`, `ProgramKind*`, `UnitsPerEm`, `GlyphAdvance`, `Outline` undefined.

- [ ] **Step 3: Add a `ProgramKind` and store bytes on `Face`**

In `pkg/font/family.go`, extend `Face` and add the public methods:

```go
// ProgramKind identifies the embedded font-program format of a Face's bytes, so a
// PDF writer can pick /FontFile2 (TrueType/SFNT) vs /FontFile3 (CFF/OpenType).
type ProgramKind int

const (
	// ProgramKindUnknown means the program bytes were not retained.
	ProgramKindUnknown ProgramKind = iota
	// ProgramKindTrueType is an SFNT/TrueType program (embed as /FontFile2).
	ProgramKindTrueType
	// ProgramKindCFF is a bare CFF/Type1C program (embed as /FontFile3).
	ProgramKindCFF
	// ProgramKindType1 is a classic Type1 program (eexec); not directly CID-embeddable.
	ProgramKindType1
)
```

Add fields to `Face`:

```go
type Face struct {
	prog  *program
	names map[string]fonts.GID

	progData []byte      // raw program bytes for embedding ("" if not retained)
	progKind ProgramKind // format of progData
}
```

- [ ] **Step 4: Populate bytes in both constructors**

In `LoadStandard` (`family.go`), set the bytes from the substitute:

```go
	return &Face{
		prog:     prog,
		names:    prog.nameToGID(),
		progData: sub.Data,
		progKind: programKindFromStandard(sub.Kind),
	}, true
```

Add:

```go
// programKindFromStandard maps a bundled substitute's Kind to a ProgramKind.
func programKindFromStandard(k standard.Kind) ProgramKind {
	switch k {
	case standard.KindTrueType:
		return ProgramKindTrueType
	case standard.KindType1:
		return ProgramKindType1
	default:
		return ProgramKindUnknown
	}
}
```

In `LoadSFNT` (`sfnt.go`), the decoded SFNT bytes are the program; set them:

```go
	return &Face{
		prog:     prog,
		names:    prog.nameToGID(),
		progData: data, // decoded SFNT (WOFF/WOFF2 already decompressed upstream)
		progKind: ProgramKindTrueType,
	}, nil
```

(If `LoadSFNT` can also yield a CFF-flavored OpenType, detect the `CFF ` table and set `ProgramKindCFF`; if uncertain, `ProgramKindTrueType` is correct for glyf-flavored SFNT. Keep it simple: set TrueType unless a `CFF ` table is present.)

- [ ] **Step 5: Add the public methods**

In `family.go`:

```go
// ProgramBytes returns the raw font-program bytes for embedding and their format.
// kind is ProgramKindUnknown (and data nil) when the Face did not retain its
// program (the PDF writer then falls back to drawing outlines).
func (f *Face) ProgramBytes() (data []byte, kind ProgramKind) {
	return f.progData, f.progKind
}

// UnitsPerEm returns the face's units-per-em (always > 0).
func (f *Face) UnitsPerEm() float64 { return f.prog.upm }

// GID resolves rune r to a glyph id, preferring the glyph-name route then the
// program cmap, matching how Glyph resolves outlines. ok is false when the face
// has no glyph for r.
func (f *Face) GID(r rune) (gid uint16, ok bool) {
	g, ok := f.gidForRune(r)
	return uint16(g), ok
}

// Outline returns glyph gid's outline in em units (Y up), or nil if empty. It
// satisfies the render.glyphFace contract used by GlyphRef.
func (f *Face) Outline(gid uint16) *render.Path { return f.prog.outline(fonts.GID(gid)) }

// GlyphAdvance returns gid's horizontal advance in em units (advance/units-per-em),
// for building a PDF /W widths array.
func (f *Face) GlyphAdvance(gid uint16) float64 {
	adv, _ := f.prog.advanceEm(fonts.GID(gid))
	return adv
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./pkg/font -run TestFaceProgramBytesAndGID`
Expected: PASS

Run: `go test ./pkg/font/...`
Expected: PASS (no regressions)

- [ ] **Step 7: Commit**

```bash
git add pkg/font
git commit -m "font: expose program bytes, GID, advance, outline on Face for embedding"
```

---

## Task 3: Thread Face/GID/Runes through the layout glyph chain

**Files:**
- Modify: `pkg/layout/inline/shape.go`, `pkg/layout/page.go`, `pkg/layout/flow.go`, and the CSS inline emitter (find with `grep -rn "GlyphKind\|GlyphItem{" pkg/layout/css`)
- Modify: `pkg/layout/paint/paint.go`
- Test: `pkg/layout/inline/shape_identity_test.go` (create), `pkg/layout/paint/drawglyph_test.go` (create)

- [ ] **Step 1: Write the failing test (shaping carries identity)**

Create `pkg/layout/inline/shape_identity_test.go`:

```go
package inline

import (
	"testing"

	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
)

func TestShapeCarriesFaceAndRune(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	glyphs := Shape(faces, []Run{{Text: "Hi", Family: "Helvetica", SizePt: 12, Color: Color{A: 255}}}, nil)
	var inked int
	for _, g := range glyphs {
		if g.Outline == nil {
			continue
		}
		inked++
		if g.Face == nil {
			t.Error("inked glyph has nil Face")
		}
		if len(g.Runes) == 0 {
			t.Error("inked glyph has no Runes")
		}
	}
	if inked != 2 {
		t.Fatalf("inked glyphs = %d; want 2", inked)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/inline -run TestShapeCarriesFaceAndRune`
Expected: FAIL — `g.Face`, `g.Runes` undefined.

- [ ] **Step 3: Add identity fields to `inline.Glyph` and populate in `Shape`**

In `pkg/layout/inline/shape.go`, add to `Glyph`:

```go
	Face  *pkgfont.Face // resolved face for this glyph (nil for whitespace/atomic)
	GID   uint16        // glyph id within Face
	Runes []rune        // source characters this glyph represents
```

(`pkgfont` is already imported as the alias for `github.com/nathanstitt/doctaculous/pkg/font`.)

In `Shape`, inside the `for _, rn := range r.Text` loop, resolve the GID and set the fields:

```go
		for _, rn := range r.Text {
			outline, advEm, ok := face.Glyph(rn)
			if !ok {
				continue
			}
			gid, _ := face.GID(rn)
			out = append(out, Glyph{
				Outline:   outline,
				Advance:   advEm * r.SizePt,
				Color:     col,
				SizePt:    r.SizePt,
				AscentPt:  asc * r.SizePt,
				DescentPt: desc * r.SizePt,
				LineGapPt: gap * r.SizePt,
				Space:     rn == ' ' || rn == '\t',
				Face:      face,
				GID:       gid,
				Runes:     []rune{rn},
			})
		}
```

- [ ] **Step 4: Run shaping test to verify it passes**

Run: `go test ./pkg/layout/inline -run TestShapeCarriesFaceAndRune`
Expected: PASS

- [ ] **Step 5: Add identity fields to `layout.GlyphItem`**

In `pkg/layout/page.go`, extend `GlyphItem`:

```go
type GlyphItem struct {
	Outline  *render.Path
	XPt, YPt float64
	SizePt   float64
	Color    color.RGBA

	Face  *font.Face // identity for text-emitting backends (nil -> outline only)
	GID   uint16
	Runes []rune
}
```

Add the import `"github.com/nathanstitt/doctaculous/pkg/font"` to `page.go` if not present.

- [ ] **Step 6: Populate the fields where `GlyphItem` is built**

In `pkg/layout/flow.go` `emitLine`, copy the identity through:

```go
			st.cur = append(st.cur, Item{
				Kind: GlyphKind,
				Glyph: GlyphItem{
					Outline: gl.Outline,
					XPt:     x,
					YPt:     baseline,
					SizePt:  gl.SizePt,
					Color:   color.RGBA{R: gl.Color.R, G: gl.Color.G, B: gl.Color.B, A: gl.Color.A},
					Face:    gl.Face,
					GID:     gl.GID,
					Runes:   gl.Runes,
				},
			})
```

Do the same in the CSS inline emitter (find every `GlyphItem{` construction with `grep -rn "GlyphItem{" pkg/layout`). Each must copy `Face`/`GID`/`Runes` from its source `inline.Glyph`.

- [ ] **Step 7: Write the failing paint test (paint prefers DrawGlyph)**

Create `pkg/layout/paint/drawglyph_test.go` with a recording fake `render.Device` that counts `DrawGlyph` vs `FillGlyph`:

```go
package paint

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

type recordDev struct {
	render.Device
	drawGlyphs int
	fillGlyphs int
}

func (r *recordDev) DrawGlyph(render.GlyphRef)                         { r.drawGlyphs++ }
func (r *recordDev) FillGlyph(*render.Path, render.FillColor, string) { r.fillGlyphs++ }

func TestPaintGlyphPrefersDrawGlyph(t *testing.T) {
	face, ok := font.LoadStandard("Helvetica", font.Style{})
	if !ok {
		t.Fatal("no Helvetica")
	}
	gid, _ := face.GID('A')
	pg := &layout.Page{
		WidthPt: 100, HeightPt: 100,
		Items: []layout.Item{{
			Kind: layout.GlyphKind,
			Glyph: layout.GlyphItem{
				Outline: face.Outline(gid), XPt: 10, YPt: 20, SizePt: 12,
				Face: face, GID: gid, Runes: []rune{'A'},
			},
		}},
	}
	dev := &recordDev{}
	PaintPage(dev, pg, render.Scale(1, 1))
	if dev.drawGlyphs != 1 || dev.fillGlyphs != 0 {
		t.Fatalf("DrawGlyph=%d FillGlyph=%d; want 1, 0", dev.drawGlyphs, dev.fillGlyphs)
	}
}
```

Note: `recordDev` embeds `render.Device` so it satisfies the full interface while overriding the two glyph methods. The other methods are nil and unused by this test.

- [ ] **Step 8: Run paint test to verify it fails**

Run: `go test ./pkg/layout/paint -run TestPaintGlyphPrefersDrawGlyph`
Expected: FAIL — `paintGlyph` still calls `FillGlyph`.

- [ ] **Step 9: Update `paintGlyph` to call `DrawGlyph` when identity present**

In `pkg/layout/paint/paint.go`, rewrite `paintGlyph`:

```go
// paintGlyph draws one glyph. When the glyph carries font identity (Face+GID), it
// uses DrawGlyph so text-emitting backends (PDF) can embed real text; otherwise it
// falls back to filling the raw outline. The em -> device transform is the same in
// both cases.
func paintGlyph(dev render.Device, g *layout.GlyphItem, mat render.Matrix) {
	m := render.Scale(g.SizePt, -g.SizePt).
		Mul(render.Translate(g.XPt, g.YPt)).
		Mul(mat)
	if g.Face != nil {
		dev.DrawGlyph(render.GlyphRef{
			Face:      g.Face,
			GID:       g.GID,
			Runes:     g.Runes,
			Transform: m,
			Advance:   0, // advance not needed for paint; PDF writer recomputes from /W
			Color:     render.FillColor{R: g.Color.R, G: g.Color.G, B: g.Color.B, A: g.Color.A},
		})
		return
	}
	if g.Outline == nil || g.Outline.Empty() {
		return
	}
	dev.FillGlyph(transformPath(g.Outline, m), render.FillColor{
		R: g.Color.R, G: g.Color.G, B: g.Color.B, A: g.Color.A,
	}, "")
}
```

- [ ] **Step 10: Run paint test + full layout/raster suites**

Run: `go test ./pkg/layout/... ./pkg/render/...`
Expected: PASS. Critically, the raster goldens must be UNCHANGED (raster `DrawGlyph` renders via outline = identical pixels).

Run: `go test ./pkg/doctaculous -run TestHTMLGolden`
Expected: PASS (HTML goldens unchanged).

- [ ] **Step 11: Commit**

```bash
git add pkg/layout
git commit -m "layout: thread Face/GID/Runes through glyphs; paint via DrawGlyph"
```

---

## Task 4: PDF object model + serializer (`pkg/render/pdfwrite/object.go`)

**Files:**
- Create: `pkg/render/pdfwrite/object.go`
- Test: `pkg/render/pdfwrite/object_test.go`

- [ ] **Step 1: Write the failing test (round-trip via pkg/pdf)**

Create `pkg/render/pdfwrite/object_test.go`. Build a tiny PDF (catalog + 1 page, no content) and re-parse it with the project's own parser as the oracle.

```go
package pdfwrite

import (
	"bytes"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

func TestSerializeMinimalPDFParses(t *testing.T) {
	w := newWriter()

	pages := w.alloc()
	page := w.alloc()
	catalog := w.alloc()

	w.put(catalog, Dict{"Type": Name("Catalog"), "Pages": Ref(pages)})
	w.put(pages, Dict{
		"Type":  Name("Pages"),
		"Kids":  Array{Ref(page)},
		"Count": Int(1),
	})
	w.put(page, Dict{
		"Type":     Name("Page"),
		"Parent":   Ref(pages),
		"MediaBox": Array{Int(0), Int(0), Int(612), Int(792)},
	})
	w.setRoot(catalog)

	var buf bytes.Buffer
	if err := w.serialize(&buf); err != nil {
		t.Fatalf("serialize: %v", err)
	}

	doc, err := pdf.OpenBytes(buf.Bytes()) // use the project parser's bytes entrypoint
	if err != nil {
		t.Fatalf("pkg/pdf failed to parse our output: %v", err)
	}
	if got := doc.PageCount(); got != 1 { // adjust to pkg/pdf's actual page-count API
		t.Fatalf("page count = %d; want 1", got)
	}
}

func TestSerializeStreamFlateRoundTrips(t *testing.T) {
	w := newWriter()
	content := []byte("BT /F1 12 Tf (hi) Tj ET")
	sid := w.addStream(Dict{}, content)
	if sid == 0 {
		t.Fatal("addStream returned zero id")
	}
	var buf bytes.Buffer
	if err := w.serialize(&buf); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	// The stream must be flate-encoded and declare it.
	if !bytes.Contains(buf.Bytes(), []byte("/Filter")) {
		t.Fatal("stream not marked with a /Filter")
	}
	if bytes.Contains(buf.Bytes(), content) {
		t.Fatal("stream content stored uncompressed (raw bytes present)")
	}
}
```

Check the exact `pkg/pdf` entry point and page-count method first: `grep -rn "func OpenBytes\|func.*PageCount\|func.*NumPages\|Pages" pkg/pdf/*.go | head`. Adjust the test to the real API.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/render/pdfwrite -run TestSerialize`
Expected: FAIL — package/types undefined.

- [ ] **Step 3: Implement the object model + serializer**

Create `pkg/render/pdfwrite/object.go`:

```go
// Package pdfwrite implements a render.Device that emits a PDF document instead of
// pixels. This file holds the write-only PDF object model and serializer; it is
// deliberately separate from the parse-oriented pkg/pdf.
package pdfwrite

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// object is any value that can appear in the PDF body.
type object interface{ writeTo(w *bytes.Buffer) }

// Name is a PDF name (/Foo). Int, Real, Bool, String are scalars. Ref is an
// indirect reference. Dict and Array are composites. stream is a stream object.
type Name string
type Int int64
type Real float64
type Bool bool
type String string // written as a (literal) string, escaped
type Ref int        // 1-based object id; 0 means "null"
type Dict map[string]object
type Array []object

func (n Name) writeTo(b *bytes.Buffer)   { b.WriteByte('/'); b.WriteString(escapeName(string(n))) }
func (i Int) writeTo(b *bytes.Buffer)    { b.WriteString(strconv.FormatInt(int64(i), 10)) }
func (r Real) writeTo(b *bytes.Buffer)   { b.WriteString(strconv.FormatFloat(float64(r), 'f', 4, 64)) }
func (x Bool) writeTo(b *bytes.Buffer) {
	if x {
		b.WriteString("true")
	} else {
		b.WriteString("false")
	}
}
func (s String) writeTo(b *bytes.Buffer) { b.WriteByte('('); b.WriteString(escapeString(string(s))); b.WriteByte(')') }
func (r Ref) writeTo(b *bytes.Buffer)    { fmt.Fprintf(b, "%d 0 R", int(r)) }

func (d Dict) writeTo(b *bytes.Buffer) {
	b.WriteString("<<")
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic output for reproducible tests
	for _, k := range keys {
		b.WriteByte('/')
		b.WriteString(escapeName(k))
		b.WriteByte(' ')
		d[k].writeTo(b)
		b.WriteByte(' ')
	}
	b.WriteString(">>")
}

func (a Array) writeTo(b *bytes.Buffer) {
	b.WriteByte('[')
	for i, e := range a {
		if i > 0 {
			b.WriteByte(' ')
		}
		e.writeTo(b)
	}
	b.WriteByte(']')
}

// stream is a dict plus already-encoded body bytes; /Length is set at serialize.
type stream struct {
	dict Dict
	data []byte
}

func (s stream) writeTo(b *bytes.Buffer) {
	s.dict.writeTo(b)
	b.WriteString("\nstream\n")
	b.Write(s.data)
	b.WriteString("\nendstream")
}

// writer accumulates indirect objects and serializes a complete PDF file.
type writer struct {
	objs []object // index i holds object id i+1; nil = free/unused
	root Ref
	info Ref
}

func newWriter() *writer { return &writer{} }

// alloc reserves a new indirect object id (1-based).
func (w *writer) alloc() Ref {
	w.objs = append(w.objs, nil)
	return Ref(len(w.objs))
}

// put stores obj at id (from alloc).
func (w *writer) put(id Ref, obj object) { w.objs[int(id)-1] = obj }

// setRoot records the document catalog reference written into the trailer.
func (w *writer) setRoot(id Ref) { w.root = id }

// setInfo records the /Info dictionary reference for the trailer (optional).
func (w *writer) setInfo(id Ref) { w.info = id }

// addStream allocates a stream object, flate-compresses data, sets /Filter and
// /Length, stores it, and returns its id.
func (w *writer) addStream(dict Dict, data []byte) Ref {
	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	_, _ = zw.Write(data)
	_ = zw.Close()
	if dict == nil {
		dict = Dict{}
	}
	dict["Filter"] = Name("FlateDecode")
	dict["Length"] = Int(int64(zbuf.Len()))
	id := w.alloc()
	w.put(id, stream{dict: dict, data: zbuf.Bytes()})
	return id
}

// serialize writes the full PDF (header, body, xref table, trailer) to out.
func (w *writer) serialize(out io.Writer) error {
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n") // binary marker for robustness

	offsets := make([]int, len(w.objs)+1) // offsets[id]
	for i, obj := range w.objs {
		id := i + 1
		if obj == nil {
			return fmt.Errorf("pdfwrite: object %d allocated but never put", id)
		}
		offsets[id] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n", id)
		obj.writeTo(&b)
		b.WriteString("\nendobj\n")
	}

	xrefStart := b.Len()
	n := len(w.objs) + 1
	fmt.Fprintf(&b, "xref\n0 %d\n", n)
	b.WriteString("0000000000 65535 f \n")
	for id := 1; id < n; id++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[id])
	}

	trailer := Dict{"Size": Int(int64(n)), "Root": w.root}
	if w.info != 0 {
		trailer["Info"] = w.info
	}
	b.WriteString("trailer\n")
	trailer.writeTo(&b)
	fmt.Fprintf(&b, "\nstartxref\n%d\n%%%%EOF\n", xrefStart)

	_, err := out.Write(b.Bytes())
	if err != nil {
		return fmt.Errorf("pdfwrite: write: %w", err)
	}
	return nil
}

// escapeName escapes characters not allowed bare in a PDF name (#xx hex).
func escapeName(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '!' || c > '~' || c == '#' || c == '/' || c == '(' || c == ')' || c == '<' || c == '>' || c == '[' || c == ']' || c == '{' || c == '}' || c == '%' {
			fmt.Fprintf(&sb, "#%02X", c)
		} else {
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// escapeString escapes a PDF literal string body.
func escapeString(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '(', ')', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case '\n':
			sb.WriteString("\\n")
		case '\r':
			sb.WriteString("\\r")
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/render/pdfwrite -run TestSerialize`
Expected: PASS. If `pkg/pdf` rejects the output, inspect the error and fix the serializer (most likely xref offsets or trailer). Add `cmd/dumpfixtures`-style debugging only if needed.

- [ ] **Step 5: Commit**

```bash
git add pkg/render/pdfwrite/object.go pkg/render/pdfwrite/object_test.go
git commit -m "pdfwrite: write-only PDF object model + serializer (parses via pkg/pdf)"
```

---

## Task 5: Font subsetting + embedding (`pkg/render/pdfwrite/font.go`)

This is the riskiest unit. It is built in two slices: (5a) emit a Type0 font that embeds the WHOLE program (correct, larger files) with a `/ToUnicode` CMap; (5b) replace whole-program embedding with a glyf subset. Ship 5a first so the pipeline works end-to-end, then optimize.

**Files:**
- Create: `pkg/render/pdfwrite/font.go`
- Test: `pkg/render/pdfwrite/font_test.go`

### Task 5a: Type0 / Identity-H font with whole-program embed + ToUnicode

- [ ] **Step 1: Write the failing test**

Create `pkg/render/pdfwrite/font_test.go`:

```go
package pdfwrite

import (
	"bytes"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/font"
)

func TestEmbedFontProducesType0AndToUnicode(t *testing.T) {
	face, ok := font.LoadStandard("Helvetica", font.Style{})
	if !ok {
		t.Fatal("no Helvetica")
	}
	w := newWriter()
	fe := newFontEmbedder()

	// Record two glyphs as the device would.
	for _, r := range []rune{'A', 'B'} {
		gid, _ := face.GID(r)
		fe.use(face, gid, []rune{r})
	}

	fontRef := fe.emit(w, face) // emits the Type0 font tree, returns the /Font ref

	var buf bytes.Buffer
	w.put(w.alloc(), Dict{"X": fontRef}) // keep the ref reachable so it serializes
	w.setRoot(w.alloc())
	w.put(w.root, Dict{"Type": Name("Catalog")})
	if err := w.serialize(&buf); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	out := buf.Bytes()
	for _, want := range []string{"/Type0", "/Identity-H", "/CIDFontType2", "/ToUnicode", "/FontFile2"} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("output missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/render/pdfwrite -run TestEmbedFont`
Expected: FAIL — `newFontEmbedder` undefined.

- [ ] **Step 3: Implement the embedder (whole-program slice)**

Create `pkg/render/pdfwrite/font.go`:

```go
package pdfwrite

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/nathanstitt/doctaculous/pkg/font"
)

// fontEmbedder collects the glyphs used per face during device painting and emits
// each face as a Type0/Identity-H CID font with a ToUnicode CMap. This slice embeds
// the whole program (correct but larger); subsetting replaces the embed step later.
type fontEmbedder struct {
	used map[*font.Face]map[uint16][]rune // face -> gid -> source runes
	res  map[*font.Face]string            // face -> resource name (/F0, /F1, ...)
}

func newFontEmbedder() *fontEmbedder {
	return &fontEmbedder{used: map[*font.Face]map[uint16][]rune{}, res: map[*font.Face]string{}}
}

// use records that gid (from face) was drawn for the given source runes.
func (fe *fontEmbedder) use(face *font.Face, gid uint16, runes []rune) {
	m := fe.used[face]
	if m == nil {
		m = map[uint16][]rune{}
		fe.used[face] = m
	}
	if _, seen := m[gid]; !seen {
		m[gid] = append([]rune(nil), runes...)
	}
}

// resourceName returns the /Font resource name for face, assigning one on first use.
func (fe *fontEmbedder) resourceName(face *font.Face) string {
	if n, ok := fe.res[face]; ok {
		return n
	}
	n := fmt.Sprintf("F%d", len(fe.res))
	fe.res[face] = n
	return n
}

// emit writes face's Type0 font tree to w and returns the top /Font reference.
// Returns 0 if face has no embeddable program (caller draws outlines instead).
func (fe *fontEmbedder) emit(w *writer, face *font.Face) Ref {
	data, kind := face.ProgramBytes()
	if len(data) == 0 || kind != font.ProgramKindTrueType {
		return 0 // CFF/Type1/unknown: 5a handles TrueType only; outline fallback elsewhere
	}
	gids := sortedGIDs(fe.used[face])
	upm := face.UnitsPerEm()

	// FontFile2 (whole program).
	fontFile := w.addStream(Dict{"Length1": Int(int64(len(data)))}, data)

	descriptor := w.alloc()
	w.put(descriptor, Dict{
		"Type":        Name("FontDescriptor"),
		"FontName":    Name("DTACUL+Embedded"),
		"Flags":       Int(4), // Symbolic; conservative and always valid
		"FontBBox":    Array{Int(-1000), Int(-1000), Int(2000), Int(2000)},
		"ItalicAngle": Int(0),
		"Ascent":      Int(800),
		"Descent":     Int(-200),
		"CapHeight":   Int(700),
		"StemV":       Int(80),
		"FontFile2":   fontFile,
	})

	cidFont := w.alloc()
	w.put(cidFont, Dict{
		"Type":           Name("Font"),
		"Subtype":        Name("CIDFontType2"),
		"BaseFont":       Name("DTACUL+Embedded"),
		"CIDSystemInfo":  Dict{"Registry": String("Adobe"), "Ordering": String("Identity"), "Supplement": Int(0)},
		"FontDescriptor": descriptor,
		"CIDToGIDMap":    Name("Identity"),
		"W":              widthsArray(gids, face, upm),
	})

	toUni := w.addStream(Dict{}, toUnicodeCMap(fe.used[face]))

	font0 := w.alloc()
	w.put(font0, Dict{
		"Type":            Name("Font"),
		"Subtype":         Name("Type0"),
		"BaseFont":        Name("DTACUL+Embedded"),
		"Encoding":        Name("Identity-H"),
		"DescendantFonts": Array{cidFont},
		"ToUnicode":       toUni,
	})
	return font0
}

// sortedGIDs returns the used GIDs in ascending order.
func sortedGIDs(m map[uint16][]rune) []uint16 {
	out := make([]uint16, 0, len(m))
	for g := range m {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// widthsArray builds a /W array mapping each GID (==CID under Identity) to its
// advance in 1000-unit glyph space.
func widthsArray(gids []uint16, face *font.Face, upm float64) Array {
	var w Array
	for _, g := range gids {
		adv := face.GlyphAdvance(g) * 1000 // GlyphAdvance is in em; *1000 -> glyph space
		w = append(w, Int(int64(g)), Array{Int(int64(adv + 0.5))})
	}
	return w
}

// toUnicodeCMap builds a minimal ToUnicode CMap mapping each CID (==GID) to its
// source UTF-16BE code units, so text is searchable/copyable.
func toUnicodeCMap(m map[uint16][]rune) []byte {
	var b bytes.Buffer
	b.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n")
	b.WriteString("/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n")
	b.WriteString("1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	gids := sortedGIDs(m)
	b.WriteString(fmt.Sprintf("%d beginbfchar\n", len(gids)))
	for _, g := range gids {
		fmt.Fprintf(&b, "<%04X> <%s>\n", g, utf16BEHex(m[g]))
	}
	b.WriteString("endbfchar\nendcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
	return b.Bytes()
}

// utf16BEHex encodes runes as concatenated UTF-16BE hex (surrogate pairs for
// astral code points).
func utf16BEHex(runes []rune) string {
	var sb bytes.Buffer
	for _, r := range runes {
		if r > 0xFFFF {
			r -= 0x10000
			hi := 0xD800 + (r >> 10)
			lo := 0xDC00 + (r & 0x3FF)
			fmt.Fprintf(&sb, "%04X%04X", hi, lo)
		} else {
			fmt.Fprintf(&sb, "%04X", r)
		}
	}
	return sb.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/render/pdfwrite -run TestEmbedFont`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/render/pdfwrite/font.go pkg/render/pdfwrite/font_test.go
git commit -m "pdfwrite: Type0/Identity-H font embedding (whole program) + ToUnicode"
```

### Task 5b: glyf subsetting

- [ ] **Step 1: Write the failing test**

Add to `font_test.go`:

```go
func TestSubsetRetainsUsedGlyphsOnly(t *testing.T) {
	face, _ := font.LoadStandard("Helvetica", font.Style{})
	data, _ := face.ProgramBytes()
	gidA, _ := face.GID('A')
	gidB, _ := face.GID('B')

	sub, err := subsetTrueType(data, []uint16{gidA, gidB})
	if err != nil {
		t.Fatalf("subsetTrueType: %v", err)
	}
	if len(sub) == 0 || len(sub) >= len(data) {
		t.Fatalf("subset size %d not smaller than original %d", len(sub), len(data))
	}
	// The subset must still be a parseable SFNT that the font package can load.
	if _, err := font.LoadSFNT(sub); err != nil {
		t.Fatalf("subset not loadable: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/render/pdfwrite -run TestSubset`
Expected: FAIL — `subsetTrueType` undefined.

- [ ] **Step 3: Implement `subsetTrueType`**

Add to `font.go`. The function parses the SFNT table directory, retains used glyphs plus composite dependencies, rewrites `loca`/`glyf`/`hmtx`/`maxp`/`head`/`cmap`(minimal)/`hhea`, and re-emits an SFNT.

There may already be SFNT-building helpers in `pkg/font/sfntbuild.go` / `pkg/font/sfnt.go` (the web-font path builds SFNT from WOFF tables). **Check first** — `grep -rn "func " pkg/font/sfntbuild.go pkg/font/sfnt.go` — and reuse the table-directory writer rather than reimplementing it. If `pkg/font` has an internal SFNT writer that is not exported, export a minimal builder (e.g. `font.BuildSFNT(tables map[string][]byte) []byte`) and call it here.

Subsetting algorithm (TrueType glyf):
1. Parse table directory → map tag → bytes.
2. Read `head.indexToLocFormat`, `maxp.numGlyphs`, parse `loca`.
3. Compute the retained set: requested GIDs ∪ composite-component GIDs (transitively). Always include GID 0 (.notdef).
4. Build a new compact GID order? **No** — keep CIDToGIDMap = Identity (Task 5a), so GIDs are NOT remapped; instead keep the original glyph indices and zero out the glyf data for unused glyphs (set their loca entries to empty). This keeps Identity mapping valid and still shrinks `glyf` substantially.
5. Rewrite `glyf` (only retained glyph programs, others zero-length), `loca` (offsets reflecting the new glyf), `maxp` (numGlyphs unchanged), `hmtx` (unchanged), `head` (unchanged or new loca format).
6. Re-emit SFNT via the table-directory writer.

```go
// subsetTrueType returns an SFNT containing only the glyph programs for keep (plus
// composite dependencies and .notdef), with glyph indices preserved so a Type0
// font's Identity CIDToGIDMap stays valid. Unused glyphs become zero-length. It
// returns an error if data is not a parseable glyf-flavored SFNT (caller then
// embeds the whole program).
func subsetTrueType(data []byte, keep []uint16) ([]byte, error) {
	// ... implement per the algorithm above, reusing pkg/font SFNT helpers ...
}
```

Implementation note: keep this readable and well-commented; it is the highest-risk code. If reusing `pkg/font` internals proves heavy, embed the whole program (5a) is an acceptable fallback — but the subset test must pass for at least the glyf-zeroing approach.

- [ ] **Step 4: Wire subsetTrueType into emit**

In `fontEmbedder.emit`, replace the whole-program `fontFile := w.addStream(...)` with:

```go
	progBytes := data
	if sub, err := subsetTrueType(data, gids); err == nil {
		progBytes = sub
	} // else: embed whole program (graceful)
	fontFile := w.addStream(Dict{"Length1": Int(int64(len(progBytes)))}, progBytes)
```

- [ ] **Step 5: Run tests**

Run: `go test ./pkg/render/pdfwrite`
Expected: PASS (both embed and subset tests).

- [ ] **Step 6: Commit**

```bash
git add pkg/render/pdfwrite/font.go pkg/render/pdfwrite/font_test.go pkg/font
git commit -m "pdfwrite: glyf subsetting (zero unused glyphs, Identity-preserving)"
```

---

## Task 6: The device — content-stream operators (`pkg/render/pdfwrite/device.go`)

**Files:**
- Create: `pkg/render/pdfwrite/device.go`
- Test: `pkg/render/pdfwrite/device_test.go`

- [ ] **Step 1: Write the failing test (operators emitted)**

Create `pkg/render/pdfwrite/device_test.go`. Feed a few ops, finish the page, and assert the decompressed content stream contains the expected operators.

```go
package pdfwrite

import (
	"bytes"
	"compress/zlib"
	"io"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

func TestDeviceEmitsFillAndGlyphOps(t *testing.T) {
	dev := newPageDevice(200, 200) // single page, points

	// A filled rectangle.
	p := &render.Path{}
	p.MoveTo(10, 10)
	p.LineTo(50, 10)
	p.LineTo(50, 40)
	p.LineTo(10, 40)
	p.Close()
	dev.Fill(p, render.FillPaint{Color: render.FillColor{R: 255, A: 255}})

	// A glyph.
	face, _ := font.LoadStandard("Helvetica", font.Style{})
	gid, _ := face.GID('A')
	dev.DrawGlyph(render.GlyphRef{
		Face: face, GID: gid, Runes: []rune{'A'},
		Transform: render.Scale(12, -12).Mul(render.Translate(20, 100)),
		Color:     render.FillColor{A: 255},
	})

	content := decompress(t, dev.contentStream())
	for _, want := range []string{" f", "BT", "Tj", "ET"} {
		if !bytes.Contains(content, []byte(want)) {
			t.Errorf("content stream missing %q\n%s", want, content)
		}
	}
	if len(dev.fonts().used) == 0 {
		t.Error("glyph not recorded for embedding")
	}
}

func decompress(t *testing.T, data []byte) []byte {
	t.Helper()
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil { // content may be stored raw before flate; allow that
		return data
	}
	out, _ := io.ReadAll(zr)
	return out
}
```

Adjust accessor names (`contentStream`, `fonts`) to whatever you implement; keep them test-visible (same package).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/render/pdfwrite -run TestDeviceEmits`
Expected: FAIL — `newPageDevice` undefined.

- [ ] **Step 3: Implement the page device**

Create `pkg/render/pdfwrite/device.go`. It implements `render.Device` for ONE page (the multi-page assembler in Task 7 creates one per page). It writes operators into a buffer and records glyphs/images/extgstates.

```go
package pdfwrite

import (
	"bytes"
	"fmt"
	"image"

	"github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// pageDevice implements render.Device by appending PDF content-stream operators to
// a buffer for a single page. It emits RAW page-space coordinates (top-left origin,
// Y down); Task 7 prepends a single page-level CTM ("1 0 0 -1 0 H cm") that flips
// the whole page into PDF bottom-left/Y-up space. The device never flips per
// coordinate — one flip strategy, applied once at the page level.
type pageDevice struct {
	buf        bytes.Buffer
	wPt, hPt   float64
	embed      *fontEmbedder
	images     []pendingImage // images referenced this page (assembled later)
	extGStates map[string]Dict
	logf       func(string, ...any)
}

type pendingImage struct {
	name string
	img  image.Image
	ctm  render.Matrix
}

func newPageDevice(wPt, hPt float64) *pageDevice {
	return &pageDevice{wPt: wPt, hPt: hPt, embed: newFontEmbedder(), extGStates: map[string]Dict{}}
}

func (d *pageDevice) Size() (int, int) { return int(d.wPt), int(d.hPt) }

func (d *pageDevice) Fill(p *render.Path, paint render.FillPaint) {
	d.color(paint.Color, false)
	d.writePath(p)
	if paint.Rule == render.EvenOdd {
		d.buf.WriteString(" f*\n")
	} else {
		d.buf.WriteString(" f\n")
	}
}

func (d *pageDevice) Stroke(p *render.Path, paint render.StrokePaint) {
	d.color(paint.Color, true)
	fmt.Fprintf(&d.buf, "%.3f w\n", paint.Width)
	d.writePath(p)
	d.buf.WriteString(" S\n")
}

func (d *pageDevice) DrawGlyph(g render.GlyphRef) {
	face, ok := g.Face.(*font.Face)
	if !ok || face == nil {
		// Unknown face type: fall back to filling the outline.
		if o := outlineFromRef(g); o != nil {
			d.Fill(o, render.FillPaint{Color: g.Color})
		}
		return
	}
	d.embed.use(face, g.GID, g.Runes)
	name := d.embed.resourceName(face)

	// Decompose g.Transform into a text matrix. The transform maps em space -> page
	// space; PDF wants a Tm that maps text space (1 unit = 1 em at the font size)
	// likewise. Emit BT, set font size 1 (the matrix carries the scale), Tm, Tj.
	m := g.Transform
	d.colorRGBA(g.Color, false)
	d.buf.WriteString("BT\n")
	fmt.Fprintf(&d.buf, "/%s 1 Tf\n", name)
	// The text matrix is g.Transform verbatim (raw page space). The page-level CTM
	// (Task 7) flips the whole page, so no per-coordinate flip here. Font size is 1
	// because the matrix's linear part already carries the em scale.
	fmt.Fprintf(&d.buf, "%.4f %.4f %.4f %.4f %.4f %.4f Tm\n", m.A, m.B, m.C, m.D, m.E, m.F)
	fmt.Fprintf(&d.buf, "<%04X> Tj\n", g.GID)
	d.buf.WriteString("ET\n")
}

func (d *pageDevice) FillGlyph(outline *render.Path, c render.FillColor, blend string) {
	d.Fill(outline, render.FillPaint{Color: c})
}

func (d *pageDevice) DrawImage(img image.Image, ctm render.Matrix, alpha float64, blend string) {
	name := fmt.Sprintf("Im%d", len(d.images))
	d.images = append(d.images, pendingImage{name: name, img: img, ctm: ctm})
	d.buf.WriteString("q\n")
	m := ctm
	fmt.Fprintf(&d.buf, "%.4f %.4f %.4f %.4f %.4f %.4f cm\n", m.A, m.B, m.C, m.D, m.E, m.F)
	fmt.Fprintf(&d.buf, "/%s Do\n", name)
	d.buf.WriteString("Q\n")
}

func (d *pageDevice) FillShading(s render.Shader, ctm render.Matrix, blend string) {
	// The HTML layout engine does not currently emit shadings; log and skip.
	if d.logf != nil {
		d.logf("pdfwrite: FillShading not supported; skipped")
	}
}

func (d *pageDevice) PushClip(p *render.Path, rule render.FillRule) {
	d.writePath(p)
	if rule == render.EvenOdd {
		d.buf.WriteString(" W* n\n")
	} else {
		d.buf.WriteString(" W n\n")
	}
}

func (d *pageDevice) Save()    { d.buf.WriteString("q\n") }
func (d *pageDevice) Restore() { d.buf.WriteString("Q\n") }

// writePath emits path construction operators (m/l/c/h) in raw page-space
// coordinates. The page-level Y-flip CTM (prepended in Task 7) maps these to PDF
// bottom-left space, so this device does NOT flip per coordinate.
func (d *pageDevice) writePath(p *render.Path) {
	for _, s := range p.Segments {
		switch s.Kind {
		case render.MoveTo:
			fmt.Fprintf(&d.buf, "%.3f %.3f m\n", s.P0.X, s.P0.Y)
		case render.LineTo:
			fmt.Fprintf(&d.buf, "%.3f %.3f l\n", s.P0.X, s.P0.Y)
		case render.CubeTo:
			fmt.Fprintf(&d.buf, "%.3f %.3f %.3f %.3f %.3f %.3f c\n",
				s.P0.X, s.P0.Y, s.P1.X, s.P1.Y, s.P2.X, s.P2.Y)
		case render.Close:
			d.buf.WriteString("h\n")
		}
	}
}

func (d *pageDevice) color(c render.FillColor, stroke bool)     { d.colorRGBA(c, stroke) }
func (d *pageDevice) colorRGBA(c render.FillColor, stroke bool) {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	op := "rg"
	if stroke {
		op = "RG"
	}
	fmt.Fprintf(&d.buf, "%.4f %.4f %.4f %s\n", r, g, b, op)
}

// contentStream returns the raw (uncompressed) page content bytes.
func (d *pageDevice) contentStream() []byte { return d.buf.Bytes() }

// fonts returns the page's font embedder (glyphs recorded for embedding).
func (d *pageDevice) fonts() *fontEmbedder { return d.embed }

// outlineFromRef returns the transformed outline for a GlyphRef whose face is not a
// *font.Face (rare fallback). Returns nil if no outline is available.
func outlineFromRef(g render.GlyphRef) *render.Path {
	if g.Face == nil {
		return nil
	}
	o := g.Face.Outline(g.GID)
	if o == nil {
		return nil
	}
	return render.TransformPath(o, g.Transform)
}
```

**Y-flip strategy (already baked into the code above).** The incoming `g.Transform` maps
em-space (Y up) → page-space (Y down, top-left origin), because `paintGlyph` built it as
`Scale(size,-size) · Translate(X,Y) · mat`. PDF page space is Y-up bottom-left. This device
emits **raw page-space coordinates** and relies on a **single page-level CTM**
(`1 0 0 -1 0 <pageHeight> cm`, prepended in Task 7) to flip the whole page once. That is why
none of the methods above flip per coordinate — do not add per-coordinate flips. If you ever
see content mirrored vertically, the bug is the page-level CTM (Task 7), not this device.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/render/pdfwrite -run TestDeviceEmits`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/render/pdfwrite/device.go pkg/render/pdfwrite/device_test.go
git commit -m "pdfwrite: page device emits content-stream ops (fill/stroke/glyph/image/clip)"
```

---

## Task 7: Document assembly + fragmentation + parallel page rendering (`pkg/render/pdfwrite/page.go`)

This task has TWO concurrency phases (see spec §Concurrency):
- **Parallel render phase:** each page-band renders on a worker into its OWN `pageDevice` (own
  local `fontEmbedder`/image list). Output is a pure value — content bytes + the faces/images that
  band used. No shared mutable state, so `go test -race` is clean.
- **Sequential assembly phase:** one goroutine folds the per-band results into the shared object
  table + xref, DE-DUPLICATING faces across pages (one embedded subset per face) and assigning
  final object ids, in page order for deterministic output.

**Prerequisite — make `fontEmbedder` per-page-local.** In Task 5/6 the device held one
`fontEmbedder`. That is fine: each `pageDevice` already owns its own embedder (`newPageDevice`
calls `newFontEmbedder()`). The cross-page de-dup happens in the assembly phase here, NOT in the
embedder. Do not share one embedder across devices.

**Files:**
- Create: `pkg/render/pdfwrite/page.go`
- Test: `pkg/render/pdfwrite/page_test.go`

- [ ] **Step 1: Write the failing tests (fragmentation + assembly + determinism)**

Create `pkg/render/pdfwrite/page_test.go`:

```go
package pdfwrite

import (
	"bytes"
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/layout"
)

func TestWriteDocumentPaginatesAndParses(t *testing.T) {
	// One tall layout page, 600pt content, fragmented onto 200pt-tall PDF pages
	// (no margins) => 3 pages.
	pages := &layout.Pages{Pages: []layout.Page{tallTextPage(t, 600)}}
	opts := Options{PageWidthPt: 300, PageHeightPt: 200, MarginPt: 0}

	var buf bytes.Buffer
	if err := WriteDocument(context.Background(), &buf, pages, opts); err != nil {
		t.Fatalf("WriteDocument: %v", err)
	}
	doc, err := pdf.OpenBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if got := doc.PageCount(); got != 3 {
		t.Fatalf("page count = %d; want 3", got)
	}
}

// TestWriteDocumentDeterministic asserts the parallel render + sequential merge
// produces byte-identical output across runs (no map-iteration nondeterminism,
// no worker-order leakage).
func TestWriteDocumentDeterministic(t *testing.T) {
	pages := &layout.Pages{Pages: []layout.Page{tallTextPage(t, 600)}}
	opts := Options{PageWidthPt: 300, PageHeightPt: 200, MarginPt: 0}

	var a, b bytes.Buffer
	if err := WriteDocument(context.Background(), &a, pages, opts); err != nil {
		t.Fatal(err)
	}
	if err := WriteDocument(context.Background(), &b, pages, opts); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("WriteDocument output not deterministic across runs")
	}
}

// tallTextPage builds a layout.Page heightPt tall with a glyph every 20pt so
// fragmentation has line boxes to break between. (Helper — fill in using
// layout.Page/Item/GlyphItem and a bundled face.)
func tallTextPage(t *testing.T, heightPt float64) layout.Page { /* ... */ }
```

Implement `tallTextPage` with a bundled Helvetica face, placing one `GlyphKind` item per 20pt of
height (all using the SAME face, so the cross-page de-dup is exercised — one font subset must
serve all 3 pages).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/render/pdfwrite -run TestWriteDocument`
Expected: FAIL — `Options`, `WriteDocument` undefined.

- [ ] **Step 3: Implement fragmentation + the two-phase writer**

Create `pkg/render/pdfwrite/page.go`:

```go
package pdfwrite

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"sync"

	"github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/paint"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// Options controls PDF output geometry and concurrency.
type Options struct {
	PageWidthPt  float64 // default US Letter width (612) if <= 0
	PageHeightPt float64 // default US Letter height (792) if <= 0
	MarginPt     float64 // uniform content margin; default 36 (0.5in) if zero
	Title        string
	Workers      int // page-render worker cap; default GOMAXPROCS if <= 0
	Logf         func(string, ...any)
}

// band is a vertical slice [topPt, bottomPt) of a layout page placed on one PDF page.
type band struct {
	page      *layout.Page
	topPt     float64
	bottomPt  float64
}

// renderedPage is one worker's pure output: the page's content bytes plus the faces
// and images it used. It carries NO object ids — those are assigned during the
// sequential merge, so workers share no writer state.
type renderedPage struct {
	index   int             // position in document order, for deterministic assembly
	content []byte          // raw page-space content (no flip CTM yet)
	faces   map[*font.Face]map[uint16][]rune // glyphs used, per face
	images  []pendingImage  // images used, in encounter order
	faceOrder []*font.Face  // faces in first-use order (deterministic resource naming)
}

// WriteDocument fragments the laid-out pages into fixed-size PDF pages, renders the
// pages concurrently, then assembles the PDF sequentially and writes it to out. It
// honors context cancellation and recovers per page so one bad page cannot abort the
// document.
func WriteDocument(ctx context.Context, out io.Writer, pages *layout.Pages, opts Options) error {
	opts = withDefaults(opts)
	contentH := opts.PageHeightPt - 2*opts.MarginPt

	// 1. Fragment every layout page into bands (cheap, sequential).
	var bands []band
	for i := range pages.Pages {
		lp := &pages.Pages[i]
		for _, b := range fragment(lp, contentH) {
			bands = append(bands, band{page: lp, topPt: b.topPt, bottomPt: b.bottomPt})
		}
	}

	// 2. Render bands concurrently into pure renderedPage values (no shared state).
	rendered := renderBandsParallel(ctx, bands, opts)
	if err := ctx.Err(); err != nil {
		return err
	}

	// 3. Assemble sequentially: one writer, faces de-duped across pages.
	return assemble(out, rendered, opts)
}

// renderBandsParallel renders each band on a bounded worker pool. Results are
// returned in document order (results[i] corresponds to bands[i]); a band that
// fails to render is left as a zero renderedPage with content nil and logged.
func renderBandsParallel(ctx context.Context, bands []band, opts Options) []renderedPage {
	results := make([]renderedPage, len(bands))
	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := range bands {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil && opts.Logf != nil {
					opts.Logf("pdfwrite: page %d panicked: %v", idx, r)
				}
			}()
			results[idx] = renderBand(bands[idx], idx, opts)
		}(i)
	}
	wg.Wait()
	return results
}

// renderBand paints one band into its own pageDevice and returns a pure value. It
// touches no shared writer state, so it is safe to call from many goroutines.
func renderBand(b band, index int, opts Options) renderedPage {
	dev := newPageDevice(opts.PageWidthPt, opts.PageHeightPt)
	dev.logf = opts.Logf

	// Translate page space so the band's top maps to the top of the content area.
	mat := render.Translate(opts.MarginPt, opts.MarginPt-b.topPt)
	sub := clipPageToBand(b.page, b)
	paint.PaintPage(dev, sub, mat)

	return renderedPage{
		index:     index,
		content:   dev.contentStream(),
		faces:     dev.embed.used,
		faceOrder: dev.embed.order, // first-use face order (add to fontEmbedder, Step 4)
		images:    dev.images,
	}
}

// assemble folds the rendered pages into a single PDF, de-duplicating fonts across
// pages (one embedded subset per face) and writing in document order.
func assemble(out io.Writer, rendered []renderedPage, opts Options) error {
	w := newWriter()
	pagesRef := w.alloc()

	// Merge all faces used anywhere, in deterministic order (page order, then each
	// page's first-use order), so each face is embedded ONCE.
	merged := newFontEmbedder()
	for _, rp := range rendered {
		for _, face := range rp.faceOrder {
			for gid, runes := range rp.faces[face] {
				merged.use(face, gid, runes)
			}
		}
	}
	// Emit each unique face once; remember its top /Font ref and resource name.
	faceRef := map[*font.Face]Ref{}
	for _, face := range merged.orderedFaces() { // deterministic
		ref := merged.emit(w, face)
		if ref != 0 {
			faceRef[face] = ref
		}
	}

	var kids []object
	for _, rp := range rendered {
		if rp.content == nil {
			continue // failed/skipped band
		}
		flip := fmt.Sprintf("1 0 0 -1 0 %.4f cm\n", opts.PageHeightPt)
		contentRef := w.addStream(Dict{}, append([]byte(flip), rp.content...))

		// Per-page resources reference the shared face refs by the merged resource
		// name, plus this page's own embedded images.
		fonts := Dict{}
		for _, face := range rp.faceOrder {
			if ref, ok := faceRef[face]; ok {
				fonts[merged.resourceName(face)] = ref
			}
		}
		res := Dict{"Font": fonts}
		if len(rp.images) > 0 {
			xobjs := Dict{}
			for _, pi := range rp.images {
				imgRef, err := embedImage(w, pi.img)
				if err != nil {
					if opts.Logf != nil {
						opts.Logf("pdfwrite: image embed failed: %v", err)
					}
					continue
				}
				xobjs[pi.name] = imgRef
			}
			res["XObject"] = xobjs
		}

		pageRef := w.alloc()
		w.put(pageRef, Dict{
			"Type":      Name("Page"),
			"Parent":    pagesRef,
			"MediaBox":  Array{Int(0), Int(0), Real(opts.PageWidthPt), Real(opts.PageHeightPt)},
			"Contents":  contentRef,
			"Resources": res,
		})
		kids = append(kids, pageRef)
	}

	w.put(pagesRef, Dict{"Type": Name("Pages"), "Kids": Array(kids), "Count": Int(int64(len(kids)))})
	catalog := w.alloc()
	w.put(catalog, Dict{"Type": Name("Catalog"), "Pages": pagesRef})
	w.setRoot(catalog)
	if opts.Title != "" {
		info := w.alloc()
		w.put(info, Dict{"Title": String(opts.Title)})
		w.setInfo(info)
	}
	return w.serialize(out)
}

// fragment slices a layout page into bands at most contentH tall, breaking between
// line boxes (never inside one).
func fragment(lp *layout.Page, contentH float64) []struct{ topPt, bottomPt float64 } {
	// 1. Collect distinct line-box top/bottom Y values from lp.Items (glyph rows,
	//    rules, backgrounds). 2. Greedily accumulate rows until the next row would
	//    exceed contentH; close a band there. 3. A single row taller than contentH
	//    gets its own band (it overflows; log via the caller's Logf). 4. If lp has
	//    no items, return one empty band so a blank page is still emitted.
	// Return bands as {topPt, bottomPt} pairs in top-to-bottom order.
}

// clipPageToBand returns a layout.Page containing only the items whose vertical
// extent intersects band b, so off-page items are not painted.
func clipPageToBand(lp *layout.Page, b band) *layout.Page {
	// Copy lp.Items, keeping those with Y in [b.topPt, b.bottomPt). For glyphs use
	// Glyph.YPt; for rules/backgrounds/borders/images use their YPt..YPt+HPt. Return
	// a *layout.Page with the same WidthPt and HeightPt = b.bottomPt-b.topPt.
}

// withDefaults fills zero-value Options with US Letter + 0.5in margins.
func withDefaults(o Options) Options {
	if o.PageWidthPt <= 0 {
		o.PageWidthPt = 612
	}
	if o.PageHeightPt <= 0 {
		o.PageHeightPt = 792
	}
	if o.MarginPt < 0 {
		o.MarginPt = 0
	} else if o.MarginPt == 0 {
		o.MarginPt = 36
	}
	return o
}
```

**Determinism is non-negotiable.** Two sources of nondeterminism must be eliminated, both covered
by `TestWriteDocumentDeterministic`:
1. **Worker order:** `results[idx] = ...` writes to a per-index slot, so output order is document
   order regardless of which worker finishes first. Never append to a shared slice from workers.
2. **Map iteration:** never iterate `map[*font.Face]...` directly when assigning object ids or
   resource names. Always iterate via the recorded first-use ORDER (`faceOrder`/`orderedFaces`).
   This is why `fontEmbedder` must record face order (Step 4).

- [ ] **Step 4: Add deterministic face ordering to `fontEmbedder`**

The assembly merge must assign object ids and resource names in a stable order, never by map
iteration. Extend `fontEmbedder` in `pkg/render/pdfwrite/font.go` to record first-use order:

```go
type fontEmbedder struct {
	used  map[*font.Face]map[uint16][]rune
	res   map[*font.Face]string
	order []*font.Face // faces in first-use order (deterministic id/name assignment)
}
```

In `newFontEmbedder`, initialize nothing extra (nil slice is fine). In `use`, when a face is seen
for the first time, append it to `order`:

```go
func (fe *fontEmbedder) use(face *font.Face, gid uint16, runes []rune) {
	m := fe.used[face]
	if m == nil {
		m = map[uint16][]rune{}
		fe.used[face] = m
		fe.order = append(fe.order, face) // first sight: record order
	}
	if _, seen := m[gid]; !seen {
		m[gid] = append([]rune(nil), runes...)
	}
}

// orderedFaces returns the faces in first-use order, for deterministic emission.
func (fe *fontEmbedder) orderedFaces() []*font.Face { return fe.order }
```

`resourceName` already assigns `F0`, `F1`, ... on first call; because the merge calls it (via
`orderedFaces`) in `order`, names are deterministic. Update the Task 5a `emit` test if it relied
on map iteration (it only checks substring presence, so it is unaffected).

- [ ] **Step 5: Implement `embedImage`**

Add to `page.go` (or a small `image.go`):

```go
// embedImage encodes img as an RGB image XObject (Flate), adding an /SMask for
// alpha when present, and returns its reference.
func embedImage(w *writer, img image.Image) (Ref, error) {
	b := img.Bounds()
	wd, ht := b.Dx(), b.Dy()
	if wd <= 0 || ht <= 0 {
		return 0, fmt.Errorf("pdfwrite: empty image")
	}
	rgb := make([]byte, 0, wd*ht*3)
	alpha := make([]byte, 0, wd*ht)
	hasAlpha := false
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA() // 16-bit; >>8 to 8-bit
			rgb = append(rgb, byte(r>>8), byte(g>>8), byte(bl>>8))
			a8 := byte(a >> 8)
			if a8 != 0xff {
				hasAlpha = true
			}
			alpha = append(alpha, a8)
		}
	}
	dict := Dict{
		"Type":             Name("XObject"),
		"Subtype":          Name("Image"),
		"Width":            Int(int64(wd)),
		"Height":           Int(int64(ht)),
		"ColorSpace":       Name("DeviceRGB"),
		"BitsPerComponent": Int(8),
	}
	if hasAlpha {
		smaskDict := Dict{
			"Type": Name("XObject"), "Subtype": Name("Image"),
			"Width": Int(int64(wd)), "Height": Int(int64(ht)),
			"ColorSpace": Name("DeviceGray"), "BitsPerComponent": Int(8),
		}
		smask := w.addStream(smaskDict, alpha)
		dict["SMask"] = smask
	}
	return w.addStream(dict, rgb), nil
}
```

(Import `image` and `fmt` in `page.go`; `image` may already be imported via `pendingImage`.)

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./pkg/render/pdfwrite -run TestWriteDocument`
Expected: PASS (3 pages, parses; deterministic across runs).

Run: `go test -race ./pkg/render/pdfwrite`
Expected: PASS — the `-race` run is REQUIRED here: it proves the parallel render phase shares no
mutable state. If it reports a race, the cause is almost certainly a worker touching shared writer
or embedder state — workers must touch only their own `pageDevice`.

Run: `go test ./pkg/render/pdfwrite`
Expected: PASS (all pdfwrite tests).

- [ ] **Step 7: Add a benchmark proving the speedup**

Add to `page_test.go`:

```go
func BenchmarkWriteDocument(b *testing.B) {
	pages := &layout.Pages{Pages: []layout.Page{tallTextPage(nil, 6000)}} // many bands
	opts := Options{PageWidthPt: 612, PageHeightPt: 792}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		if err := WriteDocument(context.Background(), &buf, pages, opts); err != nil {
			b.Fatal(err)
		}
	}
}
```

(Make `tallTextPage` tolerate a nil `*testing.T` for the benchmark, or add a `tallTextPageB`
helper.) Run with `-cpu 1,4` to compare:

Run: `go test ./pkg/render/pdfwrite -bench BenchmarkWriteDocument -cpu 1,4 -run x`
Expected: the 4-CPU run is faster than the 1-CPU run (the parallel render phase scales). Record
the numbers in the commit message.

- [ ] **Step 8: Commit**

```bash
git add pkg/render/pdfwrite/page.go pkg/render/pdfwrite/font.go pkg/render/pdfwrite/page_test.go
git commit -m "pdfwrite: parallel page render + sequential assembly, fragmentation, image embed"
```

---

## Task 8: `@media print` capture in `pkg/css`

**Files:**
- Modify: `pkg/css/parse.go` (and the rule/sheet types — find with `grep -rn "type Rule\|type Sheet\|skipAtRule\|@media" pkg/css/*.go`)
- Create: `pkg/css/media.go` (if a new file suits the parser layout)
- Test: `pkg/css/media_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/css/media_test.go`:

```go
package css

import "testing"

func TestMediaPrintRulesCapturedAndFiltered(t *testing.T) {
	src := `
		p { color: red }
		@media print { p { color: black } }
		@media screen { p { color: blue } }
	`
	sheet := Parse(src)

	// Under a print media context, the @media print block participates.
	printRules := sheet.RulesForMedia(MediaPrint)
	// Under screen, the @media screen block participates instead.
	screenRules := sheet.RulesForMedia(MediaScreen)

	if len(printRules) == 0 || len(screenRules) == 0 {
		t.Fatalf("print=%d screen=%d; both should be non-empty", len(printRules), len(screenRules))
	}
	if countSelectorRules(printRules) <= countSelectorRules(sheet.RulesForMedia(MediaScreen))-1 {
		// crude guard: print context includes the @media print rule
	}
}

func countSelectorRules(rs []Rule) int { return len(rs) }
```

Refine assertions to the actual `Rule`/`Sheet` API. The essential behavior: `@media print`/`@media screen` blocks are retained (not discarded), tagged with their media type, and a `RulesForMedia(m)` returns base rules + rules whose media matches `m` (or `all`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css -run TestMediaPrint`
Expected: FAIL — `MediaPrint`, `RulesForMedia` undefined; and the existing skip-discards the block.

- [ ] **Step 3: Capture `@media` in the parser**

In `pkg/css/parse.go`, where the parser currently consumes-and-discards `@media`, instead parse the media query prelude (just the leading media TYPE token — `print`/`screen`/`all`) and the inner rules, attaching the media type to each captured rule. Add:

```go
// Media is a parsed media type. Only types (print/screen/all) are honored; full
// media queries (feature tests) degrade to "matches if the type matches".
type Media int

const (
	MediaAll Media = iota // applies in any context (default for top-level rules)
	MediaScreen
	MediaPrint
)
```

Add a `Media` field to the rule type (default `MediaAll` for top-level rules). When parsing `@media <prelude> { ... }`: extract the first identifier in `<prelude>` (`print`→`MediaPrint`, `screen`→`MediaScreen`, else `MediaAll`); parse the inner rules and tag each with that `Media`. Unrecognized media features in the prelude are ignored (degrade per spec).

- [ ] **Step 4: Add `RulesForMedia`**

```go
// RulesForMedia returns the sheet's rules that apply in media context m: every
// rule tagged MediaAll plus rules tagged m. Top-level rules are MediaAll.
func (s *Sheet) RulesForMedia(m Media) []Rule {
	var out []Rule
	for _, r := range s.Rules {
		if r.Media == MediaAll || r.Media == m {
			out = append(out, r)
		}
	}
	return out
}
```

- [ ] **Step 5: Keep existing behavior intact**

The existing default cascade (no media filtering) must still work. Ensure the existing cascade entrypoint defaults to `MediaScreen` (or `MediaAll`) so current HTML rendering is unchanged. Verify with the existing parser tests — `parse_test.go:10` asserted the `@media` block was skipped, leaving 2 rules; UPDATE that test to the new behavior (the block is now captured, so the top-level rule count for `MediaAll` is unchanged but `RulesForMedia(MediaPrint)` now includes the print rule). Adjust the assertion to reflect captured-not-discarded.

- [ ] **Step 6: Run tests**

Run: `go test ./pkg/css/...`
Expected: PASS (including the updated `parse_test.go`).

- [ ] **Step 7: Commit**

```bash
git add pkg/css
git commit -m "css: capture @media print/screen blocks, tag rules, add RulesForMedia"
```

---

## Task 9: Public API — `ConvertHTMLToPDF`, `WritePDF`, `PDFOptions`

**Files:**
- Create: `pkg/doctaculous/pdfwrite_backend.go`
- Modify: `pkg/doctaculous/reflow_backend.go` (expose pages), `pkg/doctaculous/html_backend.go` (Print option)
- Test: `pkg/doctaculous/pdfwrite_golden_test.go`

- [ ] **Step 1: Expose the laid-out pages from the reflow renderer**

In `pkg/doctaculous/reflow_backend.go`, add an accessor and an interface so `WritePDF` can reach the pages:

```go
// reflowPages is implemented by renderers backed by *layout.Pages, so the PDF
// writer can drive the same laid-out pages the rasterizer uses.
type reflowPages interface{ layoutPages() *layout.Pages }

func (r *reflowRenderer) layoutPages() *layout.Pages { return r.pages }
```

- [ ] **Step 2: Write the failing test (round-trip)**

Create `pkg/doctaculous/pdfwrite_golden_test.go`:

```go
package doctaculous

import (
	"bytes"
	"context"
	"testing"
)

func TestConvertHTMLToPDFRoundTrips(t *testing.T) {
	html := `<!DOCTYPE html><html><head><style>body{margin:0}</style></head>
<body><p>Hello PDF world</p></body></html>`

	var buf bytes.Buffer
	err := ConvertHTMLToPDF(context.Background(), bytes.NewReader([]byte(html)), &buf, PDFOptions{})
	if err != nil {
		t.Fatalf("ConvertHTMLToPDF: %v", err)
	}
	// Parse our own output and assert it has at least one page and embedded text.
	doc, err := OpenBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("reopen generated PDF: %v", err)
	}
	if doc.PageCount() < 1 {
		t.Fatalf("page count = %d; want >= 1", doc.PageCount())
	}
}

func TestWritePDFWorksForDOCX(t *testing.T) {
	// Use an existing tiny DOCX fixture/generator (see pkg/doctaculous docx tests).
	d := openTinyDOCXForTest(t) // helper: reuse an existing docx fixture
	var buf bytes.Buffer
	if err := d.WritePDF(context.Background(), &buf, PDFOptions{}); err != nil {
		t.Fatalf("WritePDF (docx): %v", err)
	}
	if _, err := OpenBytes(buf.Bytes()); err != nil {
		t.Fatalf("docx->pdf output unparseable: %v", err)
	}
}
```

For `openTinyDOCXForTest`, reuse whatever the existing DOCX golden tests use to construct a `*Document` (search `grep -rn "OpenDOCXBytes\|docx" pkg/doctaculous/*_test.go`).

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./pkg/doctaculous -run TestConvertHTMLToPDFRoundTrips`
Expected: FAIL — `ConvertHTMLToPDF`, `PDFOptions`, `WritePDF` undefined.

- [ ] **Step 4: Implement the backend**

Create `pkg/doctaculous/pdfwrite_backend.go`:

```go
package doctaculous

import (
	"context"
	"fmt"
	"io"

	"github.com/nathanstitt/doctaculous/pkg/render/pdfwrite"
)

// PDFOptions controls HTML/DOCX -> PDF conversion.
type PDFOptions struct {
	// PageWidthPt, PageHeightPt set the PDF page size in points; default US Letter
	// (612x792) when zero.
	PageWidthPt, PageHeightPt float64
	// MarginPt is the uniform content margin in points; default 36 (0.5in).
	MarginPt float64
	// Print, when true, makes the cascade honor @media print rules (and exclude
	// screen-only rules). Default false (screen context).
	Print bool
	// Title sets the PDF /Info /Title metadata.
	Title string
	// Workers caps the goroutines used to render pages concurrently. Defaults to
	// GOMAXPROCS when zero (matching RasterOptions.Workers).
	Workers int
	// Logf receives degradation diagnostics (nil -> no-op).
	Logf func(string, ...any)
}

func (o PDFOptions) toWriterOptions() pdfwrite.Options {
	return pdfwrite.Options{
		PageWidthPt:  o.PageWidthPt,
		PageHeightPt: o.PageHeightPt,
		MarginPt:     o.MarginPt,
		Title:        o.Title,
		Workers:      o.Workers,
		Logf:         o.Logf,
	}
}

// ConvertHTMLToPDF reads HTML from in, lays it out, and writes a PDF to out.
func ConvertHTMLToPDF(ctx context.Context, in io.Reader, out io.Writer, opts PDFOptions) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("doctaculous: read html: %w", err)
	}
	htmlOpts := []HTMLOption{}
	if opts.Print {
		htmlOpts = append(htmlOpts, WithPrintMedia()) // added in Step 6
	}
	if opts.Logf != nil {
		htmlOpts = append(htmlOpts, WithLogf(opts.Logf))
	}
	doc, err := OpenHTMLBytes(data, htmlOpts...)
	if err != nil {
		return err
	}
	return doc.WritePDF(ctx, out, opts)
}

// WritePDF writes an opened reflow document (HTML or DOCX) to out as a PDF. It
// returns an error if the document is not a reflow document (e.g. an opened PDF).
func (d *Document) WritePDF(ctx context.Context, out io.Writer, opts PDFOptions) error {
	rp, ok := d.r.(reflowPages)
	if !ok {
		return fmt.Errorf("doctaculous: WritePDF: document is not a reflow document")
	}
	if err := pdfwrite.WriteDocument(ctx, out, rp.layoutPages(), opts.toWriterOptions()); err != nil {
		return fmt.Errorf("doctaculous: write pdf: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run round-trip test**

Run: `go test ./pkg/doctaculous -run TestConvertHTMLToPDFRoundTrips`
Expected: PASS (after Step 6 wires `WithPrintMedia`; if `opts.Print` is false in the test, `WithPrintMedia` is never called, so this may pass before Step 6 — but build will fail on the undefined symbol; do Step 6 in the same commit).

- [ ] **Step 6: Wire the Print option into the cascade**

Add an `HTMLOption` that sets the cascade media to print. In `html_backend.go`, add to `htmlConfig` a `media css.Media` (default `MediaScreen`) and:

```go
// WithPrintMedia makes box generation honor @media print rules (for PDF output).
func WithPrintMedia() HTMLOption {
	return func(c *htmlConfig) { c.media = css.MediaPrint }
}
```

Thread `cfg.media` into the box-generation call (`layoutcss.BuildWithFonts` or wherever the cascade selects rules) so it calls `sheet.RulesForMedia(cfg.media)`. Find the cascade call site: `grep -rn "RulesForMedia\|\.Rules\b\|Cascade\|BuildWithFonts" pkg/layout/css pkg/html | head`. If box generation does not currently take a media parameter, add one (default `MediaScreen`) and pass it through from `htmlDocument`.

- [ ] **Step 7: Run full doctaculous suite**

Run: `go test ./pkg/doctaculous`
Expected: PASS (both new tests; existing HTML/DOCX goldens unchanged).

- [ ] **Step 8: Commit**

```bash
git add pkg/doctaculous
git commit -m "doctaculous: ConvertHTMLToPDF + WritePDF (+ DOCX bonus), PDFOptions, print media"
```

---

## Task 10: End-to-end fidelity — HTML→PDF→raster ≈ HTML→raster

**Files:**
- Modify: `pkg/doctaculous/pdfwrite_golden_test.go`
- Test data: none committed (generated + compared at runtime)

- [ ] **Step 1: Write the round-trip fidelity test**

Add to `pdfwrite_golden_test.go` a test that renders an HTML fixture two ways and compares: (a) directly via raster (the trusted path), (b) HTML→PDF→parse→raster, asserting the two images match within the project's standard tolerance (±4/channel, 0.2% differing-pixel budget — reuse the existing golden comparison helper; find it with `grep -rn "func.*compareImages\|diffPixels\|0.002\|tolerance" pkg/doctaculous pkg/render/raster`).

```go
func TestHTMLToPDFFidelity(t *testing.T) {
	cases := []struct{ name, html string }{
		{"text", `<!DOCTYPE html><html><head><style>body{margin:0}</style></head><body><p>Hello PDF</p></body></html>`},
		{"borders", `<!DOCTYPE html><html><head><style>body{margin:0}.b{border:4px solid #036;padding:8px}</style></head><body><div class="b">Boxed</div></body></html>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// (a) direct raster
			direct, err := OpenHTMLBytes([]byte(tc.html))
			if err != nil { t.Fatal(err) }
			imgDirect, err := direct.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
			if err != nil { t.Fatal(err) }

			// (b) HTML -> PDF -> raster
			var pdfBuf bytes.Buffer
			if err := ConvertHTMLToPDF(context.Background(), bytes.NewReader([]byte(tc.html)), &pdfBuf,
				PDFOptions{PageWidthPt: 612, PageHeightPt: 792}); err != nil {
				t.Fatal(err)
			}
			pdfDoc, err := OpenBytes(pdfBuf.Bytes())
			if err != nil { t.Fatal(err) }
			imgPDF, err := pdfDoc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
			if err != nil { t.Fatal(err) }

			// Compare the region where content overlaps (top-left of both pages).
			if !imagesRoughlyEqual(imgDirect, imgPDF, 4, 0.05) {
				t.Errorf("%s: HTML->PDF->raster differs from direct raster beyond tolerance", tc.name)
			}
		})
	}
}
```

Note: the direct raster page is a single tall page; the PDF page is Letter-sized with margins. They will NOT be pixel-aligned. Two realistic options — pick one and state it in the test comment:
- **Option A (recommended):** assert structurally instead of pixel-comparing — extract text from the PDF and assert the words are present, AND assert the PDF rasterizes non-blank in the content region (proves glyphs+borders drew). This avoids brittle alignment math.
- **Option B:** configure both paths to the same geometry (viewport = page content width, zero margin, single page) so the images ARE comparable, then use the tolerance compare.

Implement Option A as the robust default; keep a single Option-B case only if alignment proves stable.

- [ ] **Step 2: Add the searchable-text assertion helper**

Find how `pkg/pdf` / `pkg/doctaculous` exposes text extraction (it may not yet — if there is no text extraction API, assert instead that the generated PDF's content stream contains the expected `Tj`/`<hex>` for the glyphs and a `/ToUnicode` entry, by re-parsing with `pkg/pdf` and inspecting the page content). Search: `grep -rn "ToUnicode\|ExtractText\|func.*Text" pkg/pdf pkg/doctaculous | head`.

If no extraction exists, the assertion is: the generated PDF parses, has the expected page count, and its (decoded) content stream contains `BT`/`Tj`/`ET` and the document has a `/ToUnicode`. This proves real text, not outlines.

- [ ] **Step 3: Run the fidelity test**

Run: `go test ./pkg/doctaculous -run TestHTMLToPDFFidelity`
Expected: PASS

- [ ] **Step 4: Run the whole suite + race + vet + lint**

Run: `go test ./...`
Expected: PASS

Run: `go test -race ./pkg/render/pdfwrite ./pkg/doctaculous`
Expected: PASS

Run: `go vet ./... && golangci-lint run`
Expected: clean

- [ ] **Step 5: Commit**

```bash
git add pkg/doctaculous/pdfwrite_golden_test.go
git commit -m "test: HTML->PDF fidelity round-trip + searchable-text assertion"
```

---

## Task 11: Update roadmap docs

**Files:**
- Modify: `CLAUDE.md` (Status & roadmap)

- [ ] **Step 1: Move HTML→PDF to Done**

In `CLAUDE.md`, under "Done", add a bullet describing the HTML→PDF writer device (the `pkg/render/pdfwrite` backend, `DrawGlyph` seam, Identity-H CID embedding + ToUnicode, simple fragmentation, `ConvertHTMLToPDF`/`WritePDF`/`PDFOptions`, `@media print`, DOCX→PDF bonus). In the TODO list, narrow the HTML "remaining slices" entry to note the writer landed and that full CSS paged media (sub-project B) and the text backend are next.

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: mark HTML->PDF writer device done; note paged-media + text backend next"
```

---

## Self-review notes (resolved)

- **Spec coverage:** object serializer (T4), device ops (T6), font embed+subset+ToUnicode (T5), DrawGlyph seam (T1) + plumbing (T2,T3), fragmentation (T7), @media print (T8), public API + DOCX bonus (T9), fidelity round-trip + searchable-text (T10). All spec sections map to a task.
- **Y-flip strategy:** standardized on a single page-level CTM flip — the `pageDevice` (Task 6) emits raw page-space coordinates and the assembler (Task 7) prepends `1 0 0 -1 0 H cm` to each page's content. The device has no per-coordinate flip; do NOT add one.
- **Resource dict keys:** keys are stored WITHOUT a leading slash (the Dict serializer adds `/`). Task 7 Step 3 corrects the `fonts[...]` key.
- **Type consistency:** `Ref` is the object-id type throughout (0 = absent). `font.ProgramKind*`, `font.Face.GID/Outline/ProgramBytes/GlyphAdvance/UnitsPerEm`, `render.GlyphRef`/`glyphFace`, `pdfwrite.Options`/`WriteDocument`/`newPageDevice`/`fontEmbedder` names are used identically across tasks.
- **Subsetting risk:** Task 5 is split 5a (whole-program embed, ships working) → 5b (glyf zeroing subset). If 5b proves heavy, 5a is a correct fallback; the pipeline does not block on subsetting.
- **Graceful degradation:** non-embeddable face → outline fill (T5 emit returns 0; device fallback); unsupported shading → skip+log (T6); per-page recover (T7 worker recover); WritePDF on a non-reflow doc → typed error (T9).
- **Concurrency (per spec §Concurrency, decided after review):** Task 7 splits into a PARALLEL render phase (each band → its own `pageDevice`+`fontEmbedder`, pure `renderedPage` value, no shared state) and a SEQUENTIAL assembly phase (one writer, faces de-duped across pages, object ids assigned in document order). Bounded worker pool sized to `GOMAXPROCS` (overridable via `Options.Workers`/`PDFOptions.Workers`). Two determinism guards are mandatory and tested (`TestWriteDocumentDeterministic`): write worker results into per-index slots (never append from workers), and assign ids/resource-names via recorded first-use order (`fontEmbedder.order`), never by map iteration. `go test -race ./pkg/render/pdfwrite` is a required gate in T7 Step 6; `BenchmarkWriteDocument -cpu 1,4` proves the speedup in T7 Step 7.
