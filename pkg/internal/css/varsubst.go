package css

import "strings"

// maxVarSubstitutionDepth bounds how deep var() substitution may recurse.
//
// A cyclic reference (`--a: var(--b); --b: var(--a)`) is required by CSS
// Variables 1 §3 to make every property in the cycle invalid at computed-value
// time, and the cycle detector below catches that case exactly. This limit is a
// second, cruder backstop for the case the detector cannot see: a NON-cyclic
// chain that still expands without practical bound, because each level
// substitutes more var() references than it consumes
// (`--a: var(--b) var(--b); --b: var(--c) var(--c); ...`). That is exponential in
// the number of levels, not infinite, so it terminates — but not before
// exhausting memory on a hostile stylesheet. Depth is the cheap thing to bound.
const maxVarSubstitutionDepth = 32

// substituteVars replaces every var() reference in value with its resolved
// substitution value, per CSS Variables 1 §3.
//
// The second return value is the "valid at computed-value time" flag. CSS gives
// var() a failure mode with no analogue elsewhere in this engine: a declaration
// whose var() cannot be resolved is NOT simply dropped (which would leave the
// previous cascaded value showing) and does NOT fall back to the initial value
// at parse time. It becomes "invalid at computed-value time", which the caller
// must treat as unset/inherit — see applyDeclaration. Returning ok=false rather
// than a best-effort string is what lets the caller honour that rule.
//
// Resolution follows the spec:
//   - var(--x) substitutes --x's value if --x is declared.
//   - var(--x, fallback) substitutes the fallback if --x is NOT declared. The
//     fallback is itself a token stream that may contain further var()s.
//   - A declared-but-empty custom property substitutes nothing and is a SUCCESS;
//     the fallback is not used. This is why CustomProps.Get reports declared-ness
//     separately from the value.
//   - An undeclared property with no fallback is invalid at computed-value time.
//   - A cyclic reference is invalid at computed-value time.
func substituteVars(value string, props CustomProps) (string, bool) {
	if !containsVar(value) {
		// Overwhelmingly the common case: no var() anywhere, so no work and no
		// allocation. Keeps every existing stylesheet byte-identical.
		return value, true
	}
	return substituteVarsDepth(value, props, make(map[string]bool), 0)
}

// containsVar reports whether value contains a var() reference, matching the
// function name case-insensitively (CSS function names are ASCII
// case-insensitive, so VAR(--x) is legal).
//
// This is a fast pre-filter, not a parser: a false positive (the text "var("
// inside a quoted string) only costs a trip through the real substituter, which
// then finds no function to expand and returns the input unchanged.
func containsVar(value string) bool {
	for i := 0; i+4 <= len(value); i++ {
		if (value[i] == 'v' || value[i] == 'V') &&
			(value[i+1] == 'a' || value[i+1] == 'A') &&
			(value[i+2] == 'r' || value[i+2] == 'R') &&
			value[i+3] == '(' {
			return true
		}
	}
	return false
}

// substituteVarsDepth is substituteVars' recursive worker. active holds the
// custom-property names currently being expanded on this path, which is what
// makes cycle detection exact rather than depth-based.
func substituteVarsDepth(value string, props CustomProps, active map[string]bool, depth int) (string, bool) {
	if depth > maxVarSubstitutionDepth {
		return "", false
	}

	var b strings.Builder
	i := 0
	for {
		start := indexVarFunc(value[i:])
		if start < 0 {
			b.WriteString(value[i:])
			return b.String(), true
		}
		start += i
		b.WriteString(value[i:start])

		// Find the matching ")" for this var(, honouring nesting and strings so
		// that var(--a, var(--b)) and var(--a, "),") both terminate correctly.
		open := start + len("var(")
		end, ok := matchParen(value, open)
		if !ok {
			// Unterminated var( — the declaration is malformed, not merely
			// unresolvable. Treat it as invalid at computed-value time so it is
			// handled the same as any other var() failure.
			return "", false
		}

		substituted, ok := resolveVarRef(value[open:end], props, active, depth)
		if !ok {
			return "", false
		}
		b.WriteString(substituted)
		i = end + 1 // skip past ")"
	}
}

// resolveVarRef resolves the inside of a single var(...) — the argument text
// between the parentheses — to its substitution value.
func resolveVarRef(args string, props CustomProps, active map[string]bool, depth int) (string, bool) {
	name, fallback, hasFallback := splitVarArgs(args)
	// Custom property names are case-sensitive, so name is NOT lowercased here.
	name = strings.TrimSpace(name)
	if !IsCustomProperty(name) {
		// var(foo) / var() — not a custom property reference at all. Invalid.
		return "", false
	}

	if active[name] {
		// Cyclic reference: --a is already being expanded further up this path.
		// Per CSS Variables 1 §3 every property in the cycle is invalid at
		// computed-value time.
		return "", false
	}

	if raw, declared := props.Get(name); declared {
		if raw == "" {
			// Declared as empty: substitutes nothing, and this is a SUCCESS —
			// the fallback is deliberately not consulted.
			return "", true
		}
		active[name] = true
		out, ok := substituteVarsDepth(raw, props, active, depth+1)
		delete(active, name)
		return out, ok
	}

	// Undeclared. The fallback, if present, is itself a token stream that may
	// contain further var() references. An EMPTY fallback is legal and valid
	// (var(--undeclared,) substitutes nothing), which is why presence is tracked
	// separately from content.
	if !hasFallback {
		return "", false
	}
	return substituteVarsDepth(fallback, props, active, depth+1)
}

// splitVarArgs splits a var() argument list into the custom-property name and
// the optional fallback, at the FIRST top-level comma. Commas nested inside
// parentheses or strings belong to the fallback, not to this split:
// var(--x, rgb(1, 2, 3)) has one fallback, not three arguments.
func splitVarArgs(args string) (name, fallback string, hasFallback bool) {
	depth := 0
	var quote byte
	for i := 0; i < len(args); i++ {
		c := args[i]
		if quote != 0 {
			i, quote = scanQuoted(i, c, quote)
			continue
		}
		switch {
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			return args[:i], strings.TrimSpace(args[i+1:]), true
		}
	}
	return args, "", false
}

// scanQuoted advances one byte inside a quoted string, returning the (possibly
// skipped-ahead) index and the still-open quote character, or 0 once the string
// closes. A backslash consumes the byte after it, so an escaped quote does not
// end the string.
//
// The three scanners in this file all need to step over strings identically —
// a var() argument may legally contain a quoted "(" or "," that must not be read
// as structure — so the rule lives here once rather than being restated at each
// loop.
func scanQuoted(i int, c, quote byte) (int, byte) {
	switch c {
	case '\\':
		return i + 1, quote // skip the escaped byte
	case quote:
		return i, 0 // string closed
	default:
		return i, quote
	}
}

// indexVarFunc returns the byte offset of the next "var(" in s that is not
// inside a quoted string, or -1. The function name is matched
// case-insensitively.
func indexVarFunc(s string) int {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			i, quote = scanQuoted(i, c, quote)
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if (c == 'v' || c == 'V') && i+4 <= len(s) &&
			(s[i+1] == 'a' || s[i+1] == 'A') &&
			(s[i+2] == 'r' || s[i+2] == 'R') &&
			s[i+3] == '(' {
			return i
		}
	}
	return -1
}

// matchParen returns the index of the ")" closing the "(" that opened just
// before position open, honouring nested parentheses and quoted strings.
func matchParen(s string, open int) (int, bool) {
	depth := 0
	var quote byte
	for i := open; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			i, quote = scanQuoted(i, c, quote)
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return i, true
			}
			depth--
		}
	}
	return 0, false
}
