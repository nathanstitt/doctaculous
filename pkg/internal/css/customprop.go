package css

import "strings"

// CustomProps holds an element's computed custom properties (CSS Variables 1),
// keyed by the full property name INCLUDING the leading "--".
//
// Custom properties differ from every other entry in ComputedStyle in three ways
// that shape this type:
//
//   - They are INHERITED, so a child sees its parent's map.
//   - They cascade as UNPARSED token streams. `--x: 1px solid` is not a length,
//     a colour, or anything else until it is substituted into a real property,
//     so the value is kept as raw text (the same treatment ComputedStyle.Filter
//     already gets) rather than being parsed at declaration time.
//   - Their names are CASE-SENSITIVE, unlike normal CSS property names. `--Foo`
//     and `--foo` are two different properties.
//
// The zero value is an empty, usable map-less set: a document that declares no
// custom property allocates nothing, so the common case stays free.
//
// Sharing is copy-on-write. inheritFrom hands the parent's map to the child by
// reference and set() clones before the first mutation, so a deep tree where only
// :root declares variables holds ONE map rather than one per element.
type CustomProps struct {
	m map[string]string
}

// Len reports how many custom properties are visible on this element.
func (c CustomProps) Len() int { return len(c.m) }

// Get returns the raw, unsubstituted token stream declared for name (which must
// include the leading "--"), and whether it was declared at all. The distinction
// matters: a property declared as the empty string is valid and resolves to
// nothing, whereas an undeclared one triggers var()'s fallback.
func (c CustomProps) Get(name string) (string, bool) {
	v, ok := c.m[name]
	return v, ok
}

// set records name=value, cloning the underlying map first so that a map shared
// with an ancestor (see inheritFrom) is never mutated in place.
func (c *CustomProps) set(name, value string) {
	cloned := make(map[string]string, len(c.m)+1)
	for k, v := range c.m {
		cloned[k] = v
	}
	cloned[name] = value
	c.m = cloned
}

// IsCustomProperty reports whether a declaration name is a custom property.
// Per CSS Variables 1 §2 the name is everything after a literal "--" prefix,
// and a bare "--" (empty name) is itself a valid custom property.
func IsCustomProperty(prop string) bool {
	return strings.HasPrefix(prop, "--")
}
