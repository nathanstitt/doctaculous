package doctaculous

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// colorInk rasterizes html and counts total ink and NON-GREYSCALE pixels.
//
// The colour count is the whole point: an emoji with no colour support still paints —
// as black tofu or a monochrome outline — so "something was drawn" is true both when
// the feature works and when it does not. Only a channel-difference test distinguishes
// them, which is the trap the original gap report called out.
func colorInk(t *testing.T, html string, w, h int, opts ...HTMLOption) (ink, colored int) {
	t.Helper()
	opts = append([]HTMLOption{WithPageSize(float64(w), float64(h))}, opts...)
	doc, err := OpenHTMLBytes([]byte(html), opts...)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{
		MaxWidthPx: w, MaxHeightPx: h, Background: color.White})
	if err != nil {
		t.Fatalf("raster: %v", err)
	}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bb, _ := img.At(x, y).RGBA()
			r8, g8, b8 := r>>8, g>>8, bb>>8
			if r8 != 255 || g8 != 255 || b8 != 255 {
				ink++
				if r8 != g8 || g8 != b8 {
					colored++
				}
			}
		}
	}
	return ink, colored
}

// emojiFontLoader serves a committed colour-emoji fixture as an @font-face source, so
// the test does not depend on what the host has installed.
func emojiFontLoader(t *testing.T, name string) resource.MapLoader {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "gen", "fonts", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return resource.MapLoader{"emoji.ttf": {Data: b}}
}

func emojiPage(sizePx int, text string) string {
	return fmt.Sprintf(`<html><head><style>
@font-face { font-family: "TestEmoji"; src: url("emoji.ttf") format("truetype"); }
body { margin: 0; font-family: "TestEmoji"; }
</style></head><body><p style="font-size:%dpx">%s</p></body></html>`, sizePx, text)
}

// A COLR/CPAL font paints its emoji in COLOUR. Before this the layers were never read,
// so the base glyph — which is empty in a colour font — painted nothing at all.
func TestCOLRFontPaintsInColor(t *testing.T) {
	loader := emojiFontLoader(t, "NotoColorEmoji-COLRv1.ttf")
	ink, colored := colorInk(t, emojiPage(64, "😀🎉❤👍🌟"), 480, 120, WithResourceLoader(loader))
	if ink == 0 {
		t.Fatal("no ink at all for a page of colour emoji")
	}
	if colored == 0 {
		t.Fatalf("ink=%d but no non-greyscale pixels: the glyphs painted, but monochrome", ink)
	}
}

// A CBDT/CBLC (bitmap-strike) font paints in colour too. It is a completely separate
// code path from COLR — an image rather than layered outlines — and Apple Color Emoji
// has no COLR table at all, so this is the path most hosts actually use.
func TestCBDTFontPaintsInColor(t *testing.T) {
	loader := emojiFontLoader(t, "NotoColorEmoji-CBDT.ttf")
	ink, colored := colorInk(t, emojiPage(64, "😀🎉❤👍🌟"), 480, 120, WithResourceLoader(loader))
	if ink == 0 {
		t.Fatal("no ink at all for a page of bitmap colour emoji")
	}
	if colored == 0 {
		t.Fatalf("ink=%d but no non-greyscale pixels", ink)
	}
}

// Falsifiability control: ordinary text through the same pipeline paints ink but NO
// colour, so the colour assertions above are testing the colour-font path rather than
// something incidental to rasterizing a page.
func TestPlainTextPaintsNoColor(t *testing.T) {
	html := `<html><body style="margin:0"><p style="font-size:64px">ABC</p></body></html>`
	ink, colored := colorInk(t, html, 480, 120)
	if ink == 0 {
		t.Fatal("no ink for plain text")
	}
	if colored != 0 {
		t.Errorf("plain black text produced %d non-greyscale pixels", colored)
	}
}

// Colour emoji render at small sizes as well as large — a bitmap font must pick a
// strike near the used size rather than always the largest.
func TestColorEmojiPaintAtSmallSize(t *testing.T) {
	for _, name := range []string{"NotoColorEmoji-COLRv1.ttf", "NotoColorEmoji-CBDT.ttf"} {
		t.Run(name, func(t *testing.T) {
			loader := emojiFontLoader(t, name)
			_, colored := colorInk(t, emojiPage(14, "😀🎉❤👍🌟"), 200, 60, WithResourceLoader(loader))
			if colored == 0 {
				t.Error("no colour at 14px")
			}
		})
	}
}

// A colour glyph keeps the FONT's colours rather than taking the CSS text colour: an
// emoji inside red text is still its own palette. (A COLR layer may opt into the text
// colour via the palette's foreground sentinel; that is the font's choice, not the
// document's.)
func TestColorEmojiIgnoreTextColor(t *testing.T) {
	loader := emojiFontLoader(t, "NotoColorEmoji-COLRv1.ttf")
	html := `<html><head><style>
@font-face { font-family: "TestEmoji"; src: url("emoji.ttf") format("truetype"); }
</style></head><body style="margin:0">
<p style="font-family:TestEmoji;font-size:64px;color:#0000ff">❤</p></body></html>`
	doc, err := OpenHTMLBytes([]byte(html), WithPageSize(120, 100), WithResourceLoader(loader))
	if err != nil {
		t.Fatal(err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{
		MaxWidthPx: 120, MaxHeightPx: 100, Background: color.White})
	if err != nil {
		t.Fatal(err)
	}
	// The heart must contribute RED pixels despite the blue text colour.
	red := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bb, _ := img.At(x, y).RGBA()
			if r>>8 > 180 && g>>8 < 120 && bb>>8 < 120 {
				red++
			}
		}
	}
	if red == 0 {
		t.Error("no red pixels: the colour glyph took the CSS text colour instead of its palette")
	}
}

// An emoji in ordinary prose resolves through the script-fallback chain — no font
// named, no @font-face — provided the host has an emoji font installed. This is the
// case that makes emoji "just work" in a document, and it is skipped rather than
// failed on a bare host with no emoji font.
func TestEmojiFallbackInPlainText(t *testing.T) {
	html := `<html><body style="margin:0"><p style="font-size:48px">Hi 👍</p></body></html>`
	ink, colored := colorInk(t, html, 300, 90)
	if ink == 0 {
		t.Fatal("no ink at all, not even the text")
	}
	plainInk, _ := colorInk(t, `<html><body style="margin:0"><p style="font-size:48px">Hi</p></body></html>`, 300, 90)
	if ink <= plainInk {
		t.Skip("no emoji font installed on this host; the fallback chain has nothing to find")
	}
	if colored == 0 {
		t.Error("the emoji painted, but with no colour")
	}
}
