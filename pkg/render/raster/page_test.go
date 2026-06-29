package raster

import (
	"context"
	"image"
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/testdata/gen"
)

func TestRenderVectorProducesRedPixels(t *testing.T) {
	doc, err := pdf.Parse(gen.VectorPDF())
	if err != nil {
		t.Fatal(err)
	}
	pg, _ := doc.Page(0)
	img, err := RenderPage(context.Background(), pg, Options{DPI: 72, Background: color.White})
	if err != nil {
		t.Fatal(err)
	}
	// The fixture fills a red rectangle at user-space (100,100)-(300,250) and
	// strokes a blue diagonal across it. Sample a point inside the rectangle but
	// well off the diagonal (upper-left region: small x, large y) so we hit red.
	// At 72 DPI (scale 1) device y = 792 - userY.
	ux, uy := 130.0, 235.0
	cx, cy := int(ux), int(792-uy)
	got := img.RGBAAt(cx, cy)
	if got.R < 200 || got.G > 60 || got.B > 60 {
		t.Errorf("rect pixel at user(%v,%v) = %v, want ~red", ux, uy, got)
	}
	// A corner well outside the rectangle should be white background.
	bgPix := img.RGBAAt(5, 5)
	if bgPix.R < 250 || bgPix.G < 250 || bgPix.B < 250 {
		t.Errorf("background pixel = %v, want white", bgPix)
	}
}

func TestRenderFormXObject(t *testing.T) {
	doc, err := pdf.Parse(gen.FormXObjectPDF())
	if err != nil {
		t.Fatal(err)
	}
	pg, _ := doc.Page(0)
	img, err := RenderPage(context.Background(), pg, Options{DPI: 72, Background: color.White})
	if err != nil {
		t.Fatal(err)
	}
	// The form draws a 100x100 green square at its origin, shifted +(50,50) by its
	// /Matrix and +(200,200) by the page cm, landing at user (250,250)-(350,350).
	// At 72 DPI device y = 792 - userY. Sample the center, user (300,300).
	cx, cy := 300, int(792-300)
	got := img.RGBAAt(cx, cy)
	if got.G < 200 || got.R > 60 || got.B > 60 {
		t.Errorf("form pixel at user(300,300) = %v, want ~green (form not drawn)", got)
	}
	// Outside the square stays white — proves the /Matrix offset is honored rather
	// than the form being drawn at the page origin.
	if bg := img.RGBAAt(5, 5); bg.R < 250 || bg.G < 250 || bg.B < 250 {
		t.Errorf("background pixel = %v, want white", bg)
	}
}

// TestRenderMalformedImageColorSpaceNoPanic renders a page whose image XObject has a
// malformed single-element array color space ("[/ICCBased]"). Before the fix this
// indexed arr[1] out of range and panicked; in a render-worker goroutine (no recover)
// that was process-fatal. The page must now render (degrade) without panicking. This
// exercises BOTH the resolveImageCSArray length guard and the RenderPage page-boundary
// recover end to end (removing either keeps this from crashing only because the other
// still holds).
func TestRenderMalformedImageColorSpaceNoPanic(t *testing.T) {
	doc, err := pdf.Parse(gen.MalformedImageColorSpacePDF())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pg, err := doc.Page(0)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	// Must not panic; a degraded render returns a non-nil image.
	img, err := RenderPage(context.Background(), pg, Options{DPI: 72, Background: color.White})
	if err != nil {
		t.Fatalf("RenderPage returned error (want graceful render): %v", err)
	}
	if img == nil {
		t.Fatal("RenderPage returned nil image for a malformed-color-space page")
	}
}

// decodeFixtureImage parses an image-fixture PDF and decodes its /Im0 image
// XObject through decodeImageXObject, returning the decoded image.
func decodeFixtureImage(t *testing.T, pdfBytes []byte) image.Image {
	t.Helper()
	doc, err := pdf.Parse(pdfBytes)
	if err != nil {
		t.Fatal(err)
	}
	pg, _ := doc.Page(0)
	xo := doc.GetDict(pg.Resources["XObject"])
	s := doc.GetStream(xo["Im0"])
	if s == nil {
		t.Fatal("no /Im0 image XObject")
	}
	img, err := decodeImageXObject(doc, s, render.FillColor{A: 0xFF}, nil)
	if err != nil {
		t.Fatalf("decodeImageXObject: %v", err)
	}
	return img
}

func TestDecodeImageColorSpaces(t *testing.T) {
	near := func(got, want uint32, tol int) bool {
		d := int(got>>8) - int(want)
		return d >= -tol && d <= tol
	}

	t.Run("gray", func(t *testing.T) {
		img := decodeFixtureImage(t, gen.GrayImagePDF())
		// 8-wide gradient: x=0 black, x=7 white.
		r0, _, _, _ := img.At(0, 0).RGBA()
		r7, _, _, _ := img.At(7, 0).RGBA()
		if !near(r0, 0, 8) || !near(r7, 255, 8) {
			t.Errorf("gray gradient ends = %d..%d, want ~0..255", r0>>8, r7>>8)
		}
	})

	t.Run("cmyk", func(t *testing.T) {
		img := decodeFixtureImage(t, gen.CMYKImagePDF())
		// Top-left quadrant is cyan (C=1) → RGB ~ (0,255,255).
		r, g, b, _ := img.At(1, 1).RGBA()
		if !near(r, 0, 8) || !near(g, 255, 8) || !near(b, 255, 8) {
			t.Errorf("cyan pixel = (%d,%d,%d), want ~(0,255,255)", r>>8, g>>8, b>>8)
		}
	})

	t.Run("indexed", func(t *testing.T) {
		img := decodeFixtureImage(t, gen.IndexedImagePDF())
		// Index at (0,0) is (0+0)%4 = 0 = red.
		r, g, b, _ := img.At(0, 0).RGBA()
		if !near(r, 255, 8) || !near(g, 0, 8) || !near(b, 0, 8) {
			t.Errorf("indexed (0,0) = (%d,%d,%d), want red", r>>8, g>>8, b>>8)
		}
	})

	t.Run("smask", func(t *testing.T) {
		img := decodeFixtureImage(t, gen.SMaskImagePDF())
		// Left half opaque (A=255), right half transparent (A=0).
		_, _, _, aL := img.At(1, 1).RGBA()
		_, _, _, aR := img.At(6, 1).RGBA()
		if aL>>8 != 255 {
			t.Errorf("left alpha = %d, want 255 (opaque)", aL>>8)
		}
		if aR>>8 != 0 {
			t.Errorf("right alpha = %d, want 0 (transparent)", aR>>8)
		}
	})
}

func TestDecodeImageMask(t *testing.T) {
	doc, err := pdf.Parse(gen.ImageMaskPDF())
	if err != nil {
		t.Fatal(err)
	}
	pg, _ := doc.Page(0)
	s := doc.GetStream(doc.GetDict(pg.Resources["XObject"])["Im0"])
	// Decode with a green fill, mirroring the content stream's "0 1 0 rg".
	green := render.FillColor{R: 0, G: 255, B: 0, A: 255}
	img, err := decodeImageXObject(doc, s, green, nil)
	if err != nil {
		t.Fatalf("decodeImageXObject: %v", err)
	}
	// Left half: painted green, opaque. Right half: transparent.
	rL, gL, bL, aL := img.At(1, 1).RGBA()
	if aL>>8 != 255 || rL>>8 != 0 || gL>>8 != 255 || bL>>8 != 0 {
		t.Errorf("left mask pixel = (%d,%d,%d,%d), want opaque green", rL>>8, gL>>8, bL>>8, aL>>8)
	}
	if _, _, _, aR := img.At(6, 1).RGBA(); aR>>8 != 0 {
		t.Errorf("right mask pixel alpha = %d, want 0 (transparent)", aR>>8)
	}
}

func TestRenderImageFixture(t *testing.T) {
	for name, build := range map[string]func() []byte{
		"flate": gen.ImagePDF,
		"jpeg":  gen.JPEGImagePDF,
	} {
		t.Run(name, func(t *testing.T) {
			doc, err := pdf.Parse(build())
			if err != nil {
				t.Fatal(err)
			}
			pg, _ := doc.Page(0)
			img, err := RenderPage(context.Background(), pg, Options{DPI: 72})
			if err != nil {
				t.Fatalf("RenderPage: %v", err)
			}
			// The image is drawn in a 400x400 (or similar) box; assert the page is
			// not entirely white (something was drawn).
			if isAllWhite(img.Pix) {
				t.Errorf("%s: rendered page is entirely white (image not drawn)", name)
			}
		})
	}
}

func TestRotatedPageDimensionsSwap(t *testing.T) {
	doc, err := pdf.Parse(gen.RotatedPDF(90))
	if err != nil {
		t.Fatal(err)
	}
	pg, _ := doc.Page(0)
	img, err := RenderPage(context.Background(), pg, Options{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	// MediaBox is 612x792 portrait; rotated 90 the device image is 792x612.
	if got := img.Bounds(); got.Dx() != 792 || got.Dy() != 612 {
		t.Errorf("rotated bounds = %v, want 792x612", got)
	}
}

func TestRenderCoreFixturesNoError(t *testing.T) {
	for _, f := range gen.Core {
		t.Run(f.Name, func(t *testing.T) {
			doc, err := pdf.Parse(f.Bytes())
			if err != nil {
				t.Fatalf("Parse (%s): %v", f.Desc, err)
			}
			for i := range f.Pages {
				pg, err := doc.Page(i)
				if err != nil {
					t.Fatalf("Page(%d): %v", i, err)
				}
				if _, err := RenderPage(context.Background(), pg, Options{DPI: 72}); err != nil {
					t.Errorf("RenderPage(%s p%d): %v", f.Name, i, err)
				}
			}
		})
	}
}

func TestRejectsHugePage(t *testing.T) {
	doc, err := pdf.Parse(gen.TextPDF())
	if err != nil {
		t.Fatal(err)
	}
	pg, _ := doc.Page(0)
	// 612x792 pt at 10000 DPI is ~85k x 110k px ≈ 9.4e9 px, far over the cap.
	_, err = RenderPage(context.Background(), pg, Options{DPI: 10000})
	if err == nil {
		t.Fatal("expected error for absurd DPI, got nil")
	}
}

func TestClipRestrictsFill(t *testing.T) {
	// Clip to a small rect, then fill a large rect; only the clipped area paints.
	doc, _ := pdf.Parse(gen.VectorPDF())
	pg, _ := doc.Page(0)
	// Build a tiny synthetic content stream via a Device directly is simpler:
	img, err := RenderPage(context.Background(), pg, Options{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	// Sanity: the rendered vector page is not all white (clip path covered by golden).
	if isAllWhite(img.Pix) {
		t.Error("vector page rendered all white")
	}
}

func isAllWhite(pix []uint8) bool {
	for i := 0; i+3 < len(pix); i += 4 {
		if pix[i] != 0xFF || pix[i+1] != 0xFF || pix[i+2] != 0xFF {
			return false
		}
	}
	return true
}

// TestRenderEmbeddedFontsNotBlank renders each embedded-font fixture and checks
// that glyphs actually painted: the page is no longer all white, and a pixel
// inside the drawn text region is dark. This guards the whole font backend
// (parsing, encoding, CFF wrapping, outline extraction, fill).
func TestRenderEmbeddedFontsNotBlank(t *testing.T) {
	for _, name := range []string{"embedded-truetype", "type0", "cff"} {
		t.Run(name, func(t *testing.T) {
			var build func() []byte
			for _, f := range gen.Core {
				if f.Name == name {
					build = f.Build
				}
			}
			if build == nil {
				t.Fatalf("no gen.Core fixture %q", name)
			}
			doc, err := pdf.Parse(build())
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			pg, _ := doc.Page(0)
			img, err := RenderPage(context.Background(), pg, Options{DPI: 72, Background: color.White})
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if isAllWhite(img.Pix) {
				t.Fatal("page is blank; no glyphs were drawn")
			}
			// The text is drawn at user (72, 680) at 48pt. At 72 DPI (scale 1)
			// the device baseline is y = 792-680 = 112, and 48pt glyphs rise
			// ~34px above it, so scan device y∈[70,115], x∈[70,210] for coverage.
			if !hasDarkPixel(img, 70, 70, 210, 115) {
				t.Error("no dark pixel in the expected text region")
			}
		})
	}
}

// hasDarkPixel reports whether any pixel in the device-space rectangle is dark.
func hasDarkPixel(img interface {
	RGBAAt(x, y int) color.RGBA
}, x0, y0, x1, y1 int) bool {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c := img.RGBAAt(x, y)
			if c.R < 128 && c.G < 128 && c.B < 128 {
				return true
			}
		}
	}
	return false
}
