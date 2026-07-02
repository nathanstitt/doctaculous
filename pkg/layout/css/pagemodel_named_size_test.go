package css

import (
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
)

// TestNamedPageSizeHonoredUnderExplicitSize pins the fix: with an explicit
// WithPageSize (ExplicitSize=true), the DEFAULT (unnamed) page uses the API size,
// but a NAMED page a section opted into keeps its own @page size (e.g. landscape).
func TestNamedPageSizeHonoredUnderExplicitSize(t *testing.T) {
	sheet := gcss.Parse(`@page landscape { size: 1056px 816px }`)
	cfg := PagedConfig{
		FallbackW:    816,
		FallbackH:    1056,
		ExplicitSize: true, // WithPageSize was used
		Pages:        gcss.Stylesheet{Pages: sheet.Pages},
	}

	// The unnamed page keeps the API/fallback size (portrait 816x1056).
	def := cfg.resolvePageGeom(0, "", false)
	if def.pageW != 816 || def.pageH != 1056 {
		t.Errorf("default page = %vx%v; want 816x1056 (API size wins)", def.pageW, def.pageH)
	}

	// The named landscape page uses ITS @page size (wide 1056x816), even though
	// ExplicitSize is set.
	land := cfg.resolvePageGeom(0, "landscape", false)
	if land.pageW != 1056 || land.pageH != 816 {
		t.Errorf("landscape page = %vx%v; want 1056x816 (named @page size wins)", land.pageW, land.pageH)
	}
}

// TestNamedPageInheritsUnnamedSizeUnderExplicitSize pins the intended edge case: the
// CSS Paged Media cascade folds an UNNAMED @page rule's declarations into a NAMED-page
// lookup (page.go matchingPageRules matches a rule whose name is "" OR equals the
// requested name). So a named page that declares NO size of its own still resolves
// HasSize=true carrying the unnamed @page size, and the named-page gate applies it even
// under ExplicitSize. This is cascade-consistent (named pages inherit unnamed
// declarations) and INTENDED — the default (unnamed) page still keeps the API size.
func TestNamedPageInheritsUnnamedSizeUnderExplicitSize(t *testing.T) {
	// Unnamed @page declares a distinctive size (1200x400); the named page declares
	// only a margin, no size of its own.
	sheet := gcss.Parse(`@page { size: 1200px 400px } @page name { margin: 1in }`)
	cfg := PagedConfig{
		FallbackW:    816,
		FallbackH:    1056,
		ExplicitSize: true, // WithPageSize was used
		Pages:        gcss.Stylesheet{Pages: sheet.Pages},
	}

	// The default (unnamed) page keeps the API/fallback size — the explicit API size
	// pins the unnamed page's size.
	def := cfg.resolvePageGeom(0, "", false)
	if def.pageW != 816 || def.pageH != 1056 {
		t.Errorf("default page = %vx%v; want 816x1056 (API size wins for the unnamed page)", def.pageW, def.pageH)
	}

	// The named page — which declares no size — INHERITS the unnamed @page size (1200x400)
	// through the cascade, and the named-page gate applies it even under ExplicitSize.
	named := cfg.resolvePageGeom(0, "name", false)
	if named.pageW != 1200 || named.pageH != 400 {
		t.Errorf("named page = %vx%v; want 1200x400 (inherited unnamed @page size under ExplicitSize)", named.pageW, named.pageH)
	}
}
