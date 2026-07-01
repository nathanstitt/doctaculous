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
