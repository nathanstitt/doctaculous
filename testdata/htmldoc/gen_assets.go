//go:build ignore

// Command gen_assets generates the small, hermetic raster fixtures the HTML
// showcase document references (img/quad.png, img/photo.jpg, img/icon.gif). It is
// run by hand (`go run gen_assets.go` from this directory) when the images need to
// change; the produced files are committed so the golden test stays offline. Each
// image exercises a distinct decoder on the engine's <img> path: PNG, baseline
// JPEG (DCTDecode-equivalent), and GIF.
package main

import (
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
)

func main() {
	must(writePNG("img/quad.png", quad(64)))
	must(writeJPEG("img/photo.jpg", gradientDisc(160, 120)))
	must(writeGIF("img/icon.gif", monogram(48)))
	must(writePNG("img/tile.png", weave(32)))
}

// weave is a small seamless tile for background-image: a faint diagonal hatch in the
// showcase's parchment palette so it tiles into a subtle paper texture. Seamless
// because the hatch period divides the tile size.
func weave(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	base := color.RGBA{0xf0, 0xe6, 0xc4, 0xff}  // parchment
	hatch := color.RGBA{0xe2, 0xd2, 0xa0, 0xff} // a shade darker
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			c := base
			if (x+y)%8 == 0 || (x-y+size)%8 == 0 { // two crossing diagonals
				c = hatch
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// quad is a four-quadrant square: an orientation tell-tale (TL ink, TR gold, BL
// rust, BR slate) so a rendered image's flip/rotation is unambiguous on review.
func quad(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	half := size / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var c color.RGBA
			switch {
			case x < half && y < half:
				c = color.RGBA{0x1a, 0x1a, 0x18, 0xff} // top-left: near-black ink
			case x >= half && y < half:
				c = color.RGBA{0xd9, 0xa5, 0x21, 0xff} // top-right: gold
			case x < half && y >= half:
				c = color.RGBA{0xa6, 0x4b, 0x2a, 0xff} // bottom-left: rust
			default:
				c = color.RGBA{0x3a, 0x4a, 0x5a, 0xff} // bottom-right: slate
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// gradientDisc is a non-flat photographic-ish image (a soft radial disc over a
// diagonal wash) so the JPEG path is exercised on real continuous tone, not a
// flat fill that would compress to nothing.
func gradientDisc(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	cx, cy := float64(w)/2, float64(h)/2
	maxR := math.Hypot(cx, cy)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Diagonal background wash (warm to cool).
			t := float64(x+y) / float64(w+h)
			bg := lerp3(0xe8, 0xdc, 0xc8, 0x2a, 0x3a, 0x4a, t)
			// Radial disc highlight in the center.
			r := math.Hypot(float64(x)-cx, float64(y)-cy) / maxR
			glow := math.Max(0, 1-r*1.6)
			c := color.RGBA{
				clamp(bg[0] + 0x70*glow),
				clamp(bg[1] + 0x55*glow),
				clamp(bg[2] + 0x20*glow),
				0xff,
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// monogram is a tiny flat-color "badge" (a gold ring on transparent) suited to
// GIF's indexed palette: a handful of solid colors, sharp edges.
func monogram(size int) *image.Paletted {
	pal := color.Palette{
		color.RGBA{0, 0, 0, 0},             // 0: transparent
		color.RGBA{0xd9, 0xa5, 0x21, 0xff}, // 1: gold ring
		color.RGBA{0x1a, 0x1a, 0x18, 0xff}, // 2: ink center
	}
	img := image.NewPaletted(image.Rect(0, 0, size, size), pal)
	cx, cy := float64(size)/2, float64(size)/2
	outer, inner := float64(size)/2-1, float64(size)/2-7
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			r := math.Hypot(float64(x)-cx, float64(y)-cy)
			switch {
			case r <= inner:
				img.SetColorIndex(x, y, 2) // ink center
			case r <= outer:
				img.SetColorIndex(x, y, 1) // gold ring
			default:
				img.SetColorIndex(x, y, 0) // transparent
			}
		}
	}
	return img
}

func lerp3(r0, g0, b0, r1, g1, b1 float64, t float64) [3]float64 {
	return [3]float64{r0 + (r1-r0)*t, g0 + (g1-g0)*t, b0 + (b1-b0)*t}
}

func clamp(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func writeJPEG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return jpeg.Encode(f, img, &jpeg.Options{Quality: 82})
}

func writeGIF(path string, img *image.Paletted) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return gif.Encode(f, img, &gif.Options{NumColors: 4})
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
