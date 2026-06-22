// Package font: cff_charset.go
//
// Parser for the CFF (Compact Font Format) charset, producing a
// glyph-name -> GID (glyph index) map. PDF "Type1C" / CIDFontType0C fonts
// embed a bare CFF table; a simple (single-byte-encoded) Type1C font needs
// code -> glyphname -> GID resolution, which golang.org/x/image/font/sfnt
// does not expose for CFF fonts (the post table is absent in CFF, and sfnt
// only surfaces GID->name there). cffNameToGID fills that gap.
//
// Spec reference: Adobe Technical Note #5176 "The Compact Font Format
// Specification" (5176.CFF.pdf).
package font

import "encoding/binary"

// cffNameToGID returns errInvalidCFF (defined in cff.go) for any
// malformed/out-of-bounds input.

// maxCFFGlyphs caps numGlyphs to a sane upper bound. The CFF/OpenType GID
// space is 16-bit, so 65535 is the hard ceiling; anything larger is corrupt.
const maxCFFGlyphs = 65535

// cffReadOff reads an offSize-byte (1..4) big-endian unsigned integer at off,
// bounds-checked. Named distinctly from readOffset so this file stays
// self-contained even if the package helper changes.
func cffReadOff(buf []byte, off, offSize int) (int, error) {
	if offSize < 1 || offSize > 4 || off < 0 || off+offSize > len(buf) {
		return 0, errInvalidCFF
	}
	v := 0
	for i := range offSize {
		v = v<<8 | int(buf[off+i])
	}
	return v, nil
}

// cffIndex describes a parsed INDEX: the absolute byte offsets of each object
// plus the absolute offset one past the end of the whole INDEX.
type cffIndex struct {
	offsets []int // len == count+1; offsets[i] is the absolute start of object i
	end     int   // absolute offset just past the INDEX (start of next structure)
}

// count returns the number of objects in the INDEX.
func (ix *cffIndex) count() int {
	if len(ix.offsets) == 0 {
		return 0
	}
	return len(ix.offsets) - 1
}

// object returns the raw bytes of object i, bounds-checked.
func (ix *cffIndex) object(cff []byte, i int) ([]byte, error) {
	if i < 0 || i+1 >= len(ix.offsets) {
		return nil, errInvalidCFF
	}
	lo, hi := ix.offsets[i], ix.offsets[i+1]
	if lo < 0 || hi < lo || hi > len(cff) {
		return nil, errInvalidCFF
	}
	return cff[lo:hi], nil
}

// parseIndex parses a CFF INDEX beginning at pos and returns it.
//
// Layout: count(uint16). If count==0 the INDEX is exactly those 2 bytes.
// Otherwise: offSize(uint8, 1..4), then count+1 offsets of offSize bytes each
// (1-based, relative to dataBase), then the object data. dataBase is the byte
// immediately before the first object, i.e. (offset-array-end - 1), so that a
// 1-based offset of 1 points at the first data byte.
func parseIndex(cff []byte, pos int) (*cffIndex, error) {
	if pos < 0 || pos+2 > len(cff) {
		return nil, errInvalidCFF
	}
	count := int(binary.BigEndian.Uint16(cff[pos:]))
	if count == 0 {
		return &cffIndex{offsets: nil, end: pos + 2}, nil
	}
	if count > maxCFFGlyphs {
		return nil, errInvalidCFF
	}
	offSizePos := pos + 2
	if offSizePos+1 > len(cff) {
		return nil, errInvalidCFF
	}
	offSize := int(cff[offSizePos])
	if offSize < 1 || offSize > 4 {
		return nil, errInvalidCFF
	}
	offArrayStart := offSizePos + 1
	nOffsets := count + 1
	// Offset array must fit.
	if offArrayStart+nOffsets*offSize > len(cff) {
		return nil, errInvalidCFF
	}
	// dataBase: offsets are 1-based relative to the byte before the first
	// data byte. The data begins right after the offset array, so the base
	// such that base+1 == firstDataByte is (offArrayStart + nOffsets*offSize) - 1.
	dataBase := offArrayStart + nOffsets*offSize - 1

	offsets := make([]int, nOffsets)
	prev := -1
	for i := range nOffsets {
		rel, err := cffReadOff(cff, offArrayStart+i*offSize, offSize)
		if err != nil {
			return nil, err
		}
		if rel < 1 {
			return nil, errInvalidCFF // offsets are 1-based
		}
		abs := dataBase + rel
		if abs < 0 || abs > len(cff) {
			return nil, errInvalidCFF
		}
		// Offsets must be monotonically non-decreasing.
		if abs < prev {
			return nil, errInvalidCFF
		}
		prev = abs
		offsets[i] = abs
	}
	return &cffIndex{offsets: offsets, end: offsets[nOffsets-1]}, nil
}

// cffTopDict holds the operators we care about from the Top DICT.
type cffTopDict struct {
	charsetOff     int
	charStringsOff int
	hasCharset     bool
	hasCharStrings bool
}

// parseCFFTopDict decodes a Top DICT byte string, extracting the charset (op
// 15) and CharStrings (op 17) offset operators. Operand/operator encoding
// follows 5176 §4. Operands are pushed onto a small stack; an operator
// consumes the current stack and clears it.
func parseCFFTopDict(d []byte) (cffTopDict, error) {
	var td cffTopDict
	// Operand stack. CFF DICTs never legitimately exceed 48 operands; cap to
	// avoid unbounded growth on malformed input.
	const maxStack = 48
	stack := make([]int, 0, maxStack)
	push := func(v int) bool {
		if len(stack) >= maxStack {
			return false
		}
		stack = append(stack, v)
		return true
	}

	i := 0
	for i < len(d) {
		b0 := int(d[i])
		switch {
		case b0 >= 32 && b0 <= 246:
			if !push(b0 - 139) {
				return td, errInvalidCFF
			}
			i++
		case b0 >= 247 && b0 <= 250:
			if i+1 >= len(d) {
				return td, errInvalidCFF
			}
			b1 := int(d[i+1])
			if !push((b0-247)*256 + b1 + 108) {
				return td, errInvalidCFF
			}
			i += 2
		case b0 >= 251 && b0 <= 254:
			if i+1 >= len(d) {
				return td, errInvalidCFF
			}
			b1 := int(d[i+1])
			if !push(-(b0-251)*256 - b1 - 108) {
				return td, errInvalidCFF
			}
			i += 2
		case b0 == 28:
			if i+2 >= len(d) {
				return td, errInvalidCFF
			}
			v := int(int16(uint16(d[i+1])<<8 | uint16(d[i+2])))
			if !push(v) {
				return td, errInvalidCFF
			}
			i += 3
		case b0 == 29:
			if i+4 >= len(d) {
				return td, errInvalidCFF
			}
			v := int(int32(uint32(d[i+1])<<24 | uint32(d[i+2])<<16 | uint32(d[i+3])<<8 | uint32(d[i+4])))
			if !push(v) {
				return td, errInvalidCFF
			}
			i += 5
		case b0 == 30:
			// Real number: nibble-encoded, two nibbles per byte (high then
			// low), terminated by nibble 0xf. The value is irrelevant to us,
			// so we only need to consume the bytes and (to keep the stack
			// arithmetic sane) push a 0 placeholder.
			i++
			done := false
			for i < len(d) && !done {
				bb := d[i]
				i++
				for _, nib := range [2]byte{bb >> 4, bb & 0x0f} {
					if nib == 0x0f {
						done = true
						break
					}
				}
			}
			if !done {
				return td, errInvalidCFF // unterminated real
			}
			if !push(0) {
				return td, errInvalidCFF
			}
		case b0 <= 21:
			// Operator.
			op := b0
			if b0 == 12 {
				if i+1 >= len(d) {
					return td, errInvalidCFF
				}
				op = 1200 + int(d[i+1])
				i += 2
			} else {
				i++
			}
			switch op {
			case 15: // charset
				if len(stack) < 1 {
					return td, errInvalidCFF
				}
				td.charsetOff = stack[len(stack)-1]
				td.hasCharset = true
			case 17: // CharStrings
				if len(stack) < 1 {
					return td, errInvalidCFF
				}
				td.charStringsOff = stack[len(stack)-1]
				td.hasCharStrings = true
			}
			stack = stack[:0] // clear operand stack after any operator
		default:
			// b0 in 22..27 or 31 are reserved/unused in DICTs.
			return td, errInvalidCFF
		}
	}
	return td, nil
}

// cffSIDToName resolves a String ID to its PostScript name. SIDs below the
// standard-strings count index the embedded standard table; the remainder
// index the font's String INDEX (offset by len(cffStandardStrings)).
func cffSIDToName(sid int, strIndex *cffIndex, cff []byte) (string, bool) {
	if sid < 0 {
		return "", false
	}
	if sid < len(cffStandardStrings) {
		return cffStandardStrings[sid], true
	}
	idx := sid - len(cffStandardStrings)
	if idx >= strIndex.count() {
		return "", false
	}
	b, err := strIndex.object(cff, idx)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// parseCharsetSIDs reads the charset table at off and returns the SID assigned
// to each GID (length == numGlyphs). GID 0 is always .notdef (SID 0) and is
// not stored in the table. Supports formats 0, 1 and 2.
func parseCharsetSIDs(cff []byte, off, numGlyphs int) ([]int, error) {
	if numGlyphs < 1 {
		return nil, errInvalidCFF
	}
	sids := make([]int, numGlyphs)
	sids[0] = 0 // .notdef
	if numGlyphs == 1 {
		return sids, nil
	}
	if off < 0 || off >= len(cff) {
		return nil, errInvalidCFF
	}
	format := cff[off]
	p := off + 1
	switch format {
	case 0:
		// numGlyphs-1 SIDs, each uint16, for GID 1,2,3,...
		for gid := 1; gid < numGlyphs; gid++ {
			if p+2 > len(cff) {
				return nil, errInvalidCFF
			}
			sids[gid] = int(binary.BigEndian.Uint16(cff[p:]))
			p += 2
		}
	case 1, 2:
		gid := 1
		for gid < numGlyphs {
			if p+2 > len(cff) {
				return nil, errInvalidCFF
			}
			first := int(binary.BigEndian.Uint16(cff[p:]))
			p += 2
			var nLeft int
			if format == 1 {
				if p+1 > len(cff) {
					return nil, errInvalidCFF
				}
				nLeft = int(cff[p])
				p++
			} else { // format 2
				if p+2 > len(cff) {
					return nil, errInvalidCFF
				}
				nLeft = int(binary.BigEndian.Uint16(cff[p:]))
				p += 2
			}
			// Assign SIDs first, first+1, ..., first+nLeft to consecutive GIDs.
			for k := 0; k <= nLeft && gid < numGlyphs; k++ {
				sid := first + k
				if sid > 0xffff {
					return nil, errInvalidCFF
				}
				sids[gid] = sid
				gid++
			}
		}
	default:
		return nil, errInvalidCFF
	}
	return sids, nil
}

// cffParseTopLevel parses a bare CFF's fixed prefix: header, Name INDEX, Top
// DICT INDEX, and String INDEX, then decodes the Top DICT and derives the glyph
// count from the CharStrings INDEX. It is the shared front end for both the
// charset (cffNameToGID) and the glyph-count (cffNumGlyphs) paths.
func cffParseTopLevel(cff []byte) (td cffTopDict, strIdx *cffIndex, numGlyphs int, err error) {
	// Header.
	if len(cff) < 4 {
		return td, nil, 0, errInvalidCFF
	}
	if cff[0] != 1 { // major version
		return td, nil, 0, errInvalidCFF
	}
	hdrSize := int(cff[2])
	if hdrSize < 4 || hdrSize > len(cff) {
		return td, nil, 0, errInvalidCFF
	}

	nameIdx, err := parseIndex(cff, hdrSize)
	if err != nil {
		return td, nil, 0, err
	}
	topIdx, err := parseIndex(cff, nameIdx.end)
	if err != nil {
		return td, nil, 0, err
	}
	if topIdx.count() < 1 {
		return td, nil, 0, errInvalidCFF
	}
	strIdx, err = parseIndex(cff, topIdx.end) // String INDEX follows the Top DICT INDEX
	if err != nil {
		return td, nil, 0, err
	}

	topBytes, err := topIdx.object(cff, 0)
	if err != nil {
		return td, nil, 0, err
	}
	td, err = parseCFFTopDict(topBytes)
	if err != nil {
		return td, nil, 0, err
	}
	if !td.hasCharStrings || td.charStringsOff < 0 || td.charStringsOff >= len(cff) {
		return td, nil, 0, errInvalidCFF
	}
	csIdx, err := parseIndex(cff, td.charStringsOff)
	if err != nil {
		return td, nil, 0, err
	}
	numGlyphs = csIdx.count()
	if numGlyphs < 1 || numGlyphs > maxCFFGlyphs {
		return td, nil, 0, errInvalidCFF
	}
	return td, strIdx, numGlyphs, nil
}

// cffNumGlyphs returns the glyph count of a bare CFF table (the CharStrings
// INDEX count), which the OTTO wrapper needs to synthesize a matching maxp.
func cffNumGlyphs(cff []byte) (int, error) {
	_, _, numGlyphs, err := cffParseTopLevel(cff)
	return numGlyphs, err
}

// cffNameToGID parses a bare CFF table and returns a map from each glyph's
// PostScript name to its glyph index (GID). On any malformed or out-of-bounds
// input it returns a nil map and errInvalidCFF; it never panics.
//
// Approximation: when the Top DICT has no charset operator, or the charset
// offset is a predefined-charset id (0 ISOAdobe, 1 Expert, 2 ExpertSubset),
// glyph names are approximated by mapping GID i -> cffStandardStrings[i]
// (i.e. SID i) for i < len(cffStandardStrings); higher GIDs are skipped. This
// matches the ISOAdobe ordering for the common default case and is sufficient
// for the simple Type1C fonts this targets; the Expert/ExpertSubset predefined
// orderings are not reproduced exactly.
func cffNameToGID(cff []byte) (map[string]uint16, error) {
	td, strIdx, numGlyphs, err := cffParseTopLevel(cff)
	if err != nil {
		return nil, err
	}

	result := make(map[string]uint16, numGlyphs)

	// Predefined / absent charset: approximate via standard-string ordering.
	if !td.hasCharset || td.charsetOff == 0 || td.charsetOff == 1 || td.charsetOff == 2 {
		for gid := 0; gid < numGlyphs && gid < len(cffStandardStrings); gid++ {
			name := cffStandardStrings[gid]
			if name == "" {
				continue
			}
			if _, exists := result[name]; !exists {
				result[name] = uint16(gid)
			}
		}
		return result, nil
	}

	// Explicit embedded charset: GID -> SID -> name.
	sids, err := parseCharsetSIDs(cff, td.charsetOff, numGlyphs)
	if err != nil {
		return nil, err
	}
	for gid := range numGlyphs {
		name, ok := cffSIDToName(sids[gid], strIdx, cff)
		if !ok || name == "" {
			// Unresolvable SID: skip this glyph rather than failing the
			// whole font (graceful degradation).
			continue
		}
		// First GID wins on duplicate names (lowest GID), which is the
		// conventional choice for code->glyph resolution.
		if _, exists := result[name]; !exists {
			result[name] = uint16(gid)
		}
	}
	return result, nil
}

// cffStandardStrings is the Adobe CFF predefined "Standard Strings" table
// (5176 Appendix A): 391 entries indexed by SID (0..390). It is identical to
// FreeType's cff_standard_strings and fontTools' cffStandardStrings. SIDs at
// or above len(cffStandardStrings) index the font's String INDEX (offset by
// this length).
var cffStandardStrings = [...]string{
	".notdef",             // 0
	"space",               // 1
	"exclam",              // 2
	"quotedbl",            // 3
	"numbersign",          // 4
	"dollar",              // 5
	"percent",             // 6
	"ampersand",           // 7
	"quoteright",          // 8
	"parenleft",           // 9
	"parenright",          // 10
	"asterisk",            // 11
	"plus",                // 12
	"comma",               // 13
	"hyphen",              // 14
	"period",              // 15
	"slash",               // 16
	"zero",                // 17
	"one",                 // 18
	"two",                 // 19
	"three",               // 20
	"four",                // 21
	"five",                // 22
	"six",                 // 23
	"seven",               // 24
	"eight",               // 25
	"nine",                // 26
	"colon",               // 27
	"semicolon",           // 28
	"less",                // 29
	"equal",               // 30
	"greater",             // 31
	"question",            // 32
	"at",                  // 33
	"A",                   // 34
	"B",                   // 35
	"C",                   // 36
	"D",                   // 37
	"E",                   // 38
	"F",                   // 39
	"G",                   // 40
	"H",                   // 41
	"I",                   // 42
	"J",                   // 43
	"K",                   // 44
	"L",                   // 45
	"M",                   // 46
	"N",                   // 47
	"O",                   // 48
	"P",                   // 49
	"Q",                   // 50
	"R",                   // 51
	"S",                   // 52
	"T",                   // 53
	"U",                   // 54
	"V",                   // 55
	"W",                   // 56
	"X",                   // 57
	"Y",                   // 58
	"Z",                   // 59
	"bracketleft",         // 60
	"backslash",           // 61
	"bracketright",        // 62
	"asciicircum",         // 63
	"underscore",          // 64
	"quoteleft",           // 65
	"a",                   // 66
	"b",                   // 67
	"c",                   // 68
	"d",                   // 69
	"e",                   // 70
	"f",                   // 71
	"g",                   // 72
	"h",                   // 73
	"i",                   // 74
	"j",                   // 75
	"k",                   // 76
	"l",                   // 77
	"m",                   // 78
	"n",                   // 79
	"o",                   // 80
	"p",                   // 81
	"q",                   // 82
	"r",                   // 83
	"s",                   // 84
	"t",                   // 85
	"u",                   // 86
	"v",                   // 87
	"w",                   // 88
	"x",                   // 89
	"y",                   // 90
	"z",                   // 91
	"braceleft",           // 92
	"bar",                 // 93
	"braceright",          // 94
	"asciitilde",          // 95
	"exclamdown",          // 96
	"cent",                // 97
	"sterling",            // 98
	"fraction",            // 99
	"yen",                 // 100
	"florin",              // 101
	"section",             // 102
	"currency",            // 103
	"quotesingle",         // 104
	"quotedblleft",        // 105
	"guillemotleft",       // 106
	"guilsinglleft",       // 107
	"guilsinglright",      // 108
	"fi",                  // 109
	"fl",                  // 110
	"endash",              // 111
	"dagger",              // 112
	"daggerdbl",           // 113
	"periodcentered",      // 114
	"paragraph",           // 115
	"bullet",              // 116
	"quotesinglbase",      // 117
	"quotedblbase",        // 118
	"quotedblright",       // 119
	"guillemotright",      // 120
	"ellipsis",            // 121
	"perthousand",         // 122
	"questiondown",        // 123
	"grave",               // 124
	"acute",               // 125
	"circumflex",          // 126
	"tilde",               // 127
	"macron",              // 128
	"breve",               // 129
	"dotaccent",           // 130
	"dieresis",            // 131
	"ring",                // 132
	"cedilla",             // 133
	"hungarumlaut",        // 134
	"ogonek",              // 135
	"caron",               // 136
	"emdash",              // 137
	"AE",                  // 138
	"ordfeminine",         // 139
	"Lslash",              // 140
	"Oslash",              // 141
	"OE",                  // 142
	"ordmasculine",        // 143
	"ae",                  // 144
	"dotlessi",            // 145
	"lslash",              // 146
	"oslash",              // 147
	"oe",                  // 148
	"germandbls",          // 149
	"onesuperior",         // 150
	"logicalnot",          // 151
	"mu",                  // 152
	"trademark",           // 153
	"Eth",                 // 154
	"onehalf",             // 155
	"plusminus",           // 156
	"Thorn",               // 157
	"onequarter",          // 158
	"divide",              // 159
	"brokenbar",           // 160
	"degree",              // 161
	"thorn",               // 162
	"threequarters",       // 163
	"twosuperior",         // 164
	"registered",          // 165
	"minus",               // 166
	"eth",                 // 167
	"multiply",            // 168
	"threesuperior",       // 169
	"copyright",           // 170
	"Aacute",              // 171
	"Acircumflex",         // 172
	"Adieresis",           // 173
	"Agrave",              // 174
	"Aring",               // 175
	"Atilde",              // 176
	"Ccedilla",            // 177
	"Eacute",              // 178
	"Ecircumflex",         // 179
	"Edieresis",           // 180
	"Egrave",              // 181
	"Iacute",              // 182
	"Icircumflex",         // 183
	"Idieresis",           // 184
	"Igrave",              // 185
	"Ntilde",              // 186
	"Oacute",              // 187
	"Ocircumflex",         // 188
	"Odieresis",           // 189
	"Ograve",              // 190
	"Otilde",              // 191
	"Scaron",              // 192
	"Uacute",              // 193
	"Ucircumflex",         // 194
	"Udieresis",           // 195
	"Ugrave",              // 196
	"Yacute",              // 197
	"Ydieresis",           // 198
	"Zcaron",              // 199
	"aacute",              // 200
	"acircumflex",         // 201
	"adieresis",           // 202
	"agrave",              // 203
	"aring",               // 204
	"atilde",              // 205
	"ccedilla",            // 206
	"eacute",              // 207
	"ecircumflex",         // 208
	"edieresis",           // 209
	"egrave",              // 210
	"iacute",              // 211
	"icircumflex",         // 212
	"idieresis",           // 213
	"igrave",              // 214
	"ntilde",              // 215
	"oacute",              // 216
	"ocircumflex",         // 217
	"odieresis",           // 218
	"ograve",              // 219
	"otilde",              // 220
	"scaron",              // 221
	"uacute",              // 222
	"ucircumflex",         // 223
	"udieresis",           // 224
	"ugrave",              // 225
	"yacute",              // 226
	"ydieresis",           // 227
	"zcaron",              // 228
	"exclamsmall",         // 229
	"Hungarumlautsmall",   // 230
	"dollaroldstyle",      // 231
	"dollarsuperior",      // 232
	"ampersandsmall",      // 233
	"Acutesmall",          // 234
	"parenleftsuperior",   // 235
	"parenrightsuperior",  // 236
	"twodotenleader",      // 237
	"onedotenleader",      // 238
	"zerooldstyle",        // 239
	"oneoldstyle",         // 240
	"twooldstyle",         // 241
	"threeoldstyle",       // 242
	"fouroldstyle",        // 243
	"fiveoldstyle",        // 244
	"sixoldstyle",         // 245
	"sevenoldstyle",       // 246
	"eightoldstyle",       // 247
	"nineoldstyle",        // 248
	"commasuperior",       // 249
	"threequartersemdash", // 250
	"periodsuperior",      // 251
	"questionsmall",       // 252
	"asuperior",           // 253
	"bsuperior",           // 254
	"centsuperior",        // 255
	"dsuperior",           // 256
	"esuperior",           // 257
	"isuperior",           // 258
	"lsuperior",           // 259
	"msuperior",           // 260
	"nsuperior",           // 261
	"osuperior",           // 262
	"rsuperior",           // 263
	"ssuperior",           // 264
	"tsuperior",           // 265
	"ff",                  // 266
	"ffi",                 // 267
	"ffl",                 // 268
	"parenleftinferior",   // 269
	"parenrightinferior",  // 270
	"Circumflexsmall",     // 271
	"hyphensuperior",      // 272
	"Gravesmall",          // 273
	"Asmall",              // 274
	"Bsmall",              // 275
	"Csmall",              // 276
	"Dsmall",              // 277
	"Esmall",              // 278
	"Fsmall",              // 279
	"Gsmall",              // 280
	"Hsmall",              // 281
	"Ismall",              // 282
	"Jsmall",              // 283
	"Ksmall",              // 284
	"Lsmall",              // 285
	"Msmall",              // 286
	"Nsmall",              // 287
	"Osmall",              // 288
	"Psmall",              // 289
	"Qsmall",              // 290
	"Rsmall",              // 291
	"Ssmall",              // 292
	"Tsmall",              // 293
	"Usmall",              // 294
	"Vsmall",              // 295
	"Wsmall",              // 296
	"Xsmall",              // 297
	"Ysmall",              // 298
	"Zsmall",              // 299
	"colonmonetary",       // 300
	"onefitted",           // 301
	"rupiah",              // 302
	"Tildesmall",          // 303
	"exclamdownsmall",     // 304
	"centoldstyle",        // 305
	"Lslashsmall",         // 306
	"Scaronsmall",         // 307
	"Zcaronsmall",         // 308
	"Dieresissmall",       // 309
	"Brevesmall",          // 310
	"Caronsmall",          // 311
	"Dotaccentsmall",      // 312
	"Macronsmall",         // 313
	"figuredash",          // 314
	"hypheninferior",      // 315
	"Ogoneksmall",         // 316
	"Ringsmall",           // 317
	"Cedillasmall",        // 318
	"questiondownsmall",   // 319
	"oneeighth",           // 320
	"threeeighths",        // 321
	"fiveeighths",         // 322
	"seveneighths",        // 323
	"onethird",            // 324
	"twothirds",           // 325
	"zerosuperior",        // 326
	"foursuperior",        // 327
	"fivesuperior",        // 328
	"sixsuperior",         // 329
	"sevensuperior",       // 330
	"eightsuperior",       // 331
	"ninesuperior",        // 332
	"zeroinferior",        // 333
	"oneinferior",         // 334
	"twoinferior",         // 335
	"threeinferior",       // 336
	"fourinferior",        // 337
	"fiveinferior",        // 338
	"sixinferior",         // 339
	"seveninferior",       // 340
	"eightinferior",       // 341
	"nineinferior",        // 342
	"centinferior",        // 343
	"dollarinferior",      // 344
	"periodinferior",      // 345
	"commainferior",       // 346
	"Agravesmall",         // 347
	"Aacutesmall",         // 348
	"Acircumflexsmall",    // 349
	"Atildesmall",         // 350
	"Adieresissmall",      // 351
	"Aringsmall",          // 352
	"AEsmall",             // 353
	"Ccedillasmall",       // 354
	"Egravesmall",         // 355
	"Eacutesmall",         // 356
	"Ecircumflexsmall",    // 357
	"Edieresissmall",      // 358
	"Igravesmall",         // 359
	"Iacutesmall",         // 360
	"Icircumflexsmall",    // 361
	"Idieresissmall",      // 362
	"Ethsmall",            // 363
	"Ntildesmall",         // 364
	"Ogravesmall",         // 365
	"Oacutesmall",         // 366
	"Ocircumflexsmall",    // 367
	"Otildesmall",         // 368
	"Odieresissmall",      // 369
	"OEsmall",             // 370
	"Oslashsmall",         // 371
	"Ugravesmall",         // 372
	"Uacutesmall",         // 373
	"Ucircumflexsmall",    // 374
	"Udieresissmall",      // 375
	"Yacutesmall",         // 376
	"Thornsmall",          // 377
	"Ydieresissmall",      // 378
	"001.000",             // 379
	"001.001",             // 380
	"001.002",             // 381
	"001.003",             // 382
	"Black",               // 383
	"Bold",                // 384
	"Book",                // 385
	"Light",               // 386
	"Medium",              // 387
	"Regular",             // 388
	"Roman",               // 389
	"Semibold",            // 390
}
