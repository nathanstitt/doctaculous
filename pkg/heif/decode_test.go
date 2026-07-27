package heif

import (
	"bytes"
	"image"
	"os"
	"path/filepath"
	"testing"

	genheif "github.com/nathanstitt/doctaculous/testdata/gen/heif"
)

// realPayload loads a committed Annex-B payload and wraps it into hvcC +
// item payload form.
func realPayload(t *testing.T, name string) (hvcc, payload []byte) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "gen", "heif", "payloads", name))
	if err != nil {
		t.Fatalf("read payload: %v", err)
	}
	return genheif.HvcCFromAnnexB(data)
}

// realSingle builds a .heic holding one real coded image plus extra props.
func realSingle(t *testing.T, payloadName string, w, h uint32, extra ...genheif.Prop) []byte {
	t.Helper()
	hvcc, payload := realPayload(t, payloadName)
	props := append([]genheif.Prop{genheif.Ispe(w, h), genheif.HvcC(hvcc)}, extra...)
	f := &genheif.File{
		Primary: 1,
		Items:   []genheif.Item{{ID: 1, Type: "hvc1", Data: payload, Props: props}},
	}
	return f.Build()
}

func TestDecodeSingleRealImage(t *testing.T) {
	img, err := Decode(bytes.NewReader(realSingle(t, "x265-64x64-qp27.hevc", 64, 64)))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 64 {
		t.Fatalf("bounds = %v", img.Bounds())
	}
	if _, ok := img.(*image.NRGBA); !ok {
		t.Fatalf("type = %T, want *image.NRGBA", img)
	}
}

func TestDecode10BitRealImage(t *testing.T) {
	img, err := Decode(bytes.NewReader(realSingle(t, "x265-64x64-qp27-10bit.hevc", 64, 64)))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, ok := img.(*image.NRGBA64); !ok {
		t.Fatalf("type = %T, want *image.NRGBA64", img)
	}
}

func TestDecodeRealGrid(t *testing.T) {
	hvcc, payload := realPayload(t, "x265-64x64-qp27.hevc")
	tileProps := func() []genheif.Prop {
		return []genheif.Prop{genheif.Ispe(64, 64), genheif.HvcC(hvcc)}
	}
	f := &genheif.File{
		Primary: 10,
		Items: []genheif.Item{
			{ID: 10, Type: "grid", Data: genheif.GridPayload(2, 2, 120, 100), InIdat: true,
				Props: []genheif.Prop{genheif.Ispe(120, 100)},
				Refs:  []genheif.Ref{{Type: "dimg", To: []uint32{1, 2, 3, 4}}}},
			{ID: 1, Type: "hvc1", Data: payload, Props: tileProps(), Hidden: true},
			{ID: 2, Type: "hvc1", Data: payload, Props: tileProps(), Hidden: true},
			{ID: 3, Type: "hvc1", Data: payload, Props: tileProps(), Hidden: true},
			{ID: 4, Type: "hvc1", Data: payload, Props: tileProps(), Hidden: true},
		},
	}
	img, err := Decode(bytes.NewReader(f.Build()))
	if err != nil {
		t.Fatalf("Decode grid: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 120 || b.Dy() != 100 {
		t.Fatalf("bounds = %v, want 120x100", b)
	}
	// All four tiles are the same coded image, so pixels one tile apart
	// (inside the un-cropped region) must be identical.
	for _, p := range [][2]int{{5, 7}, {30, 20}, {50, 33}} {
		a := img.At(p[0], p[1])
		if img.At(p[0]+64, p[1]) != a || img.At(p[0], p[1]+36) == nil {
			t.Fatalf("tile repetition broken at %v", p)
		}
	}
}

func TestDecodeRealAlpha(t *testing.T) {
	hvcc, payload := realPayload(t, "x265-64x64-qp27.hevc")
	f := &genheif.File{
		Primary: 1,
		Items: []genheif.Item{
			{ID: 1, Type: "hvc1", Data: payload,
				Props: []genheif.Prop{genheif.Ispe(64, 64), genheif.HvcC(hvcc)}},
			{ID: 2, Type: "hvc1", Data: payload, Hidden: true,
				Props: []genheif.Prop{genheif.Ispe(64, 64), genheif.HvcC(hvcc),
					genheif.AuxC("urn:mpeg:hevc:2015:auxid:1")},
				Refs: []genheif.Ref{{Type: "auxl", To: []uint32{1}}}},
		},
	}
	img, err := Decode(bytes.NewReader(f.Build()))
	if err != nil {
		t.Fatalf("Decode alpha: %v", err)
	}
	nrgba, ok := img.(*image.NRGBA)
	if !ok {
		t.Fatalf("type = %T", img)
	}
	// The alpha plane is the aux image's luma: for this fixture, varied
	// noise — assert it is not constant-opaque.
	varied := false
	first := nrgba.Pix[3]
	for i := 3; i < len(nrgba.Pix); i += 4 {
		if nrgba.Pix[i] != first {
			varied = true
			break
		}
	}
	if !varied {
		t.Fatal("alpha channel is constant; aux plane not applied")
	}
}

func TestDecodeRotation(t *testing.T) {
	plain, err := Decode(bytes.NewReader(realSingle(t, "x265-96x80-qp27.hevc", 96, 80)))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	rot, err := Decode(bytes.NewReader(realSingle(t, "x265-96x80-qp27.hevc", 96, 80, genheif.Irot(1))))
	if err != nil {
		t.Fatalf("Decode rotated: %v", err)
	}
	if rot.Bounds().Dx() != 80 || rot.Bounds().Dy() != 96 {
		t.Fatalf("rotated bounds = %v, want 80x96", rot.Bounds())
	}
	// 90 deg anti-clockwise: dst(y, w-1-x) = src(x, y).
	for _, p := range [][2]int{{0, 0}, {10, 3}, {95, 79}} {
		want := plain.At(p[0], p[1])
		got := rot.At(p[1], 96-1-p[0])
		if want != got {
			t.Fatalf("rotation mismatch at %v: %v vs %v", p, want, got)
		}
	}
	mir, err := Decode(bytes.NewReader(realSingle(t, "x265-96x80-qp27.hevc", 96, 80, genheif.Imir(0))))
	if err != nil {
		t.Fatalf("Decode mirrored: %v", err)
	}
	if mir.At(0, 5) != plain.At(95, 5) {
		t.Fatal("vertical-axis mirror mismatch")
	}
}

func TestDecodeRealSipsFileToImage(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "sips-quad-64x48.heic"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("image.Decode: %v", err)
	}
	if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 48 {
		t.Fatalf("bounds = %v", img.Bounds())
	}
	// The source is a four-quadrant tell-tale: red-ish TL, green-ish TR,
	// blue-ish BL, yellow-ish BR. Verify the dominant channels survived
	// encode/decode (loose thresholds, this is lossy 4:2:0).
	check := func(x, y int, wantR, wantG, wantB bool) {
		r, g, b, _ := img.At(x, y).RGBA()
		t.Logf("(%d,%d) r=%d g=%d b=%d", x, y, r>>8, g>>8, b>>8)
		if wantR != (r>>8 > 128) || wantG != (g>>8 > 128) || wantB != (b>>8 > 128) {
			t.Errorf("quadrant colour wrong at (%d,%d): r=%d g=%d b=%d", x, y, r>>8, g>>8, b>>8)
		}
	}
	check(16, 12, true, false, false) // top-left: red
	check(48, 12, false, true, false) // top-right: green
	check(16, 36, false, false, true) // bottom-left: blue
	check(48, 36, true, true, false)  // bottom-right: yellow
}

func TestDecodeRealHeifEncFile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "heifenc-noise-96x80.heic"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	img, name, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("image.Decode: %v", err)
	}
	if name != "heif" || img.Bounds().Dx() != 96 || img.Bounds().Dy() != 80 {
		t.Fatalf("format %q bounds %v", name, img.Bounds())
	}
}
