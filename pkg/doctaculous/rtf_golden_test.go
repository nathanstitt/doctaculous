package doctaculous

import (
	"bytes"
	"context"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rtfSpecimen exercises the RTF vocabulary end to end: fonts, colors,
// emphasis, alignment, indents, a hyperlink field, a table, cp1252/\u
// escapes, and an embedded picture (a generated solid-blue PNG, hex-encoded
// the way RTF embeds pictures).
func rtfSpecimen() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 24, 12))
	for y := range 12 {
		for x := range 24 {
			img.Set(x, y, color.RGBA{R: 0x33, G: 0x88, B: 0xCC, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	pictHex := hex.EncodeToString(buf.Bytes())
	return []byte(`{\rtf1\ansi\deff0
{\fonttbl{\f0\froman Times New Roman;}{\f1\fmodern Courier New;}}
{\colortbl ;\red200\green30\blue30;\red250\green240\blue160;}
{\b\fs32 RTF Specimen}\par
Body text with {\b bold}, {\i italic}, {\ul underline}, {\strike struck}, and {\cf1 colored} runs.\par
{\f1\fs18 Monospace at nine points} and {\highlight2 highlighted words} too.\par
\qc A centered line\par
\pard\li720\fi-360\bullet  A hanging-indent bullet line\par
\pard Escapes: caf\'e9 \'93smart quotes\'94 \u26085?\u26412? \endash\emdash\par
Visit {\field{\*\fldinst HYPERLINK "https://example.com/"}{\fldrslt the example site}} for more.\par
\trowd\cellx2600\cellx5200
\intbl {\b Cell one}\cell Second cell\cell\row
\trowd\cellx2600\cellx5200
\intbl Left\cell Right\cell\row
\pard {\pict\pngblip\picwgoal1500\pichgoal750 ` + pictHex + `}\par
}`)
}

// TestRTFGolden renders the specimen end to end — the RTF visual entry. Run
// with -update, then eyeball.
func TestRTFGolden(t *testing.T) {
	doc, err := OpenRTFBytes(rtfSpecimen(), WithViewportWidth(460), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenRTFBytes: %v", err)
	}
	if doc.Format() != FormatRTF {
		t.Errorf("Format() = %q, want rtf", doc.Format())
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: goldenDPI, BundledFonts: true})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	got, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("rasterized image is %T, want *image.RGBA", img)
	}

	dir := filepath.Join("testdata", "golden")
	path := filepath.Join(dir, "rtf-specimen.png")
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writePNG(t, path, got)
		t.Logf("updated %s", path)
		return
	}
	want := readPNG(t, path)
	if want == nil {
		t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run TestRTFGolden -update", path)
	}
	if diff, n := compareImages(want, got); diff {
		t.Errorf("render differs from golden %s: %d pixels beyond tolerance", path, n)
	}
}

// TestRTFDetectionAndConvert pins the unified-conversion wiring: content
// detection by the {\rtf magic, format stamping, and a structure conversion.
func TestRTFDetectionAndConvert(t *testing.T) {
	doc, err := OpenBytes(rtfSpecimen())
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if doc.Format() != FormatRTF {
		t.Errorf("detected format = %q, want rtf", doc.Format())
	}

	// rtf → markdown through the generic path: structure carries.
	var md bytes.Buffer
	if err := Convert(context.Background(), bytes.NewReader(rtfSpecimen()), &md, ConvertOptions{To: FormatMarkdown}); err != nil {
		t.Fatalf("Convert rtf→md: %v", err)
	}
	got := md.String()
	for _, want := range []string{
		"**bold**",
		"[the example site](https://example.com/)",
		"| Left | Right |",
		"café",
		"日本",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, got)
		}
	}

	// RTF is a conversion output too (pkg/render/rtfwrite).
	var out bytes.Buffer
	err = Convert(context.Background(), strings.NewReader("<p>hi</p>"), &out, ConvertOptions{From: FormatHTML, To: FormatRTF})
	if err != nil {
		t.Errorf("html→rtf: %v", err)
	}
	if !strings.HasPrefix(out.String(), `{\rtf1`) {
		t.Errorf("html→rtf output lacks an RTF signature:\n%.80s", out.String())
	}
}
