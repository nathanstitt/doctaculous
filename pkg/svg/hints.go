package svg

import (
	"github.com/nathanstitt/doctaculous/pkg/css"
)

// svgPresentationAttrs is the set of SVG presentation attributes that can
// become CSS declarations in the cascade. This list must be kept in sync with
// the properties handled in Style.apply() in pkg/svg/style.go — every
// applyXxx call in apply() must have its corresponding attribute here.
// If a future task adds a property to the appliers but not here, that property
// would silently stop being settable from a presentation attribute. The order
// here matches the order in apply() for deterministic output.
//
// stop-color/stop-opacity are not consumed by Style.apply (they only apply to
// <stop> elements, resolved by parseStops in stops.go), but they go through
// the same presentation-hint mechanism so a <stop stop-color="..."> attribute
// and a `stop { stop-color: ... }` sheet rule rank the same as every other
// presentation property.
//
// clip-path is likewise not consumed by Style.apply (it is resolved during
// Parse against the document index — see resolveClipPath — because Style
// values are cascade-only and never see docIndex), but it goes through this
// same hint mechanism so a clip-path="url(#...)" attribute and a `{
// clip-path: url(#...) }` sheet rule rank identically; the scene builder
// reads the cascaded value straight off ctx.resolve, exactly like a
// fill/stroke url() reference. clip-rule IS consumed by Style.apply
// (applyClipRule), since — unlike clip-path itself — it is an ordinary
// inherited enum property with no document-index dependency.
//
// mask is resolved out-of-band exactly like clip-path (see resolveMask),
// for the same document-index reason. mask-type IS consumed by Style.apply
// (applyMaskType): it is an ordinary non-inherited enum property with no
// document-index dependency, but it must still be listed here (not just in
// apply()) so `style="mask-type:alpha"` and a `mask-type` sheet rule both
// reach it — see the resvg mask-type-in-style fixture, which this hint
// entry is what makes pass.
var svgPresentationAttrs = []string{
	"color",
	"fill",
	"fill-opacity",
	"fill-rule",
	"stroke",
	"stroke-opacity",
	"stroke-width",
	"stroke-linecap",
	"stroke-linejoin",
	"stroke-miterlimit",
	"stroke-dasharray",
	"stroke-dashoffset",
	"opacity",
	"display",
	"visibility",
	"stop-color",
	"stop-opacity",
	"clip-path",
	"clip-rule",
	"mask",
	"mask-type",
}

// svgPresentationHints maps an element's SVG presentation attributes to CSS
// declarations so they participate in the cascade at OriginPresentationalHint
// (above UA, below author), just as presentationalHints does for HTML.
// An element with no recognized presentation attribute yields nil — the common
// case is allocation-free. The declarations are in the string form the cascade
// accepts; the property and value are verbatim from the attributes (parsing
// stays in the existing style appliers), and every hint has Important: false.
func svgPresentationHints(el *element) []css.Declaration {
	if el == nil {
		return nil
	}

	var ds []css.Declaration
	for _, attr := range svgPresentationAttrs {
		if v, ok := el.attrs[attr]; ok {
			ds = append(ds, css.Declaration{
				Property:  attr,
				Value:     v,
				Important: false,
			})
		}
	}
	return ds
}
