package hevc

import (
	"compress/gzip"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readReferenceYUV loads a committed <name>.yuv.gz reference dump (ffmpeg's
// decode of the same payload, planar 4:2:0 at the cropped size; 10-bit is
// little-endian uint16).
func readReferenceYUV(t *testing.T, name string, w, h, bitDepth int) (y, cb, cr []uint16) {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "..", "testdata", "gen", "heif", "payloads", name))
	if err != nil {
		t.Fatalf("open reference: %v", err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	var raw []byte
	buf := make([]byte, 1<<16)
	for {
		n, err := zr.Read(buf)
		raw = append(raw, buf[:n]...)
		if err != nil {
			break
		}
	}
	cw, ch := w/2, h/2
	if w%2 == 1 {
		cw = (w + 1) / 2
	}
	if h%2 == 1 {
		ch = (h + 1) / 2
	}
	total := w*h + 2*cw*ch
	toPlane := func(samples int, off int) ([]uint16, int) {
		out := make([]uint16, samples)
		if bitDepth > 8 {
			for i := range out {
				out[i] = binary.LittleEndian.Uint16(raw[off+2*i:])
			}
			return out, off + 2*samples
		}
		for i := range out {
			out[i] = uint16(raw[off+i])
		}
		return out, off + samples
	}
	bytesPer := 1
	if bitDepth > 8 {
		bytesPer = 2
	}
	if len(raw) != total*bytesPer {
		t.Fatalf("%s: reference size %d, want %d", name, len(raw), total*bytesPer)
	}
	off := 0
	y, off = toPlane(w*h, off)
	cb, off = toPlane(cw*ch, off)
	cr, _ = toPlane(cw*ch, off)
	return y, cb, cr
}

// decodeFixture runs the full DecodeFrame path on a committed payload.
func decodeFixture(t *testing.T, name string) *Frame {
	t.Helper()
	params, slices := loadStream(t, name)
	nals := make([][]byte, 0, len(slices))
	for _, s := range slices {
		// Reassemble raw NAL (header + payload) as DecodeFrame expects.
		nal := make([]byte, 0, len(s.payload)+2)
		h := uint16(s.typ)<<9 | uint16(s.layerID)<<3 | uint16(s.temporalID+1)
		nal = append(nal, byte(h>>8), byte(h))
		nal = append(nal, s.payload...)
		nals = append(nals, nal)
	}
	frame, err := DecodeFrame(params, nals)
	if err != nil {
		t.Fatalf("%s: DecodeFrame: %v", name, err)
	}
	return frame
}

// comparePlane requires exact sample equality and reports the first few
// mismatches with coordinates (the debugging entry point for divergence).
func comparePlane(t *testing.T, name, plane string, got []uint16, stride int, want []uint16, w, h int) int {
	t.Helper()
	bad := 0
	for y := range h {
		for x := range w {
			g := got[y*stride+x]
			r := want[y*w+x]
			if g != r {
				if bad < 8 {
					t.Errorf("%s %s (%d,%d): got %d, reference %d", name, plane, x, y, g, r)
				}
				bad++
			}
		}
	}
	if bad > 0 {
		t.Errorf("%s %s: %d/%d samples differ", name, plane, bad, w*h)
	}
	return bad
}

// bitExactCases are every committed pre-filter (no SAO/deblock) payload;
// the decoder must reproduce the reference decode exactly.
var bitExactCases = []struct {
	name          string
	width, height int
	bitDepth      int
}{
	{"x265-16x16-qp12-nofilt", 16, 16, 8},
	{"x265-16x16-qp27-nofilt", 16, 16, 8},
	{"x265-16x16-qp40-nofilt", 16, 16, 8},
	{"x265-16x16-qp27-10bit-nofilt", 16, 16, 10},
	{"x265-30x22-qp27-nofilt", 30, 22, 8},
	{"x265-64x64-qp12-nofilt", 64, 64, 8},
	{"x265-64x64-qp27-nofilt", 64, 64, 8},
	{"x265-64x64-qp40-nofilt", 64, 64, 8},
	{"x265-64x64-qp27-10bit-nofilt", 64, 64, 10},
	{"x265-64x64-qp27-tskip-nofilt", 64, 64, 8},
	{"x265-64x64-qp27-lossless-nofilt", 64, 64, 8},
	{"x265-64x64-qp27-scaling-nofilt", 64, 64, 8},
	{"x265-64x64-qp27-nosdh-nofilt", 64, 64, 8},
	{"x265-64x64-qp27-ctu16-nofilt", 64, 64, 8},
	{"x265-96x80-qp12-nofilt", 96, 80, 8},
	{"x265-96x80-qp27-nofilt", 96, 80, 8},
	{"x265-96x80-qp40-nofilt", 96, 80, 8},
	{"x265-96x80-qp27-10bit-nofilt", 96, 80, 10},
	{"x265-96x80-qp27-ctu16-nofilt", 96, 80, 8},
	{"x265-512x512-qp27-nofilt", 512, 512, 8},
}

func TestDecodeBitExact(t *testing.T) {
	for _, tc := range bitExactCases {
		t.Run(tc.name, func(t *testing.T) {
			frame := decodeFixture(t, tc.name+".hevc")
			if frame.Width != tc.width || frame.Height != tc.height {
				t.Fatalf("decoded %dx%d, want %dx%d", frame.Width, frame.Height, tc.width, tc.height)
			}
			if frame.BitDepth != tc.bitDepth {
				t.Fatalf("bit depth %d, want %d", frame.BitDepth, tc.bitDepth)
			}
			refY, refCb, refCr := readReferenceYUV(t, tc.name+".yuv.gz", tc.width, tc.height, tc.bitDepth)
			cw, ch := (tc.width+1)/2, (tc.height+1)/2
			bad := comparePlane(t, tc.name, "Y", frame.Y, frame.YStride, refY, tc.width, tc.height)
			bad += comparePlane(t, tc.name, "Cb", frame.Cb, frame.CStride, refCb, cw, ch)
			bad += comparePlane(t, tc.name, "Cr", frame.Cr, frame.CStride, refCr, cw, ch)
			if bad > 0 {
				t.FailNow()
			}
		})
	}
}

func FuzzDecodeFrame(f *testing.F) {
	for _, name := range []string{"x265-16x16-qp27-nofilt.hevc", "x265-30x22-qp27-nofilt.hevc", "x265-64x64-qp27-ctu16-nofilt.hevc"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "gen", "heif", "payloads", name))
		if err != nil {
			continue
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Whatever the mutation, DecodeFrame must return, never panic or
		// over-allocate.
		var params ParamSets
		var slices [][]byte
		for _, raw := range splitAnnexB(data) {
			n, err := parseNAL(raw)
			if err != nil {
				continue
			}
			switch n.typ {
			case nalVPS:
				params.VPS = append(params.VPS, raw)
			case nalSPS:
				params.SPS = append(params.SPS, raw)
			case nalPPS:
				params.PPS = append(params.PPS, raw)
			default:
				if n.isSlice() {
					slices = append(slices, raw)
				}
			}
		}
		_, _ = DecodeFrame(params, slices)
	})
}

func TestDecodeRejectsFilteredStreamsForNow(t *testing.T) {
	// Default-toolset payloads carry SAO, which lands with the loop-filter
	// milestone; until then they must fail with the typed error, not
	// misdecode.
	params, slices := loadStream(t, "x265-64x64-qp27.hevc")
	nals := make([][]byte, 0, len(slices))
	for _, s := range slices {
		nal := []byte{byte(uint16(s.typ) << 1), byte(s.temporalID + 1)}
		nal = append(nal, s.payload...)
		nals = append(nals, nal)
	}
	_, err := DecodeFrame(params, nals)
	if err == nil || !strings.Contains(err.Error(), "SAO") {
		t.Fatalf("expected SAO unsupported error, got %v", err)
	}
}
