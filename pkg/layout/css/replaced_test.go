package css

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/html"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// --- test-image encoders (hermetic: tiny images built in memory, no committed
// binaries) ---

// solidImage returns a w×h image filled with c.
func solidImage(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func pngBytes(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, solidImage(w, h, c)); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func jpegBytes(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, solidImage(w, h, c), nil); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}

func gifBytes(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, solidImage(w, h, c), nil); err != nil {
		t.Fatalf("gif.Encode: %v", err)
	}
	return buf.Bytes()
}

// --- loader-aware layout helpers ---

// layoutWithLoader parses HTML, builds the box tree resolving refs through loader,
// and lays it out, returning the root fragment. logf may be nil.
func layoutWithLoader(t *testing.T, src string, viewportW float64, loader resource.ResourceLoader, logf func(string, ...any)) *Fragment {
	t.Helper()
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	root, err := Build(context.Background(), doc, loader, logf)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	frag := New(layoutfont.NewFaceCache(), loader, logf).layoutTree(context.Background(), root, viewportW)
	if frag == nil {
		t.Fatalf("layoutTree returned nil for %q", src)
	}
	return frag
}

// imgFragment lays out a document whose body contains a single <img> (possibly
// wrapped), and returns the replaced box's fragment (the one carrying an Image).
func imgFragment(t *testing.T, src string, viewportW float64, loader resource.ResourceLoader) *Fragment {
	t.Helper()
	root := layoutWithLoader(t, src, viewportW, loader, nil)
	f := findImageFragment(root)
	if f == nil {
		t.Fatalf("no image fragment found in layout of %q", src)
	}
	return f
}

// findImageFragment walks the fragment tree depth-first for the first fragment
// carrying replaced-image content.
func findImageFragment(f *Fragment) *Fragment {
	if f.Image != nil {
		return f
	}
	for _, c := range f.Children {
		if got := findImageFragment(c); got != nil {
			return got
		}
	}
	return nil
}

// pngLoader serves a single PNG of the given intrinsic size at "img.png".
func pngLoader(t *testing.T, iw, ih int) resource.MapLoader {
	t.Helper()
	return resource.MapLoader{
		"img.png": {Data: pngBytes(t, iw, ih, color.RGBA{10, 20, 30, 255}), ContentType: "image/png"},
	}
}

// --- sizing tests ---

// TestReplacedExplicitAttrSize: width/height attributes set the used size when no
// CSS dimensions are given.
func TestReplacedExplicitAttrSize(t *testing.T) {
	f := imgFragment(t, `<img src="img.png" width="40" height="20">`, 800, pngLoader(t, 100, 50))
	if f.W != 40 || f.H != 20 {
		t.Errorf("size = %vx%v, want 40x20 (from attrs)", f.W, f.H)
	}
	if f.Image == nil || f.Image.Img == nil {
		t.Errorf("expected a decoded image on the fragment")
	}
}

// TestReplacedCSSOverridesAttr: a CSS width/height beats the presentational
// width/height attribute (CSS precedence).
func TestReplacedCSSOverridesAttr(t *testing.T) {
	f := imgFragment(t, `<img src="img.png" width="40" height="20" style="width:80px;height:60px">`, 800, pngLoader(t, 100, 50))
	if f.W != 80 || f.H != 60 {
		t.Errorf("size = %vx%v, want 80x60 (CSS over attr)", f.W, f.H)
	}
}

// TestReplacedIntrinsicSize: with no width/height, the image's own pixel size is
// used.
func TestReplacedIntrinsicSize(t *testing.T) {
	f := imgFragment(t, `<img src="img.png">`, 800, pngLoader(t, 120, 90))
	if f.W != 120 || f.H != 90 {
		t.Errorf("size = %vx%v, want 120x90 (intrinsic)", f.W, f.H)
	}
}

// TestReplacedAspectRatioFromWidth: only width given -> height derived from the
// intrinsic aspect ratio.
func TestReplacedAspectRatioFromWidth(t *testing.T) {
	// intrinsic 200x100 (2:1); width 50 -> height 25.
	f := imgFragment(t, `<img src="img.png" style="width:50px">`, 800, pngLoader(t, 200, 100))
	if f.W != 50 || f.H != 25 {
		t.Errorf("size = %vx%v, want 50x25 (aspect ratio from width)", f.W, f.H)
	}
}

// TestReplacedAspectRatioFromHeight: only height given -> width derived from the
// intrinsic aspect ratio.
func TestReplacedAspectRatioFromHeight(t *testing.T) {
	// intrinsic 200x100 (2:1); height 40 -> width 80.
	f := imgFragment(t, `<img src="img.png" style="height:40px">`, 800, pngLoader(t, 200, 100))
	if f.W != 80 || f.H != 40 {
		t.Errorf("size = %vx%v, want 80x40 (aspect ratio from height)", f.W, f.H)
	}
}

// TestReplacedMaxWidthClamp: max-width clamps the used width.
func TestReplacedMaxWidthClamp(t *testing.T) {
	f := imgFragment(t, `<img src="img.png" width="200" height="100" style="max-width:80px">`, 800, pngLoader(t, 200, 100))
	if f.W != 80 {
		t.Errorf("width = %v, want 80 (max-width clamp)", f.W)
	}
}

// TestReplacedMinHeightClamp: min-height raises the used height.
func TestReplacedMinHeightClamp(t *testing.T) {
	f := imgFragment(t, `<img src="img.png" width="40" height="20" style="min-height:50px">`, 800, pngLoader(t, 40, 20))
	if f.H != 50 {
		t.Errorf("height = %v, want 50 (min-height clamp)", f.H)
	}
}

// TestReplacedPercentHeightNoBasisIsAuto: a percentage height has no basis in this
// single-axis model, so it is treated as auto (NOT a zero-basis 0) — the image keeps
// its intrinsic height rather than collapsing to nothing.
func TestReplacedPercentHeightNoBasisIsAuto(t *testing.T) {
	// intrinsic 40x20, only height:50% (no basis) -> auto -> intrinsic 40x20.
	f := imgFragment(t, `<img src="img.png" style="height:50%">`, 800, pngLoader(t, 40, 20))
	if f.W != 40 || f.H != 20 {
		t.Errorf("size = %vx%v, want 40x20 (percent height with no basis -> auto -> intrinsic)", f.W, f.H)
	}
}

// TestReplacedPercentHeightWithWidthDerivesFromRatio: with a width given and a
// basis-less percentage height (auto), the height derives from the aspect ratio, not
// from the unresolved percentage.
func TestReplacedPercentHeightWithWidthDerivesFromRatio(t *testing.T) {
	// intrinsic 40x20 (2:1); width 100, height:50% (auto) -> height 50 from ratio.
	f := imgFragment(t, `<img src="img.png" width="100" style="height:50%">`, 800, pngLoader(t, 40, 20))
	if f.W != 100 || f.H != 50 {
		t.Errorf("size = %vx%v, want 100x50 (height from ratio, percent ignored)", f.W, f.H)
	}
}

// TestReplacedJPEGAndGIFDecode: JPEG and GIF sources decode for intrinsic sizing
// too (not just PNG).
func TestReplacedJPEGAndGIFDecode(t *testing.T) {
	loader := resource.MapLoader{
		"j.jpg": {Data: jpegBytes(t, 64, 32, color.RGBA{200, 100, 50, 255}), ContentType: "image/jpeg"},
		"g.gif": {Data: gifBytes(t, 24, 48, color.RGBA{50, 100, 200, 255}), ContentType: "image/gif"},
	}
	jf := imgFragment(t, `<img src="j.jpg">`, 800, loader)
	if jf.W != 64 || jf.H != 32 {
		t.Errorf("jpeg size = %vx%v, want 64x32", jf.W, jf.H)
	}
	gf := imgFragment(t, `<img src="g.gif">`, 800, loader)
	if gf.W != 24 || gf.H != 48 {
		t.Errorf("gif size = %vx%v, want 24x48", gf.W, gf.H)
	}
}

// TestReplacedContentTypeSniff: a loader that reports no content type still decodes
// via format sniffing (the blank-imported decoders register with image.Decode).
func TestReplacedContentTypeSniff(t *testing.T) {
	loader := resource.MapLoader{
		"mystery": {Data: pngBytes(t, 33, 22, color.RGBA{1, 2, 3, 255}), ContentType: ""},
	}
	f := imgFragment(t, `<img src="mystery">`, 800, loader)
	if f.W != 33 || f.H != 22 {
		t.Errorf("sniffed size = %vx%v, want 33x22", f.W, f.H)
	}
	if f.Image == nil || f.Image.Img == nil {
		t.Errorf("expected sniffed image to decode")
	}
}

// --- block / inline / inline-block placement ---

// TestReplacedBlockSizing: a display:block <img> with width:auto uses its INTRINSIC
// width (not the containing-block fill a normal block gets), and paints an image.
func TestReplacedBlockSizing(t *testing.T) {
	f := imgFragment(t, `<img src="img.png" style="display:block">`, 800, pngLoader(t, 150, 75))
	if f.W != 150 || f.H != 75 {
		t.Errorf("block img size = %vx%v, want 150x75 (intrinsic, not viewport fill)", f.W, f.H)
	}
	if f.Image == nil || f.Image.Img == nil {
		t.Errorf("block img has no decoded image")
	}
}

// TestReplacedBlockStacksAsBlock pins B1 (the F-E bug): a display:block <img> between
// two text runs must STACK as a block — the browser produces three stacked blocks
// (anonymous "AAA", the img block, anonymous "BBB"), NOT one inline line. The bug was
// that isBlockLevelOuter/the block-stacker child guard read Kind.IsBlockLevel(), which
// is false for BoxReplaced regardless of display, so the block img was treated
// inline-level and either flowed on the text line or was skipped. Mutation-verify:
// revert isBlockLevelReplaced (or the anon.go BoxReplaced branch) and the img no longer
// stacks (the div's child count / vertical order changes).
func TestReplacedBlockStacksAsBlock(t *testing.T) {
	src := `<div>AAA<img src="img.png" style="display:block;width:40px;height:40px">BBB</div>`
	root := layoutWithLoader(t, src, 800, pngLoader(t, 40, 40), nil)
	// root -> body -> div; the div must hold three stacked block children.
	body := root.Children[len(root.Children)-1]
	if len(body.Children) != 1 {
		t.Fatalf("want 1 body child (the div), got %d", len(body.Children))
	}
	div := body.Children[0]
	if len(div.Children) != 3 {
		t.Fatalf("div should hold 3 stacked blocks (anon AAA, img, anon BBB), got %d", len(div.Children))
	}
	// The img is the middle child and carries the image.
	imgChild := div.Children[1]
	if imgChild.Image == nil {
		t.Errorf("middle child should be the block img (has Image), got Image=nil")
	}
	if imgChild.W != 40 || imgChild.H != 40 {
		t.Errorf("block img size = %vx%v, want 40x40", imgChild.W, imgChild.H)
	}
	// Vertical stacking: AAA above the img, BBB below it.
	aaa, bbb := div.Children[0], div.Children[2]
	if !(aaa.Y < imgChild.Y && imgChild.Y < bbb.Y) {
		t.Errorf("blocks must stack vertically: AAA.Y=%.1f img.Y=%.1f BBB.Y=%.1f", aaa.Y, imgChild.Y, bbb.Y)
	}
	// And they do NOT share a baseline/line (the img is not an inline atom): each block's
	// Y strictly increases by at least the prior block's height.
	if imgChild.Y < aaa.Y+aaa.H-1e-6 {
		t.Errorf("img must start at/below AAA's bottom (block flow), img.Y=%.1f AAA bottom=%.1f", imgChild.Y, aaa.Y+aaa.H)
	}
}

// TestReplacedInlineBlockSizing: a display:inline-block <img> sizes via the replaced
// algorithm (a replaced box is replaced regardless of inline-block display).
func TestReplacedInlineBlockSizing(t *testing.T) {
	f := imgFragment(t, `<div><img src="img.png" style="display:inline-block;width:60px"></div>`, 800, pngLoader(t, 120, 40))
	// 120x40 (3:1); width 60 -> height 20.
	if f.W != 60 || f.H != 20 {
		t.Errorf("inline-block img size = %vx%v, want 60x20", f.W, f.H)
	}
	if f.Image == nil || f.Image.Img == nil {
		t.Errorf("inline-block img has no decoded image")
	}
}

// TestReplacedInlineContentRect: an inline <img> with its own padding+border places
// the image content box inside that decoration.
func TestReplacedInlineContentRect(t *testing.T) {
	f := imgFragment(t, `<div><img src="img.png" width="40" height="40" style="padding:5px;border:2px solid #000"></div>`, 800, pngLoader(t, 40, 40))
	// border box = 40 + 2*(5 pad + 2 border) = 54.
	if f.W != 54 || f.H != 54 {
		t.Errorf("border box = %vx%v, want 54x54", f.W, f.H)
	}
	if f.Image == nil {
		t.Fatalf("no image content")
	}
	// content box width/height is the 40x40 image; CX/CY offset by border+padding (7).
	if f.Image.CW != 40 || f.Image.CH != 40 {
		t.Errorf("content size = %vx%v, want 40x40", f.Image.CW, f.Image.CH)
	}
	if f.Image.CX-f.X != 7 || f.Image.CY-f.Y != 7 {
		t.Errorf("content offset = (%v,%v), want (7,7) from border box", f.Image.CX-f.X, f.Image.CY-f.Y)
	}
}

// --- degradation ---

// TestReplacedMissingSrcDegrades: an <img> with no src reserves its explicit size
// but paints no image, and never panics.
func TestReplacedMissingSrcDegrades(t *testing.T) {
	f := imgFragment(t, `<img width="40" height="20">`, 800, nil)
	if f.W != 40 || f.H != 20 {
		t.Errorf("size = %vx%v, want 40x20 (placeholder reserves attr size)", f.W, f.H)
	}
	if f.Image == nil {
		t.Fatalf("expected an ImageContent placeholder")
	}
	if f.Image.Img != nil {
		t.Errorf("missing-src image = %v, want nil (placeholder)", f.Image.Img)
	}
}

// TestReplacedNotFoundDegrades: a src the loader can't find degrades to a
// placeholder and logs, without panicking.
func TestReplacedNotFoundDegrades(t *testing.T) {
	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, format) }
	f := imgFragment(t, `<img src="missing.png" width="30" height="30">`, 800, resource.MapLoader{})
	if f.W != 30 || f.H != 30 {
		t.Errorf("size = %vx%v, want 30x30 (placeholder)", f.W, f.H)
	}
	if f.Image != nil && f.Image.Img != nil {
		t.Errorf("not-found image should be nil")
	}
	// Re-run with a capturing logf to assert the degradation is logged.
	_ = imgFragmentLogged(t, `<img src="missing.png" width="30" height="30">`, 800, resource.MapLoader{}, logf)
	if !anyContains(logs, "load image") {
		t.Errorf("expected a load-failure log, got %v", logs)
	}
}

// TestReplacedUndecodableDegrades: bytes that are not a supported image degrade to
// a placeholder and log, without panicking.
func TestReplacedUndecodableDegrades(t *testing.T) {
	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, format) }
	loader := resource.MapLoader{
		"bad.png": {Data: []byte("not a real png"), ContentType: "image/png"},
	}
	f := imgFragmentLogged(t, `<img src="bad.png" width="25" height="25">`, 800, loader, logf)
	if f.W != 25 || f.H != 25 {
		t.Errorf("size = %vx%v, want 25x25 (placeholder)", f.W, f.H)
	}
	if f.Image != nil && f.Image.Img != nil {
		t.Errorf("undecodable image should be nil")
	}
	if !anyContains(logs, "decode image") {
		t.Errorf("expected a decode-failure log, got %v", logs)
	}
}

// TestImageCacheTransientCancellationNotCached: a decode that misses because the
// context was cancelled is NOT cached, so a subsequent get with a live context can
// still succeed. A permanent miss (not-found) IS cached.
func TestImageCacheTransientCancellationNotCached(t *testing.T) {
	loader := resource.MapLoader{
		"img.png": {Data: pngBytes(t, 10, 10, color.RGBA{1, 2, 3, 255}), ContentType: "image/png"},
	}
	cache := newImageCache(loader, nil)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if d := cache.get(cancelled, "img.png"); d.ok {
		t.Fatalf("expected a miss under a cancelled context")
	}
	// A live context must now succeed — the cancelled miss must not have poisoned it.
	if d := cache.get(context.Background(), "img.png"); !d.ok {
		t.Errorf("live-context get after a cancelled miss = miss, want success (transient miss must not be cached)")
	}
}

// imgFragmentLogged is imgFragment with an explicit logf for degradation assertions.
func imgFragmentLogged(t *testing.T, src string, viewportW float64, loader resource.ResourceLoader, logf func(string, ...any)) *Fragment {
	t.Helper()
	root := layoutWithLoader(t, src, viewportW, loader, logf)
	f := findImageFragment(root)
	if f == nil {
		t.Fatalf("no image fragment found in layout of %q", src)
	}
	return f
}

func anyContains(logs []string, sub string) bool {
	for _, l := range logs {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// --- inline fidelity ---

// TestInlineImageHorizontalMargins: an inline <img>'s left+right margins are part of
// its inline advance, so following text is pushed right by marginL+width+marginR and
// the image's own box starts past its left margin.
func TestInlineImageHorizontalMargins(t *testing.T) {
	loader := pngLoader(t, 30, 30)
	// img: width 30, margin-left 10, margin-right 20 => advance 60.
	root := layoutWithLoader(t, `<div><img src="img.png" width="30" height="30" style="margin-left:10px;margin-right:20px">x</div>`, 800, loader, nil)
	div := bodyOf(t, root).Children[0]
	ln := firstLineWithGlyphs(t, div)
	// The first text glyph ("x") sits after the image's full advance from content-left.
	wantX := div.X + 60
	if got := ln.Glyphs[0].X; absf(got-wantX) > 1e-6 {
		t.Errorf("text glyph X after inline img = %v, want %v (mL+width+mR advance)", got, wantX)
	}
	// The image fragment's border-box left is content-left + margin-left (10).
	imgF := findImageFragment(div)
	if imgF == nil {
		t.Fatalf("no image fragment under div")
	}
	if got := imgF.X - div.X; absf(got-10) > 1e-6 {
		t.Errorf("image left offset = %v, want 10 (margin-left)", got)
	}
}

// TestInlineTallImageRaisesAscent: a line mixing text with a tall inline image gets
// its baseline placed below the image's top (the image's ascent raises the line box),
// so the image does not overflow above the line top.
func TestInlineTallImageRaisesAscent(t *testing.T) {
	loader := pngLoader(t, 100, 100)
	root := layoutWithLoader(t, `<div><img src="img.png" width="100" height="100">text</div>`, 800, loader, nil)
	div := bodyOf(t, root).Children[0]
	imgF := findImageFragment(div)
	if imgF == nil {
		t.Fatalf("no image fragment under div")
	}
	ln := firstLineWithGlyphs(t, div)
	// The image is bottom-aligned on the baseline; with a 100px image the baseline is
	// ~100 below the line top, so the image's top sits at (or below) the div content
	// top, never above it.
	if imgF.Y < div.Y-1e-6 {
		t.Errorf("image top %v is above div content top %v (overflow)", imgF.Y, div.Y)
	}
	// The baseline sits at least the image height below the content top (text rides the
	// image bottom since atoms are bottom-aligned).
	if ln.BaselineY < div.Y+100-1e-6 {
		t.Errorf("baseline %v not at least 100 below content top %v (tall atom should drop it)", ln.BaselineY, div.Y)
	}
	// The image bottom rests on the baseline.
	if absf((imgF.Y+imgF.H)-ln.BaselineY) > 1e-6 {
		t.Errorf("image bottom %v != baseline %v (bottom-aligned)", imgF.Y+imgF.H, ln.BaselineY)
	}
}

// TestReplacedRatioPreservingMaxHeight pins D2 (CSS 10.4): when the used size came from
// the intrinsic ratio, a violated max bound scales the OTHER axis to preserve the ratio.
// Intrinsic 200x100 (2:1), width:100 -> tentative 100x50; max-height:30 caps height to 30,
// so width scales to 60 (keeping 2:1), NOT staying 100 (the old per-axis clamp).
func TestReplacedRatioPreservingMaxHeight(t *testing.T) {
	f := imgFragment(t, `<img src="img.png" style="width:100px;max-height:30px">`, 800, pngLoader(t, 200, 100))
	if f.H != 30 {
		t.Errorf("height = %v, want 30 (max-height)", f.H)
	}
	// Ratio-preserving: width = 30 * 200/100 = 60.
	if f.W < 59 || f.W > 61 {
		t.Errorf("width = %v, want ~60 (ratio-preserving max-height); per-axis clamp would give 100", f.W)
	}
}

// TestReplacedRatioPreservingMinWidth pins D2 for a min bound: intrinsic 200x100,
// height:20 -> tentative 40x20; min-width:80 raises width to 80, scaling height to 40
// (keeping 2:1), NOT staying 20.
func TestReplacedRatioPreservingMinWidth(t *testing.T) {
	f := imgFragment(t, `<img src="img.png" style="height:20px;min-width:80px">`, 800, pngLoader(t, 200, 100))
	if f.W != 80 {
		t.Errorf("width = %v, want 80 (min-width)", f.W)
	}
	if f.H < 39 || f.H > 41 {
		t.Errorf("height = %v, want ~40 (ratio-preserving min-width); per-axis would give 20", f.H)
	}
}

// TestReplacedBothDimsExplicitNoRatioPreserve guards the fallback: when BOTH width and
// height are explicit (ratio already broken), min/max clamp per-axis (unchanged).
func TestReplacedBothDimsExplicitNoRatioPreserve(t *testing.T) {
	f := imgFragment(t, `<img src="img.png" width="200" height="100" style="max-width:80px">`, 800, pngLoader(t, 200, 100))
	if f.W != 80 || f.H != 100 {
		t.Errorf("size = %vx%v, want 80x100 (per-axis clamp; both dims explicit)", f.W, f.H)
	}
}
