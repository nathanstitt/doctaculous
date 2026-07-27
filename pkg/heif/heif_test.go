package heif

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	genheif "github.com/nathanstitt/doctaculous/testdata/gen/heif"
)

// singleHVC1 builds a minimal one-item file: an hvc1 primary with ispe and a
// placeholder hvcC, plus any extra properties.
func singleHVC1(w, h uint32, extra ...genheif.Prop) []byte {
	props := append([]genheif.Prop{genheif.Ispe(w, h), genheif.HvcC([]byte("hvcc-placeholder"))}, extra...)
	f := &genheif.File{
		Primary: 1,
		Items: []genheif.Item{
			{ID: 1, Type: "hvc1", Data: []byte("coded-payload"), Props: props},
		},
	}
	return f.Build()
}

func TestDecodeConfigSingle(t *testing.T) {
	cfg, err := DecodeConfig(bytes.NewReader(singleHVC1(100, 80)))
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if cfg.Width != 100 || cfg.Height != 80 {
		t.Fatalf("dims = %dx%d, want 100x80", cfg.Width, cfg.Height)
	}
	if cfg.ColorModel != color.YCbCrModel {
		t.Fatalf("color model = %v, want YCbCr", cfg.ColorModel)
	}
}

func TestDecodeConfigRotation(t *testing.T) {
	for _, tc := range []struct {
		angle        uint8
		wantW, wantH int
	}{
		{0, 100, 80}, {1, 80, 100}, {2, 100, 80}, {3, 80, 100},
	} {
		cfg, err := DecodeConfig(bytes.NewReader(singleHVC1(100, 80, genheif.Irot(tc.angle))))
		if err != nil {
			t.Fatalf("irot %d: %v", tc.angle, err)
		}
		if cfg.Width != tc.wantW || cfg.Height != tc.wantH {
			t.Errorf("irot %d: dims = %dx%d, want %dx%d", tc.angle, cfg.Width, cfg.Height, tc.wantW, tc.wantH)
		}
	}
}

func TestDecodeConfigClap(t *testing.T) {
	// clap crop applies before irot swaps axes.
	cfg, err := DecodeConfig(bytes.NewReader(singleHVC1(100, 80,
		genheif.Clap(96, 72, 0, 0), genheif.Irot(1))))
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if cfg.Width != 72 || cfg.Height != 96 {
		t.Fatalf("dims = %dx%d, want 72x96", cfg.Width, cfg.Height)
	}
}

func TestDecodeConfigAlphaAndDepth(t *testing.T) {
	alphaItem := genheif.Item{
		ID:   2,
		Type: "hvc1",
		Data: []byte("alpha-payload"),
		Props: []genheif.Prop{
			genheif.Ispe(100, 80),
			genheif.HvcC([]byte("hvcc")),
			genheif.AuxC("urn:mpeg:hevc:2015:auxid:1"),
		},
		Refs: []genheif.Ref{{Type: "auxl", To: []uint32{1}}},
	}
	build := func(primaryProps ...genheif.Prop) []byte {
		f := &genheif.File{
			Primary: 1,
			Items: []genheif.Item{
				{ID: 1, Type: "hvc1", Data: []byte("coded"), Props: append([]genheif.Prop{
					genheif.Ispe(100, 80), genheif.HvcC([]byte("hvcc")),
				}, primaryProps...)},
				alphaItem,
			},
		}
		return f.Build()
	}

	cfg, err := DecodeConfig(bytes.NewReader(build()))
	if err != nil {
		t.Fatalf("alpha: %v", err)
	}
	if cfg.ColorModel != color.NRGBAModel {
		t.Fatalf("alpha color model = %v, want NRGBA", cfg.ColorModel)
	}

	cfg, err = DecodeConfig(bytes.NewReader(build(genheif.Pixi(10, 10, 10))))
	if err != nil {
		t.Fatalf("alpha+10bit: %v", err)
	}
	if cfg.ColorModel != color.NRGBA64Model {
		t.Fatalf("alpha+10bit color model = %v, want NRGBA64", cfg.ColorModel)
	}

	cfg, err = DecodeConfig(bytes.NewReader(singleHVC1(100, 80, genheif.Pixi(10, 10, 10))))
	if err != nil {
		t.Fatalf("10bit: %v", err)
	}
	if cfg.ColorModel != color.RGBA64Model {
		t.Fatalf("10bit color model = %v, want RGBA64", cfg.ColorModel)
	}
}

// gridFile builds a 2x2 grid: primary grid item 10 with tiles 1-4.
func gridFile(mutate func(*genheif.File)) []byte {
	f := &genheif.File{
		Primary: 10,
		Items: []genheif.Item{
			{ID: 10, Type: "grid",
				Data:   genheif.GridPayload(2, 2, 120, 90),
				InIdat: true,
				Props:  []genheif.Prop{genheif.Ispe(120, 90)},
				Refs:   []genheif.Ref{{Type: "dimg", To: []uint32{1, 2, 3, 4}}},
			},
			{ID: 1, Type: "hvc1", Data: []byte("t1"), Props: []genheif.Prop{genheif.Ispe(64, 48), genheif.HvcC([]byte("h"))}},
			{ID: 2, Type: "hvc1", Data: []byte("t2"), Props: []genheif.Prop{genheif.Ispe(64, 48), genheif.HvcC([]byte("h"))}},
			{ID: 3, Type: "hvc1", Data: []byte("t3"), Props: []genheif.Prop{genheif.Ispe(64, 48), genheif.HvcC([]byte("h"))}},
			{ID: 4, Type: "hvc1", Data: []byte("t4"), Props: []genheif.Prop{genheif.Ispe(64, 48), genheif.HvcC([]byte("h"))}},
		},
	}
	if mutate != nil {
		mutate(f)
	}
	return f.Build()
}

func TestDecodeConfigGrid(t *testing.T) {
	cfg, err := DecodeConfig(bytes.NewReader(gridFile(nil)))
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if cfg.Width != 120 || cfg.Height != 90 {
		t.Fatalf("dims = %dx%d, want 120x90", cfg.Width, cfg.Height)
	}
}

func TestDecodeConfigGridWithoutIspe(t *testing.T) {
	// Dimensions fall back to the ImageGrid body (stored in idat here,
	// which also exercises construction method 1).
	data := gridFile(func(f *genheif.File) { f.Items[0].Props = nil })
	cfg, err := DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if cfg.Width != 120 || cfg.Height != 90 {
		t.Fatalf("dims = %dx%d, want 120x90", cfg.Width, cfg.Height)
	}
}

func TestDecodeGridValidation(t *testing.T) {
	// A structurally sound grid reaches the not-yet-implemented codec gate.
	_, err := Decode(bytes.NewReader(gridFile(nil)))
	if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "not yet implemented") {
		t.Fatalf("valid grid: %v", err)
	}
	// Tile-count mismatch is caught as a container error.
	bad := gridFile(func(f *genheif.File) {
		f.Items[0].Refs = []genheif.Ref{{Type: "dimg", To: []uint32{1, 2, 3}}}
	})
	if _, err := Decode(bytes.NewReader(bad)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("3-tile 2x2 grid: want ErrInvalid, got %v", err)
	}
	// A missing tile item is likewise invalid.
	bad = gridFile(func(f *genheif.File) {
		f.Items[0].Refs = []genheif.Ref{{Type: "dimg", To: []uint32{1, 2, 3, 99}}}
	})
	if _, err := Decode(bytes.NewReader(bad)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing tile: want ErrInvalid, got %v", err)
	}
}

func TestMultiExtentPayload(t *testing.T) {
	// The grid body split across two mdat iloc extents must reassemble.
	// Removing the grid's ispe forces DecodeConfig through the payload
	// path, proving the concatenation.
	data := gridFile(func(f *genheif.File) {
		body := f.Items[0].Data
		f.Items[0].Data = nil
		f.Items[0].Parts = [][]byte{body[:3], body[3:]}
		f.Items[0].InIdat = false
		f.Items[0].Props = nil
	})
	cfg, err := DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if cfg.Width != 120 || cfg.Height != 90 {
		t.Fatalf("dims = %dx%d, want 120x90", cfg.Width, cfg.Height)
	}
}

func TestUnsupportedInputs(t *testing.T) {
	cases := map[string][]byte{
		"msf1 sequence": (&genheif.File{
			Major: "msf1", Compat: []string{"msf1"}, Primary: 1,
			Items: []genheif.Item{{ID: 1, Type: "hvc1", Data: []byte("x")}},
		}).Build(),
		"foreign brand": (&genheif.File{
			Major: "avif", Compat: []string{"avif"}, Primary: 1,
			Items: []genheif.Item{{ID: 1, Type: "av01", Data: []byte("x")}},
		}).Build(),
		"av01 item in mif1": (&genheif.File{
			Major: "mif1", Compat: []string{"mif1", "avif"}, Primary: 1,
			Items: []genheif.Item{{ID: 1, Type: "av01", Data: []byte("x"),
				Props: []genheif.Prop{genheif.Ispe(10, 10)}}},
		}).Build(),
		"protected item": (&genheif.File{
			Primary: 1,
			Items: []genheif.Item{{ID: 1, Type: "hvc1", Data: []byte("x"), Protected: true,
				Props: []genheif.Prop{genheif.Ispe(10, 10)}}},
		}).Build(),
		"unknown essential property": (&genheif.File{
			Primary: 1,
			Items: []genheif.Item{{ID: 1, Type: "hvc1", Data: []byte("x"),
				Props: []genheif.Prop{genheif.Ispe(10, 10),
					{Type: "zzzz", Payload: []byte{1}, Essential: true}}}},
		}).Build(),
		"oversized dimension":  singleHVC1(40000, 40000),
		"pixel count over cap": singleHVC1(20000, 20000),
	}
	for name, data := range cases {
		if _, err := DecodeConfig(bytes.NewReader(data)); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: want ErrUnsupported, got %v", name, err)
		}
	}

	// A non-essential unknown property must NOT block decoding.
	ok := singleHVC1(10, 10, genheif.Prop{Type: "zzzz", Payload: []byte{1}})
	if _, err := DecodeConfig(bytes.NewReader(ok)); err != nil {
		t.Errorf("non-essential unknown property: %v", err)
	}
}

func TestInvalidInputs(t *testing.T) {
	valid := singleHVC1(100, 80)
	cases := map[string][]byte{
		"empty":           {},
		"garbage":         []byte("this is not a heif file at all........."),
		"truncated file":  valid[:len(valid)-20],
		"header only":     valid[:30],
		"zero dimensions": singleHVC1(0, 80),
	}
	for name, data := range cases {
		_, err := DecodeConfig(bytes.NewReader(data))
		if !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: want typed error, got %v", name, err)
		}
	}
}

func TestDecodeNotYetImplemented(t *testing.T) {
	_, err := Decode(bytes.NewReader(singleHVC1(100, 80)))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}

func TestRealSipsFile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "sips-quad-64x48.heic"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	cfg, err := DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if cfg.Width != 64 || cfg.Height != 48 {
		t.Fatalf("dims = %dx%d, want 64x48", cfg.Width, cfg.Height)
	}

	// Registration: the standard library must sniff it as "heif".
	cfg2, name, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("image.DecodeConfig: %v", err)
	}
	if name != "heif" {
		t.Fatalf("format name = %q, want heif", name)
	}
	if cfg2 != cfg {
		t.Fatalf("image.DecodeConfig = %+v, DecodeConfig = %+v", cfg2, cfg)
	}
}

func FuzzParseContainer(f *testing.F) {
	f.Add(singleHVC1(100, 80))
	f.Add(gridFile(nil))
	if data, err := os.ReadFile(filepath.Join("testdata", "sips-quad-64x48.heic")); err == nil {
		f.Add(data)
	}
	f.Add([]byte("????ftypheic"))
	f.Fuzz(func(t *testing.T, data []byte) {
		// Must never panic or over-allocate; errors are fine.
		c, err := parseContainer(data)
		if err != nil {
			return
		}
		primary, err := c.primary()
		if err != nil {
			return
		}
		if err := checkDecodable(primary); err != nil {
			return
		}
		_, _, _ = c.configDims(primary)
		for _, it := range c.items {
			_, _ = c.payload(it)
		}
	})
}
