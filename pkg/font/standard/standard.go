// Package standard bundles permissively-licensed substitute fonts for the
// PDF standard-14 base fonts (and common aliases) so that simple fonts which
// declare a standard /BaseFont but embed no font program can still be rendered.
//
// # Bundled faces and licensing
//
// All shipped fonts are permissively licensed and MIT-compatible:
//
//   - TeX Gyre Heros (Helvetica/Arial-like sans) and TeX Gyre Termes
//     (Times-like serif), classic Type 1 PFB programs from the GUST e-foundry,
//     under the GUST Font License — an instance of the LaTeX Project Public
//     License (LPPL): free use, modification, and redistribution (renaming only
//     *requested*, not required, on modification). See fonts/GUST-FONT-LICENSE.txt.
//   - Inconsolata (monospace, Courier-like), SIL Open Font License 1.1 (OFL) —
//     free use, modification, and redistribution. See fonts/LICENSE-Inconsolata.txt.
//
// None are GPL/AGPL. All may be embedded and shipped inside this library and any
// binary built from it.
//
// # Coverage
//
// Helvetica/Arial -> Heros; Times/TimesNewRoman -> Termes; Courier/CourierNew ->
// Inconsolata. Only regular weights are bundled, so bold/italic/oblique variants
// of a family currently map to that family's regular face — an intentional
// approximation; true weight/slant substitutes are a follow-up. The Symbol and
// ZapfDingbats fonts have no permissively-licensed look-alike bundled here, so
// they are reported as unavailable and the caller skips them.
package standard

import (
	_ "embed"
	"strings"
)

//go:embed fonts/TeXGyreHeros-Regular.pfb
var herosPFB []byte

//go:embed fonts/TeXGyreTermes-Regular.pfb
var termesPFB []byte

//go:embed fonts/Inconsolata-Regular.ttf
var inconsolataTTF []byte

// Kind identifies the on-disk font-program format of a bundled substitute so the
// caller can hand it to the matching parser (classic Type1 PFB vs. TrueType).
type Kind int

const (
	// KindType1 is a classic Type 1 (PostScript, eexec) program in PFB form,
	// parsed the same way as a PDF /FontFile.
	KindType1 Kind = iota
	// KindTrueType is an SFNT/TrueType program, parsed like a PDF /FontFile2.
	KindTrueType
)

// Substitute is a bundled replacement face for a standard font: its embedded
// program bytes and the format those bytes are in.
type Substitute struct {
	// Name is the bundled face's identifier, useful for logging which substitute
	// was chosen (e.g. "TeXGyreHeros-Regular").
	Name string
	// Data is the raw embedded font program.
	Data []byte
	// Kind is the program format of Data.
	Kind Kind
}

// The three bundled faces. Bold/italic variants of each family map here too
// (regular-weight approximation; see package doc).
var (
	heros  = Substitute{Name: "TeXGyreHeros-Regular", Data: herosPFB, Kind: KindType1}
	termes = Substitute{Name: "TeXGyreTermes-Regular", Data: termesPFB, Kind: KindType1}
	mono   = Substitute{Name: "Inconsolata-Regular", Data: inconsolataTTF, Kind: KindTrueType}
)

// Lookup returns the bundled substitute face for a font family name, resolving
// the 14 PDF standard names and common aliases (Arial->Helvetica, CourierNew->
// Courier, TimesNewRoman->Times) as well as the default Office families used by
// DOCX and other reflowable documents (Calibri/Segoe UI->Heros sans,
// Cambria/Georgia->Termes serif, Consolas->Inconsolata monospace). It also resolves
// the generic CSS family keywords (serif->Termes, sans-serif->Heros,
// monospace->Inconsolata, cursive/fantasy->Termes), which is the family a CSS reflow
// frontend computes when no concrete family is named. It strips a subset prefix
// ("ABCDEF+Name") and is case-insensitive. ok is false for families with no bundled
// substitute — notably Symbol, ZapfDingbats, and Wingdings, and any unrecognized
// name.
func Lookup(baseFont string) (Substitute, bool) {
	name := canonical(baseFont)
	// Generic CSS family keywords (the default a reflow frontend computes when no
	// concrete family is named) map to the matching substitute style: serif->Termes,
	// sans-serif->Heros, monospace->Inconsolata, with cursive/fantasy falling back to
	// serif. These are matched exactly (not by prefix), before the named-family
	// aliases, so a concrete family is never shadowed by a generic keyword.
	switch name {
	case "serif", "cursive", "fantasy":
		return termes, true
	case "sans-serif", "system-ui", "ui-sans-serif":
		return heros, true
	case "monospace", "ui-monospace":
		return mono, true
	}
	switch {
	case strings.HasPrefix(name, "courier"),
		strings.HasPrefix(name, "consolas"),
		strings.HasPrefix(name, "inconsolata"):
		return mono, true
	case strings.HasPrefix(name, "times"),
		strings.HasPrefix(name, "cambria"),
		strings.HasPrefix(name, "georgia"),
		strings.HasPrefix(name, "texgyretermes"): // the bundled serif's own name
		return termes, true
	case strings.HasPrefix(name, "helvetica"),
		strings.HasPrefix(name, "arial"),
		strings.HasPrefix(name, "calibri"),
		strings.HasPrefix(name, "segoeui"),
		strings.HasPrefix(name, "verdana"),
		strings.HasPrefix(name, "texgyreheros"): // the bundled sans's own name
		return heros, true
	default:
		return Substitute{}, false
	}
}

// canonical lowercases baseFont and removes a subset tag ("ABCDEF+") and any
// spaces so "Arial", "ArialMT", "Helvetica-Bold", "TimesNewRomanPSMT" and a
// subsetted "ABCDEF+Helvetica" all reduce to a comparable form.
func canonical(baseFont string) string {
	s := baseFont
	if i := strings.IndexByte(s, '+'); i >= 0 && i == 6 {
		s = s[i+1:]
	}
	s = strings.ReplaceAll(s, " ", "")
	return strings.ToLower(s)
}
