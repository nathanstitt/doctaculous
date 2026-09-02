//go:build ignore

// Command gen_animated builds pkg/internal/webp/testdata/animated.webp, the animated
// fixture the WebP reader's degradation tests need. Run by hand from the repo
// root when the fixture must change; the result is committed so tests stay
// offline:
//
//	go run testdata/gen/webp/gen_animated.go
//
// It exists because golang.org/x/image ships no animated WebP and Go has no
// WebP encoder to make one with. Rather than hand-rolling a bitstream, it
// wraps the real VP8L payload already committed in still-lossless.webp in an
// animation container, so each frame is genuinely decodable and the file is
// rejected for being animated — not for being malformed.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
)

const (
	srcPath = "pkg/internal/webp/testdata/still-lossless.webp"
	outPath = "pkg/internal/webp/testdata/animated.webp"

	// The canvas of still-lossless.webp (tux.lossless.webp upstream). The frames
	// reuse its bitstream, so the animation canvas must match it exactly.
	canvasW = 386
	canvasH = 395

	frameDurationMS = 100
	animationBit    = 1 << 1
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen_animated:", err)
		os.Exit(1)
	}
}

func run() error {
	src, err := os.ReadFile(filepath.FromSlash(srcPath))
	if err != nil {
		return fmt.Errorf("read source still: %w", err)
	}
	vp8l, err := findChunk(src, "VP8L")
	if err != nil {
		return err
	}

	var vp8x bytes.Buffer
	vp8x.WriteByte(animationBit)
	vp8x.Write([]byte{0, 0, 0}) // reserved
	vp8x.Write(u24(canvasW - 1))
	vp8x.Write(u24(canvasH - 1))

	var anim bytes.Buffer
	binary.Write(&anim, binary.LittleEndian, uint32(0xFFFFFFFF)) // background colour
	binary.Write(&anim, binary.LittleEndian, uint16(0))          // loop forever

	var body bytes.Buffer
	body.WriteString("WEBP")
	body.Write(chunk("VP8X", vp8x.Bytes()))
	body.Write(chunk("ANIM", anim.Bytes()))
	// Two frames: one would still be an animation, but two makes the intent
	// unambiguous to anyone opening the file.
	body.Write(chunk("ANMF", frame(vp8l)))
	body.Write(chunk("ANMF", frame(vp8l)))

	var out bytes.Buffer
	out.WriteString("RIFF")
	binary.Write(&out, binary.LittleEndian, uint32(body.Len()))
	out.Write(body.Bytes())

	if err := os.WriteFile(filepath.FromSlash(outPath), out.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write fixture: %w", err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", outPath, out.Len())
	return nil
}

// findChunk returns the complete RIFF chunk (8-byte header plus payload) with
// the given id, so it can be re-embedded verbatim.
func findChunk(data []byte, id string) ([]byte, error) {
	i := bytes.Index(data, []byte(id))
	if i < 0 || i+8 > len(data) {
		return nil, fmt.Errorf("no %s chunk in %s", id, srcPath)
	}
	n := binary.LittleEndian.Uint32(data[i+4 : i+8])
	end := i + 8 + int(n)
	if end > len(data) {
		return nil, fmt.Errorf("%s chunk in %s runs past EOF", id, srcPath)
	}
	return data[i:end], nil
}

// frame wraps a still bitstream in the ANMF payload header: position, size,
// duration, and blend/dispose flags.
func frame(bitstream []byte) []byte {
	var f bytes.Buffer
	f.Write(u24(0)) // frame x, in 2px units
	f.Write(u24(0)) // frame y
	f.Write(u24(canvasW - 1))
	f.Write(u24(canvasH - 1))
	f.Write(u24(frameDurationMS))
	f.WriteByte(0) // blend + dispose
	f.Write(bitstream)
	return f.Bytes()
}

// chunk frames a payload as a RIFF chunk, padding to even length as the spec
// requires.
func chunk(id string, payload []byte) []byte {
	var b bytes.Buffer
	b.WriteString(id)
	binary.Write(&b, binary.LittleEndian, uint32(len(payload)))
	b.Write(payload)
	if len(payload)%2 == 1 {
		b.WriteByte(0)
	}
	return b.Bytes()
}

// u24 writes a 24-bit little-endian value, the width WebP uses for canvas and
// frame dimensions and for frame durations.
func u24(v uint32) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16)} }
