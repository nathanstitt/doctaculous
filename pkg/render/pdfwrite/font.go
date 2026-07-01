package pdfwrite

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/nathanstitt/doctaculous/pkg/font"
)

// faceUse records the glyphs drawn from one face during device painting, plus the
// per-glyph emit code the content stream references.
//
// The emit code differs by embedding model:
//   - TrueType (Type0/Identity-H): code == GID, a 2-byte value with no cap.
//   - Type1 (simple font): code is a 1-byte character code assigned in first-use
//     order (0..255); a face that needs more than 256 distinct glyphs overflows
//     (further glyphs report not-embeddable and the device paints their outlines).
type faceUse struct {
	runes map[uint16][]rune // gid -> source runes (for /ToUnicode)
	code  map[uint16]uint16 // gid -> emit code
	order []uint16          // gids in first-use order (deterministic /Differences, /W)
	next  int               // next Type1 code to assign
}

// fontEmbedder collects the glyphs used per face during device painting and emits
// each face as either a Type0/Identity-H CID font (TrueType, /FontFile2) or a
// simple Type1 font (/FontFile), each with a ToUnicode CMap so text stays
// searchable. Whole programs are embedded; the TrueType path additionally subsets
// glyf (see subsetTrueType).
type fontEmbedder struct {
	uses  map[*font.Face]*faceUse
	res   map[*font.Face]string // face -> resource name (/F0, /F1, ...)
	order []*font.Face          // faces in first-use order (deterministic id/name)
}

func newFontEmbedder() *fontEmbedder {
	return &fontEmbedder{uses: map[*font.Face]*faceUse{}, res: map[*font.Face]string{}}
}

// use records that gid (from face) was drawn for the given source runes and returns
// the content-stream emit code for it. embedded is false when the glyph cannot be
// embedded as text (a non-embeddable program, or a Type1 face already at its
// 256-glyph cap); the caller then paints the glyph's outline instead.
func (fe *fontEmbedder) use(face *font.Face, gid uint16, runes []rune) (code uint16, embedded bool) {
	if face == nil {
		return 0, false
	}
	_, kind := face.ProgramBytes()
	if kind == font.ProgramKindUnknown {
		return 0, false
	}
	fu := fe.uses[face]
	if fu == nil {
		fu = &faceUse{runes: map[uint16][]rune{}, code: map[uint16]uint16{}}
		fe.uses[face] = fu
		fe.order = append(fe.order, face)
	}
	if c, seen := fu.code[gid]; seen {
		return c, true
	}
	switch kind {
	case font.ProgramKindTrueType, font.ProgramKindCFF:
		// Identity-H: the code is the GID itself.
		fu.code[gid] = gid
		fu.order = append(fu.order, gid)
		fu.runes[gid] = append([]rune(nil), runes...)
		return gid, true
	case font.ProgramKindType1:
		if fu.next >= 256 {
			return 0, false // simple-font code space exhausted; fall back to outlines
		}
		c := uint16(fu.next)
		fu.next++
		fu.code[gid] = c
		fu.order = append(fu.order, gid)
		fu.runes[gid] = append([]rune(nil), runes...)
		return c, true
	default:
		return 0, false
	}
}

// resourceName returns the /Font resource name for face, assigning one on first use
// in first-use order (so names are deterministic).
//
//nolint:unused // consumed by the page device (device.go) and assembler (page.go).
func (fe *fontEmbedder) resourceName(face *font.Face) string {
	if n, ok := fe.res[face]; ok {
		return n
	}
	n := fmt.Sprintf("F%d", len(fe.res))
	fe.res[face] = n
	return n
}

// orderedFaces returns the faces in first-use order, for deterministic emission.
//
//nolint:unused // consumed by the document assembler (page.go).
func (fe *fontEmbedder) orderedFaces() []*font.Face { return fe.order }

// emit writes face's font tree to w and returns the top /Font reference, choosing
// the model from the face's program kind. It returns 0 if face has no embeddable
// program (the caller draws outlines instead).
func (fe *fontEmbedder) emit(w *writer, face *font.Face) Ref {
	fu := fe.uses[face]
	if fu == nil || len(fu.order) == 0 {
		return 0
	}
	_, kind := face.ProgramBytes()
	switch kind {
	case font.ProgramKindTrueType, font.ProgramKindCFF:
		return fe.emitType0(w, face, fu)
	case font.ProgramKindType1:
		return fe.emitType1(w, face, fu)
	default:
		return 0
	}
}

// emitType0 embeds face as a Type0/Identity-H CIDFontType2 with a subsetted (or
// whole) TrueType program and a ToUnicode CMap.
func (fe *fontEmbedder) emitType0(w *writer, face *font.Face, fu *faceUse) Ref {
	data, _ := face.ProgramBytes()
	gids := append([]uint16(nil), fu.order...)
	sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })

	progBytes := data
	if sub, err := subsetTrueType(data, gids); err == nil {
		progBytes = sub
	} // else: embed the whole program (graceful)
	fontFile := w.addStream(Dict{"Length1": Int(int64(len(progBytes)))}, progBytes)

	descriptor := w.alloc()
	w.put(descriptor, Dict{
		"Type":        Name("FontDescriptor"),
		"FontName":    Name("DTACUL+Embedded"),
		"Flags":       Int(4), // Symbolic; conservative and always valid
		"FontBBox":    Array{Int(-1000), Int(-1000), Int(2000), Int(2000)},
		"ItalicAngle": Int(0),
		"Ascent":      Int(800),
		"Descent":     Int(-200),
		"CapHeight":   Int(700),
		"StemV":       Int(80),
		"FontFile2":   fontFile,
	})

	cidFont := w.alloc()
	w.put(cidFont, Dict{
		"Type":           Name("Font"),
		"Subtype":        Name("CIDFontType2"),
		"BaseFont":       Name("DTACUL+Embedded"),
		"CIDSystemInfo":  Dict{"Registry": String("Adobe"), "Ordering": String("Identity"), "Supplement": Int(0)},
		"FontDescriptor": descriptor,
		"CIDToGIDMap":    Name("Identity"),
		"W":              cidWidths(gids, face),
	})

	toUni := w.addStream(Dict{}, toUnicodeCMapHex(fu, true))

	font0 := w.alloc()
	w.put(font0, Dict{
		"Type":            Name("Font"),
		"Subtype":         Name("Type0"),
		"BaseFont":        Name("DTACUL+Embedded"),
		"Encoding":        Name("Identity-H"),
		"DescendantFonts": Array{font0Descendant(cidFont)},
		"ToUnicode":       toUni,
	})
	return font0
}

// font0Descendant wraps the descendant CIDFont ref; kept as a helper so the intent
// (a one-element /DescendantFonts array) is explicit.
func font0Descendant(cidFont Ref) object { return cidFont }

// emitType1 embeds face as a simple /Type1 font: the raw Type1 program in
// /FontFile (Length1/2/3), an /Encoding /Differences mapping each assigned code to
// its glyph name, a /Widths array, and a /ToUnicode CMap.
func (fe *fontEmbedder) emitType1(w *writer, face *font.Face, fu *faceUse) Ref {
	data, _ := face.ProgramBytes()
	prog, l1, l2, l3, err := pfbToType1(data)
	if err != nil {
		return 0 // undecodable PFB: caller falls back to outlines
	}
	fontFile := w.addStream(Dict{
		"Length1": Int(int64(l1)),
		"Length2": Int(int64(l2)),
		"Length3": Int(int64(l3)),
	}, prog)

	// Order glyphs by assigned code so /Differences and /Widths are contiguous.
	byCode := append([]uint16(nil), fu.order...)
	sort.Slice(byCode, func(i, j int) bool { return fu.code[byCode[i]] < fu.code[byCode[j]] })
	firstChar := int(fu.code[byCode[0]])
	lastChar := int(fu.code[byCode[len(byCode)-1]])

	// /Differences: [firstCode /nameA /nameB ...] with explicit runs when codes skip.
	var diffs Array
	prev := -2
	widths := make([]object, 0, lastChar-firstChar+1)
	// Build a code->gid map for width/name lookup.
	codeToGID := map[int]uint16{}
	for _, gid := range byCode {
		codeToGID[int(fu.code[gid])] = gid
	}
	for c := firstChar; c <= lastChar; c++ {
		gid, ok := codeToGID[c]
		name := ".notdef"
		adv := 0.0
		if ok {
			if n := face.GlyphName(gid); n != "" {
				name = n
			}
			adv = face.GlyphAdvance(gid) * 1000
		}
		if c != prev+1 {
			diffs = append(diffs, Int(int64(c)))
		}
		diffs = append(diffs, Name(name))
		prev = c
		widths = append(widths, Int(int64(adv+0.5)))
	}

	descriptor := w.alloc()
	w.put(descriptor, Dict{
		"Type":        Name("FontDescriptor"),
		"FontName":    Name("DTACUL+Embedded"),
		"Flags":       Int(4), // Symbolic
		"FontBBox":    Array{Int(-1000), Int(-1000), Int(2000), Int(2000)},
		"ItalicAngle": Int(0),
		"Ascent":      Int(800),
		"Descent":     Int(-200),
		"CapHeight":   Int(700),
		"StemV":       Int(80),
		"FontFile":    fontFile,
	})

	toUni := w.addStream(Dict{}, toUnicodeCMapHex(fu, false))

	fontRef := w.alloc()
	w.put(fontRef, Dict{
		"Type":           Name("Font"),
		"Subtype":        Name("Type1"),
		"BaseFont":       Name("DTACUL+Embedded"),
		"FirstChar":      Int(int64(firstChar)),
		"LastChar":       Int(int64(lastChar)),
		"Widths":         Array(widths),
		"FontDescriptor": descriptor,
		"Encoding":       Dict{"Type": Name("Encoding"), "Differences": diffs},
		"ToUnicode":      toUni,
	})
	return fontRef
}

// cidWidths builds a /W array mapping each GID (==CID under Identity) to its advance
// in 1000-unit glyph space, in ascending GID order.
func cidWidths(gids []uint16, face *font.Face) Array {
	var w Array
	for _, g := range gids {
		adv := face.GlyphAdvance(g) * 1000
		w = append(w, Int(int64(g)), Array{Int(int64(adv + 0.5))})
	}
	return w
}

// toUnicodeCMapHex builds a ToUnicode CMap mapping each emit code to its source
// UTF-16BE code units, so text is searchable/copyable. wide selects a 2-byte code
// space (Identity-H GIDs) vs a 1-byte code space (simple-font codes).
func toUnicodeCMapHex(fu *faceUse, wide bool) []byte {
	var b bytes.Buffer
	b.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n")
	b.WriteString("/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n")
	if wide {
		b.WriteString("1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	} else {
		b.WriteString("1 begincodespacerange\n<00> <FF>\nendcodespacerange\n")
	}
	// Emit in code order for determinism.
	codes := make([]int, 0, len(fu.code))
	codeToGID := map[int]uint16{}
	for gid, c := range fu.code {
		codes = append(codes, int(c))
		codeToGID[int(c)] = gid
	}
	sort.Ints(codes)
	fmt.Fprintf(&b, "%d beginbfchar\n", len(codes))
	for _, c := range codes {
		gid := codeToGID[c]
		if wide {
			fmt.Fprintf(&b, "<%04X> <%s>\n", c, utf16BEHex(fu.runes[gid]))
		} else {
			fmt.Fprintf(&b, "<%02X> <%s>\n", c, utf16BEHex(fu.runes[gid]))
		}
	}
	b.WriteString("endbfchar\nendcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
	return b.Bytes()
}

// utf16BEHex encodes runes as concatenated UTF-16BE hex (surrogate pairs for astral
// code points). An empty runes slice yields U+FFFD so no bfchar entry is empty.
func utf16BEHex(runes []rune) string {
	if len(runes) == 0 {
		return "FFFD"
	}
	var sb bytes.Buffer
	for _, r := range runes {
		if r > 0xFFFF {
			r -= 0x10000
			hi := 0xD800 + (r >> 10)
			lo := 0xDC00 + (r & 0x3FF)
			fmt.Fprintf(&sb, "%04X%04X", hi, lo)
		} else {
			fmt.Fprintf(&sb, "%04X", r)
		}
	}
	return sb.String()
}

// pfbToType1 decodes a PFB (segmented Type1) container into the raw Type1 program a
// PDF /FontFile carries, returning the concatenated bytes plus the three segment
// lengths: Length1 (clear-text header), Length2 (binary eexec portion), and
// Length3 (clear-text trailer). It accepts a raw (non-PFB) Type1 by falling back to
// a heuristic split. It never panics on malformed input.
func pfbToType1(data []byte) (prog []byte, l1, l2, l3 int, err error) {
	if len(data) >= 2 && data[0] == 0x80 {
		return decodePFBSegments(data)
	}
	// Raw Type1 (no PFB wrapper): split at "eexec" and the trailing zeros.
	return splitRawType1(data)
}

// decodePFBSegments walks the 0x80-marked PFB segments, concatenating the data and
// summing lengths by segment type (1 = ASCII text, 2 = binary). The three Type1
// portions are: leading ASCII (Length1), the binary eexec block (Length2), and the
// trailing ASCII (Length3). Type-3 (0x80 0x03) marks end-of-file.
func decodePFBSegments(data []byte) (prog []byte, l1, l2, l3 int, err error) {
	var out bytes.Buffer
	i := 0
	// A PFB is a sequence of records: 0x80, type(1|2|3), len(4 LE), then len bytes.
	// We accumulate into the three Type1 portions in order: any ASCII before the
	// first binary block is Length1, binary blocks are Length2, ASCII after is
	// Length3.
	phase := 0 // 0 = header ASCII, 1 = binary, 2 = trailer ASCII
	for i+2 <= len(data) {
		if data[i] != 0x80 {
			return nil, 0, 0, 0, fmt.Errorf("pdfwrite: bad PFB segment marker at %d", i)
		}
		segType := data[i+1]
		if segType == 3 { // EOF
			break
		}
		if i+6 > len(data) {
			return nil, 0, 0, 0, fmt.Errorf("pdfwrite: truncated PFB segment header at %d", i)
		}
		n := int(binary.LittleEndian.Uint32(data[i+2 : i+6]))
		start := i + 6
		if start+n > len(data) {
			return nil, 0, 0, 0, fmt.Errorf("pdfwrite: PFB segment overruns data (%d+%d > %d)", start, n, len(data))
		}
		seg := data[start : start+n]
		switch segType {
		case 1: // ASCII
			if phase == 1 {
				phase = 2 // first ASCII after binary -> trailer
			}
			if phase == 0 {
				l1 += n
			} else {
				l3 += n
			}
		case 2: // binary
			phase = 1
			l2 += n
		default:
			return nil, 0, 0, 0, fmt.Errorf("pdfwrite: unknown PFB segment type %d", segType)
		}
		out.Write(seg)
		i = start + n
	}
	if l1 == 0 || l2 == 0 {
		return nil, 0, 0, 0, fmt.Errorf("pdfwrite: PFB missing header or eexec segment")
	}
	if l3 == 0 {
		// Some PFBs omit an explicit trailer segment; a valid Type1 needs the
		// 512 zeros + "cleartomark" trailer, so synthesize one.
		trailer := type1Trailer()
		out.Write(trailer)
		l3 = len(trailer)
	}
	return out.Bytes(), l1, l2, l3, nil
}

// splitRawType1 splits a non-PFB (raw) Type1 program into its three portions by
// locating the eexec-encrypted block: Length1 ends at (and includes) "eexec\n",
// Length3 is the standard 512-zero + cleartomark trailer, Length2 is the middle.
func splitRawType1(data []byte) (prog []byte, l1, l2, l3 int, err error) {
	idx := bytes.Index(data, []byte("eexec"))
	if idx < 0 {
		return nil, 0, 0, 0, fmt.Errorf("pdfwrite: raw Type1 has no eexec marker")
	}
	// Advance past "eexec" and any following whitespace (CR/LF/space/tab).
	h := idx + len("eexec")
	for h < len(data) && (data[h] == '\r' || data[h] == '\n' || data[h] == ' ' || data[h] == '\t') {
		h++
	}
	l1 = h
	// The trailer is the trailing run of zero-bytes preceded by "cleartomark"; find
	// the last run of 512 '0' ASCII characters (Type1 trailer) or fall back to a
	// synthesized trailer.
	tIdx := bytes.LastIndex(data, []byte("0000000000000000")) // part of the 512 zeros
	if tIdx > l1 {
		// Walk back to the first zero of the trailer run.
		start := tIdx
		for start > l1 && data[start-1] == '0' {
			start--
		}
		l2 = start - l1
		l3 = len(data) - start
	} else {
		l2 = len(data) - l1
		l3 = 0
	}
	if l3 == 0 {
		trailer := type1Trailer()
		out := append(append([]byte(nil), data...), trailer...)
		return out, l1, l2, len(trailer), nil
	}
	return data, l1, l2, l3, nil
}

// type1Trailer returns the canonical Type1 trailer (512 ASCII '0's on 8 lines
// followed by cleartomark), used when a container omits an explicit trailer.
func type1Trailer() []byte {
	var b bytes.Buffer
	for i := 0; i < 8; i++ {
		b.WriteString("0000000000000000000000000000000000000000000000000000000000000000\n")
	}
	b.WriteString("cleartomark\n")
	return b.Bytes()
}
