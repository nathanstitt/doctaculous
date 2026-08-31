package font

import (
	"os"
	"strings"
	"sync"

	"github.com/adrg/sysfont"

	pkgfont "github.com/nathanstitt/omnidoc/pkg/internal/font"
)

// OSFontProvider implements pkgfont.Provider.
var _ pkgfont.Provider = (*OSFontProvider)(nil)

// OSFontProvider also satisfies the local()-lookup interface so it can be installed as
// the FaceCache's sys provider (its LoadLocal is a deliberate no-op).
var _ SystemFontProvider = (*OSFontProvider)(nil)

// OSFontProvider resolves a family+style to an installed OS font via adrg/sysfont,
// which live-scans the platform's standard font directories (macOS, Linux, and Windows
// font folders via adrg/xdg). It is the default, opt-in-by-mode source of non-embedded
// faces: constructing one trades hermetic reproducibility for host-font fidelity.
//
// The directory scan is expensive, so it runs once, lazily, on first LoadStyled and is
// cached; the provider is safe for concurrent LoadStyled calls (a parsed document is
// shared read-only across the page fan-out). sysfont's default extensions are
// .ttf/.ttc/.otf, so the returned bytes are always sfnt-family (never classic Type1),
// which the reflow decode path (LoadSFNT) and the PDF substitute path both accept.
type OSFontProvider struct {
	once   sync.Once
	finder *sysfont.Finder
	logf   func(string, ...any)
}

// NewOSFontProvider returns a provider that resolves installed OS fonts. The
// font-directory scan is deferred to first use.
func NewOSFontProvider() *OSFontProvider { return &OSFontProvider{} }

// NewOSFontProviderWithLogf is NewOSFontProvider with a diagnostics logger (may be nil).
func NewOSFontProviderWithLogf(logf func(string, ...any)) *OSFontProvider {
	return &OSFontProvider{logf: logf}
}

func (p *OSFontProvider) log(format string, args ...any) {
	if p.logf != nil {
		p.logf(format, args...)
	}
}

// LoadStyled resolves family+style to an installed font's raw bytes. It returns
// ok=false when the host has no usable match, the matched file cannot be read, or the
// matched file turns out to be a DIFFERENT family than the one requested; the caller
// then falls through to the bundled substitute. It never panics.
//
// The identity check is not defensive programming — it is load-bearing. sysfont.Match
// never reports a miss: when nothing matches it falls through to findAlternative and
// returns a "suitable default", so a request for a family the host does not have comes
// back as some unrelated installed font. Measured on a stock macOS box, a request for
// the nonexistent family "ZZZZ Totally Fake 12345" returned Arial Unicode MS, the same
// bytes returned for "Roboto" and "IBM Plex Mono"; "DejaVu Sans" returned Lucida
// Grande. Without this check the ok=false path below is unreachable, the bundled
// fallback it guards can never run, and every unresolvable family silently renders in
// the wrong typeface.
func (p *OSFontProvider) LoadStyled(family string, bold, italic bool) (data []byte, ok bool) {
	p.once.Do(func() { p.finder = sysfont.NewFinder(nil) })
	if p.finder == nil {
		return nil, false
	}
	match := p.finder.Match(styleQuery(family, bold, italic))
	if match == nil || match.Filename == "" {
		return nil, false
	}
	b, err := os.ReadFile(match.Filename)
	if err != nil {
		p.log("osfont: read %q for %q: %v", match.Filename, family, err)
		return nil, false
	}
	if !p.declaresFamily(b, family, match.Filename) {
		return nil, false
	}
	return b, true
}

// declaresFamily reports whether the font program in b declares itself to be the
// requested family, so a fuzzy match that landed on an unrelated face is rejected.
//
// A face whose name table cannot be read is ACCEPTED rather than rejected: some valid
// programs (classic Type1, an sfnt with no name table) carry no declared family, and
// refusing those would discard fonts that do work. The check exists to catch a
// confidently-wrong answer, not to demand proof of identity from every file.
func (p *OSFontProvider) declaresFamily(b []byte, family, filename string) bool {
	face, err := pkgfont.LoadSFNT(b)
	if err != nil {
		// Not decodable here; let the caller's own decode report the real error.
		return true
	}
	declared, ok := face.FamilyName()
	if !ok {
		return true
	}
	if familyMatches(declared, family) {
		return true
	}
	p.log("osfont: %q resolved to %q (%s); rejecting mismatch, falling back", family, declared, filename)
	return false
}

// familyMatches reports whether a font's declared family satisfies a request for
// want. The comparison is case- and separator-insensitive, so "IBM Plex Mono" matches
// a file declaring "IBMPlexMono".
//
// A declared name may EXTEND the request, but only with STYLE words: a per-weight file
// declaring "Barlow Condensed SemiBold" satisfies a request for "Barlow Condensed",
// because styleQuery carries the weight separately and some foundries (Google's
// per-weight TTFs among them) put the style into the family name. The remainder is
// checked against a known style vocabulary rather than accepted wholesale, because a
// bare prefix test also accepts a genuinely DIFFERENT family that happens to share a
// prefix — "Arial Black" would satisfy "Arial", and "Times New Roman" would satisfy
// "Times". Those are distinct typefaces, and silently accepting them is the same class
// of wrong-font bug this check exists to prevent.
func familyMatches(declared, want string) bool {
	d, w := normalizeFamilyName(declared), normalizeFamilyName(want)
	if d == "" || w == "" {
		return false
	}
	if d == w {
		return true
	}
	rest, ok := strings.CutPrefix(d, w)
	if !ok {
		return false
	}
	// The declared name extends the request; accept only if what follows is entirely
	// style vocabulary (normalization has already removed the separators).
	for rest != "" {
		matched := false
		for _, word := range styleWords {
			if after, ok := strings.CutPrefix(rest, word); ok {
				rest, matched = after, true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// styleWords are the weight/width/slant tokens a font may append to its family name.
// Longer entries precede their own prefixes ("semibold" before "bold", "extralight"
// before "light") so the greedy scan in familyMatches consumes the whole token.
var styleWords = []string{
	"ultracondensed", "extracondensed", "semicondensed", "condensed",
	"ultraexpanded", "extraexpanded", "semiexpanded", "expanded",
	"extralight", "ultralight", "semilight", "light",
	"extrabold", "ultrabold", "semibold", "demibold", "bold",
	"extrablack", "ultrablack", "black", "heavy",
	"medium", "regular", "normal", "book", "roman",
	"thin", "hairline",
	"italic", "oblique", "slanted",
	"display", "text", "caption",
}

// normalizeFamilyName lowercases and strips spaces, hyphens, and underscores so the
// many spellings of one family compare equal.
func normalizeFamilyName(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch r {
		case ' ', '-', '_':
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// LoadLocal implements SystemFontProvider so an OSFontProvider can be installed as the
// FaceCache's provider. @font-face local() is an exact-name lookup that sysfont's
// family/style matcher does not serve, so this always reports a miss; family+style
// resolution goes through LoadStyled (the pkgfont.Provider route) instead.
func (p *OSFontProvider) LoadLocal(string) ([]byte, bool) { return nil, false }

// styleQuery builds the query string sysfont's fuzzy matcher expects: the family name
// followed by the style words, e.g. "Arial Bold", "Times Bold Italic".
func styleQuery(family string, bold, italic bool) string {
	var sb strings.Builder
	sb.WriteString(strings.TrimSpace(family))
	if bold {
		sb.WriteString(" Bold")
	}
	if italic {
		sb.WriteString(" Italic")
	}
	return sb.String()
}
