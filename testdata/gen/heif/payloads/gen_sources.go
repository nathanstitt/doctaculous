//go:build ignore

// gen_sources.go produces the deterministic source PNGs that the committed
// .hevc payloads in this directory are encoded from (see generate.sh and
// README.md). Content mixes gradients, a sinusoid, and LCG noise so encoded
// residuals exercise dense and sparse coefficient paths at every QP.
//
// Usage: go run gen_sources.go
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

func main() {
	for _, sz := range []struct{ w, h int }{{16, 16}, {30, 22}, {64, 64}, {96, 80}, {512, 512}} {
		img := image.NewNRGBA(image.Rect(0, 0, sz.w, sz.h))
		lcg := uint32(0x12345678)
		for y := 0; y < sz.h; y++ {
			for x := 0; x < sz.w; x++ {
				lcg = lcg*1664525 + 1013904223
				noise := int(lcg>>24) % 32
				r := (x*255/sz.w + noise) & 255
				g := (y*255/sz.h + int(64*math.Sin(float64(x+y)/7.0)) + 128 + noise/2) & 255
				b := ((x ^ y) + noise) & 255
				img.SetNRGBA(x, y, color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255})
			}
		}
		name := fmt.Sprintf("src-%dx%d.png", sz.w, sz.h)
		f, err := os.Create(name)
		if err != nil {
			panic(err)
		}
		if err := png.Encode(f, img); err != nil {
			panic(err)
		}
		f.Close()
		fmt.Println("wrote", name)
	}
}
