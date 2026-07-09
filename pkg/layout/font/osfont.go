package font

import (
	"os"
	"strings"
	"sync"

	"github.com/adrg/sysfont"

	pkgfont "github.com/nathanstitt/doctaculous/pkg/font"
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
// ok=false when the host has no usable match (sysfont returns nil — a bare/headless
// machine) or the matched file cannot be read; the caller then falls through to the
// bundled substitute. It never panics.
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
	return b, true
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
