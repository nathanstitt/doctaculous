package font

import "strconv"

// This file maps single-byte character codes to Unicode runes for the simple
// (non-composite) font encodings PDF uses. The simple-font path resolves
// code→glyph name→GID where possible, and falls back to code→rune→GID through the
// program's own cmap; this table provides both the code→rune mapping and the
// code→name mapping (via runeToGlyphName).
//
// Coverage is the standard Latin set shared by WinAnsi, MacRoman, and Standard
// encodings plus a glyph-name table for /Differences entries. It is deliberately
// not the full Adobe Glyph List; names outside this set resolve to rune 0 and
// the glyph falls back to .notdef.

// baseEncoding selects which built-in code→rune table to start from.
type baseEncoding int

const (
	encWinAnsi baseEncoding = iota
	encMacRoman
	encStandard
)

// baseEncodingByName maps a PDF /BaseEncoding (or /Encoding) name to a table.
// An unknown or empty name yields WinAnsi, the pragmatic default for embedded
// nonsymbolic fonts.
func baseEncodingByName(name string) baseEncoding {
	switch name {
	case "MacRomanEncoding":
		return encMacRoman
	case "StandardEncoding":
		return encStandard
	default:
		return encWinAnsi
	}
}

// codeToRune returns the rune for a code in the given base encoding, or 0 if the
// code is unmapped.
func codeToRune(enc baseEncoding, code byte) rune {
	// 0x20..0x7E is plain ASCII in all three encodings (with one Standard-only
	// exception handled below for 0x27 and 0x60).
	switch enc {
	case encMacRoman:
		if r, ok := macRomanHigh[code]; ok {
			return r
		}
	case encStandard:
		if r, ok := standardLow[code]; ok {
			return r
		}
		if r, ok := standardHigh[code]; ok {
			return r
		}
	default: // WinAnsi
		if r, ok := winAnsiHigh[code]; ok {
			return r
		}
	}
	if code >= 0x20 && code <= 0x7E {
		return rune(code)
	}
	return 0
}

// runeToGlyphName returns the canonical PostScript glyph name for a rune, or ""
// if none is known. It is the inverse of glyphNames and is used to resolve a
// code's glyph name for CFF charset (name→GID) lookup. Returns 0-rune as "".
func runeToGlyphName(r rune) string {
	if r == 0 {
		return ""
	}
	return runeNames[r]
}

// runeNames is the reverse of glyphNames, built once at init. Where multiple
// names map to the same rune, the first inserted wins; this is fine because the
// standard Latin names are unique per rune.
var runeNames = func() map[rune]string {
	m := make(map[rune]string, len(glyphNames))
	for name, r := range glyphNames {
		if _, ok := m[r]; !ok {
			m[r] = name
		}
	}
	return m
}()

// glyphNameToRune resolves a PostScript glyph name (from an /Encoding
// /Differences array) to a rune. It handles the uniXXXX/uXXXXXX forms exactly
// and otherwise consults the standard-name table. Returns 0 if unknown.
func glyphNameToRune(name string) rune {
	if r, ok := glyphNames[name]; ok {
		return r
	}
	// uniXXXX (exactly 4 hex digits) → U+XXXX.
	if len(name) == 7 && name[:3] == "uni" {
		if v, err := strconv.ParseUint(name[3:], 16, 32); err == nil {
			return rune(v)
		}
	}
	// uXXXX..uXXXXXX (4–6 hex digits) → that scalar value.
	if len(name) >= 5 && len(name) <= 7 && name[0] == 'u' {
		if v, err := strconv.ParseUint(name[1:], 16, 32); err == nil {
			return rune(v)
		}
	}
	return 0
}

// winAnsiHigh maps the non-ASCII WinAnsi (Windows-1252) code points. The
// 0x80–0x9F block is Windows-1252 specific (smart quotes, dashes, euro, …) and
// 0xA0–0xFF follows Latin-1.
var winAnsiHigh = map[byte]rune{
	0x80: '€', 0x82: '‚', 0x83: 'ƒ', 0x84: '„', 0x85: '…',
	0x86: '†', 0x87: '‡', 0x88: 'ˆ', 0x89: '‰', 0x8A: 'Š',
	0x8B: '‹', 0x8C: 'Œ', 0x8E: 'Ž', 0x91: '‘', 0x92: '’',
	0x93: '“', 0x94: '”', 0x95: '•', 0x96: '–', 0x97: '—',
	0x98: '˜', 0x99: '™', 0x9A: 'š', 0x9B: '›', 0x9C: 'œ',
	0x9E: 'ž', 0x9F: 'Ÿ',
	0xA0: ' ', 0xA1: '¡', 0xA2: '¢', 0xA3: '£', 0xA4: '¤',
	0xA5: '¥', 0xA6: '¦', 0xA7: '§', 0xA8: '¨', 0xA9: '©',
	0xAA: 'ª', 0xAB: '«', 0xAC: '¬', 0xAD: '­', 0xAE: '®',
	0xAF: '¯', 0xB0: '°', 0xB1: '±', 0xB2: '²', 0xB3: '³',
	0xB4: '´', 0xB5: 'µ', 0xB6: '¶', 0xB7: '·', 0xB8: '¸',
	0xB9: '¹', 0xBA: 'º', 0xBB: '»', 0xBC: '¼', 0xBD: '½',
	0xBE: '¾', 0xBF: '¿', 0xC0: 'À', 0xC1: 'Á', 0xC2: 'Â',
	0xC3: 'Ã', 0xC4: 'Ä', 0xC5: 'Å', 0xC6: 'Æ', 0xC7: 'Ç',
	0xC8: 'È', 0xC9: 'É', 0xCA: 'Ê', 0xCB: 'Ë', 0xCC: 'Ì',
	0xCD: 'Í', 0xCE: 'Î', 0xCF: 'Ï', 0xD0: 'Ð', 0xD1: 'Ñ',
	0xD2: 'Ò', 0xD3: 'Ó', 0xD4: 'Ô', 0xD5: 'Õ', 0xD6: 'Ö',
	0xD7: '×', 0xD8: 'Ø', 0xD9: 'Ù', 0xDA: 'Ú', 0xDB: 'Û',
	0xDC: 'Ü', 0xDD: 'Ý', 0xDE: 'Þ', 0xDF: 'ß', 0xE0: 'à',
	0xE1: 'á', 0xE2: 'â', 0xE3: 'ã', 0xE4: 'ä', 0xE5: 'å',
	0xE6: 'æ', 0xE7: 'ç', 0xE8: 'è', 0xE9: 'é', 0xEA: 'ê',
	0xEB: 'ë', 0xEC: 'ì', 0xED: 'í', 0xEE: 'î', 0xEF: 'ï',
	0xF0: 'ð', 0xF1: 'ñ', 0xF2: 'ò', 0xF3: 'ó', 0xF4: 'ô',
	0xF5: 'õ', 0xF6: 'ö', 0xF7: '÷', 0xF8: 'ø', 0xF9: 'ù',
	0xFA: 'ú', 0xFB: 'û', 0xFC: 'ü', 0xFD: 'ý', 0xFE: 'þ',
	0xFF: 'ÿ',
}

// macRomanHigh maps the non-ASCII MacRoman code points (0x80–0xFF).
var macRomanHigh = map[byte]rune{
	0x80: 'Ä', 0x81: 'Å', 0x82: 'Ç', 0x83: 'É', 0x84: 'Ñ',
	0x85: 'Ö', 0x86: 'Ü', 0x87: 'á', 0x88: 'à', 0x89: 'â',
	0x8A: 'ä', 0x8B: 'ã', 0x8C: 'å', 0x8D: 'ç', 0x8E: 'é',
	0x8F: 'è', 0x90: 'ê', 0x91: 'ë', 0x92: 'í', 0x93: 'ì',
	0x94: 'î', 0x95: 'ï', 0x96: 'ñ', 0x97: 'ó', 0x98: 'ò',
	0x99: 'ô', 0x9A: 'ö', 0x9B: 'õ', 0x9C: 'ú', 0x9D: 'ù',
	0x9E: 'û', 0x9F: 'ü', 0xA0: '†', 0xA1: '°', 0xA2: '¢',
	0xA3: '£', 0xA4: '§', 0xA5: '•', 0xA6: '¶', 0xA7: 'ß',
	0xA8: '®', 0xA9: '©', 0xAA: '™', 0xAB: '´', 0xAC: '¨',
	0xAE: 'Æ', 0xAF: 'Ø', 0xB1: '±', 0xB4: '¥', 0xB5: 'µ',
	0xBB: 'ª', 0xBC: 'º', 0xBE: 'æ', 0xBF: 'ø', 0xC0: '¿',
	0xC1: '¡', 0xC2: '¬', 0xC4: 'ƒ', 0xC7: '«', 0xC8: '»',
	0xC9: '…', 0xCA: ' ', 0xCB: 'À', 0xCC: 'Ã', 0xCD: 'Õ',
	0xCE: 'Œ', 0xCF: 'œ', 0xD0: '–', 0xD1: '—', 0xD2: '“',
	0xD3: '”', 0xD4: '‘', 0xD5: '’', 0xD6: '÷', 0xD8: 'ÿ',
	0xD9: 'Ÿ', 0xDA: '⁄', 0xDB: '¤', 0xDC: '‹', 0xDD: '›',
	0xDE: 'ﬁ', 0xDF: 'ﬂ', 0xE0: '‡', 0xE1: '·', 0xE2: '‚',
	0xE3: '„', 0xE4: '‰', 0xE5: 'Â', 0xE6: 'Ê', 0xE7: 'Á',
	0xE8: 'Ë', 0xE9: 'È', 0xEA: 'Í', 0xEB: 'Î', 0xEC: 'Ï',
	0xED: 'Ì', 0xEE: 'Ó', 0xEF: 'Ô', 0xF1: 'Ò', 0xF2: 'Ú',
	0xF3: 'Û', 0xF4: 'Ù', 0xF5: 'ı', 0xF6: 'ˆ', 0xF7: '˜',
	0xF8: '¯', 0xF9: '˘', 0xFA: '˙', 0xFB: '˚', 0xFC: '¸',
	0xFD: '˝', 0xFE: '˛', 0xFF: 'ˇ',
}

// standardLow holds the two ASCII-range positions where Adobe StandardEncoding
// differs from ASCII: 0x27 is quoteright and 0x60 is quoteleft.
var standardLow = map[byte]rune{
	0x27: '’', // quoteright
	0x60: '‘', // quoteleft
}

// standardHigh maps the non-ASCII Adobe StandardEncoding code points.
var standardHigh = map[byte]rune{
	0xA1: '¡', 0xA2: '¢', 0xA3: '£', 0xA4: '⁄', 0xA5: '¥',
	0xA6: 'ƒ', 0xA7: '§', 0xA8: '¤', 0xA9: '\'', 0xAA: '“',
	0xAB: '«', 0xAC: '‹', 0xAD: '›', 0xAE: 'ﬁ', 0xAF: 'ﬂ',
	0xB1: '–', 0xB2: '†', 0xB3: '‡', 0xB4: '·', 0xB6: '¶',
	0xB7: '•', 0xB8: '‚', 0xB9: '„', 0xBA: '”', 0xBB: '»',
	0xBC: '…', 0xBD: '‰', 0xBF: '¿', 0xC1: '`', 0xC2: '´',
	0xC3: 'ˆ', 0xC4: '˜', 0xC5: '¯', 0xC6: '˘', 0xC7: '˙',
	0xC8: '¨', 0xCA: '˚', 0xCB: '¸', 0xCD: '˝', 0xCE: '˛',
	0xCF: 'ˇ', 0xD0: '—', 0xE1: 'Æ', 0xE3: 'ª', 0xE8: 'Ł',
	0xE9: 'Ø', 0xEA: 'Œ', 0xEB: 'º', 0xF1: 'æ', 0xF5: 'ı',
	0xF8: 'ł', 0xF9: 'ø', 0xFA: 'œ', 0xFB: 'ß',
}

// glyphNames maps the standard PostScript glyph names used by the base
// encodings (and common /Differences entries) to runes. This is the
// base-encoding name set, not the full Adobe Glyph List.
var glyphNames = map[string]rune{
	"space": ' ', "exclam": '!', "quotedbl": '"', "numbersign": '#', "dollar": '$',
	"percent": '%', "ampersand": '&', "quotesingle": '\'', "parenleft": '(',
	"parenright": ')', "asterisk": '*', "plus": '+', "comma": ',', "hyphen": '-',
	"period": '.', "slash": '/', "zero": '0', "one": '1', "two": '2', "three": '3',
	"four": '4', "five": '5', "six": '6', "seven": '7', "eight": '8', "nine": '9',
	"colon": ':', "semicolon": ';', "less": '<', "equal": '=', "greater": '>',
	"question": '?', "at": '@',
	"A": 'A', "B": 'B', "C": 'C', "D": 'D', "E": 'E', "F": 'F', "G": 'G', "H": 'H',
	"I": 'I', "J": 'J', "K": 'K', "L": 'L', "M": 'M', "N": 'N', "O": 'O', "P": 'P',
	"Q": 'Q', "R": 'R', "S": 'S', "T": 'T', "U": 'U', "V": 'V', "W": 'W', "X": 'X',
	"Y": 'Y', "Z": 'Z',
	"bracketleft": '[', "backslash": '\\', "bracketright": ']', "asciicircum": '^',
	"underscore": '_', "grave": '`',
	"a": 'a', "b": 'b', "c": 'c', "d": 'd', "e": 'e', "f": 'f', "g": 'g', "h": 'h',
	"i": 'i', "j": 'j', "k": 'k', "l": 'l', "m": 'm', "n": 'n', "o": 'o', "p": 'p',
	"q": 'q', "r": 'r', "s": 's', "t": 't', "u": 'u', "v": 'v', "w": 'w', "x": 'x',
	"y": 'y', "z": 'z',
	"braceleft": '{', "bar": '|', "braceright": '}', "asciitilde": '~',
	// Common Latin-1 / typographic names.
	"exclamdown": '¡', "cent": '¢', "sterling": '£', "fraction": '⁄',
	"yen": '¥', "florin": 'ƒ', "section": '§', "currency": '¤',
	"quotedblleft": '“', "guillemotleft": '«', "guilsinglleft": '‹',
	"guilsinglright": '›', "fi": 'ﬁ', "fl": 'ﬂ', "endash": '–',
	"dagger": '†', "daggerdbl": '‡', "periodcentered": '·',
	"paragraph": '¶', "bullet": '•', "quotesinglbase": '‚',
	"quotedblbase": '„', "quotedblright": '”', "guillemotright": '»',
	"ellipsis": '…', "perthousand": '‰', "questiondown": '¿',
	"acute": '´', "circumflex": 'ˆ', "tilde": '˜', "macron": '¯',
	"breve": '˘', "dotaccent": '˙', "dieresis": '¨', "ring": '˚',
	"cedilla": '¸', "hungarumlaut": '˝', "ogonek": '˛', "caron": 'ˇ',
	"emdash": '—', "AE": 'Æ', "ordfeminine": 'ª', "Lslash": 'Ł',
	"Oslash": 'Ø', "OE": 'Œ', "ordmasculine": 'º', "ae": 'æ',
	"dotlessi": 'ı', "lslash": 'ł', "oslash": 'ø', "oe": 'œ',
	"germandbls": 'ß', "trademark": '™', "Euro": '€',
	"copyright": '©', "registered": '®', "degree": '°',
	"plusminus": '±', "twosuperior": '²', "threesuperior": '³',
	"mu": 'µ', "onesuperior": '¹', "onequarter": '¼',
	"onehalf": '½', "threequarters": '¾', "multiply": '×',
	"divide": '÷', "brokenbar": '¦', "logicalnot": '¬',
	"minus": '−', "nbspace": ' ',
}
