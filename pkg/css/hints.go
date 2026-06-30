package css

import (
	"strconv"
	"strings"
)

// presentationalHints maps a node's legacy presentational attributes to CSS
// declarations (HTML §15 "presentational hints"). The returned declarations are in the
// string form applyDeclaration accepts, so they flow through the normal value parsers.
// They are injected into the cascade at OriginPresentationalHint (above UA, below
// author), so any author rule or inline style overrides them. An element with no
// recognized presentational attribute yields nil — the common case is allocation-free
// and byte-identical to having no hints.
//
// Some hints propagate from an ancestor: a table cell reads cellpadding/border/valign
// from its nearest ancestor <table>, and a hyperlink reads link= from its <body>. This
// is done by climbing n.Parent(), so the whole feature stays inside the per-node
// cascade with no box-tree changes.
func presentationalHints(n Node) []Declaration {
	var ds []Declaration
	tag := n.Tag()
	attr := func(k string) (string, bool) { return n.Attr(k) }

	// --- color ---
	switch tag {
	case "body":
		if v, ok := attr("text"); ok {
			ds = appendColor(ds, "color", v)
		}
		if v, ok := attr("bgcolor"); ok {
			ds = appendColor(ds, "background-color", v)
		}
		ds = appendBodyBackground(ds, n)
	case "table", "tr", "td", "th", "col", "colgroup", "tbody", "thead", "tfoot":
		if v, ok := attr("bgcolor"); ok {
			ds = appendColor(ds, "background-color", v)
		}
		if v, ok := attr("bordercolor"); ok {
			ds = appendColor(ds, "border-color", v)
		}
		if tag == "td" || tag == "th" {
			ds = appendBodyBackground(ds, n) // background="url" also valid on a cell
		}
	case "font":
		if v, ok := attr("color"); ok {
			ds = appendColor(ds, "color", v)
		}
		if v, ok := attr("face"); ok && strings.TrimSpace(v) != "" {
			ds = append(ds, Declaration{Property: "font-family", Value: v})
		}
		if v, ok := attr("size"); ok {
			if px, ok := legacyFontSizePx(v); ok {
				ds = append(ds, Declaration{Property: "font-size", Value: strconv.Itoa(px) + "px"})
			}
		}
	}

	// --- dimensions (table parts; <img> sizes via Replaced.Attrs, not here) ---
	switch tag {
	case "table", "td", "th", "col", "colgroup", "hr", "tr":
		if v, ok := attr("width"); ok {
			ds = appendLength(ds, "width", v)
		}
		if v, ok := attr("height"); ok {
			ds = appendLength(ds, "height", v)
		}
	}

	// --- alignment ---
	ds = appendAlign(ds, n, tag)
	ds = appendValign(ds, n, tag)

	// --- table chrome (cellspacing/cellpadding/border) ---
	ds = appendTableChrome(ds, n, tag)

	// --- image spacing + border ---
	if tag == "img" {
		if v, ok := attr("hspace"); ok {
			if px, ok := legacyPx(v); ok {
				ds = append(ds, Declaration{Property: "margin-left", Value: px}, Declaration{Property: "margin-right", Value: px})
			}
		}
		if v, ok := attr("vspace"); ok {
			if px, ok := legacyPx(v); ok {
				ds = append(ds, Declaration{Property: "margin-top", Value: px}, Declaration{Property: "margin-bottom", Value: px})
			}
		}
		if v, ok := attr("border"); ok {
			if px, ok := legacyPx(v); ok {
				ds = append(ds, Declaration{Property: "border-width", Value: px},
					Declaration{Property: "border-style", Value: "solid"})
			}
		}
	}

	// --- white-space (nowrap on cells) ---
	if tag == "td" || tag == "th" {
		if _, ok := attr("nowrap"); ok {
			ds = append(ds, Declaration{Property: "white-space", Value: "nowrap"})
		}
	}

	// --- lists ---
	ds = appendListHints(ds, n, tag)

	// --- link colors from <body link=...> ---
	ds = appendLinkColor(ds, n, tag)

	return ds
}

// appendColor adds a color declaration if v parses as a legacy color (a #hex, a bare
// hex, or a named color). An unparseable value adds nothing.
func appendColor(ds []Declaration, prop, v string) []Declaration {
	if c, ok := parseLegacyColor(v); ok {
		return append(ds, Declaration{Property: prop, Value: c})
	}
	return ds
}

// appendLength adds a width/height declaration. A bare number is px; a trailing % is a
// percentage. Anything else (e.g. "*") adds nothing.
func appendLength(ds []Declaration, prop, v string) []Declaration {
	if val, ok := legacyLength(v); ok {
		return append(ds, Declaration{Property: prop, Value: val})
	}
	return ds
}

// appendAlign maps the align attribute. On <img>/<table>, align=left|right is a float;
// elsewhere align is text-align. align=center on a table centers it via auto margins.
func appendAlign(ds []Declaration, n Node, tag string) []Declaration {
	v, ok := n.Attr("align")
	if !ok {
		return ds
	}
	a := strings.ToLower(strings.TrimSpace(v))
	if tag == "img" || tag == "table" {
		switch a {
		case "left", "right":
			return append(ds, Declaration{Property: "float", Value: a})
		case "center":
			if tag == "table" {
				return append(ds, Declaration{Property: "margin-left", Value: "auto"},
					Declaration{Property: "margin-right", Value: "auto"})
			}
		case "top", "middle", "bottom":
			// img align=top/middle/bottom is vertical-align (a table ignores these).
			if tag == "img" {
				return append(ds, Declaration{Property: "vertical-align", Value: a})
			}
		}
		return ds
	}
	switch a {
	case "left", "right", "center", "justify":
		return append(ds, Declaration{Property: "text-align", Value: a})
	}
	return ds
}

// appendValign maps valign on a cell to vertical-align; on a cell with no own valign it
// inherits the nearest ancestor row/section/table valign (valign is not CSS-inherited).
func appendValign(ds []Declaration, n Node, tag string) []Declaration {
	if tag != "td" && tag != "th" {
		// On row/section/col elements valign has no direct CSS effect (it propagates to
		// cells, handled when each cell resolves its own hints).
		return ds
	}
	v, ok := n.Attr("valign")
	if !ok {
		v, ok = ancestorAttr(n, "valign", "tr", "thead", "tbody", "tfoot", "table")
	}
	if !ok {
		return ds
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "top", "middle", "bottom", "baseline":
		return append(ds, Declaration{Property: "vertical-align", Value: strings.ToLower(strings.TrimSpace(v))})
	}
	return ds
}

// appendTableChrome maps cellspacing/cellpadding/border. cellspacing/border on the table
// apply to the table; cellpadding and the per-cell 1px border propagate to each cell.
func appendTableChrome(ds []Declaration, n Node, tag string) []Declaration {
	switch tag {
	case "table":
		if v, ok := n.Attr("cellspacing"); ok {
			if px, ok := legacyNumber(v); ok {
				ds = append(ds, Declaration{Property: "border-spacing", Value: strconv.Itoa(px) + "px"})
			}
		}
		if v, ok := n.Attr("border"); ok {
			if px, ok := legacyNumber(v); ok && px > 0 {
				ds = append(ds, Declaration{Property: "border-width", Value: strconv.Itoa(px) + "px"},
					Declaration{Property: "border-style", Value: "outset"})
			}
		}
	case "td", "th":
		// cellpadding → padding on every cell (read from the ancestor table).
		if v, ok := ancestorAttr(n, "cellpadding", "table"); ok {
			if px, ok := legacyNumber(v); ok {
				ds = append(ds, Declaration{Property: "padding", Value: strconv.Itoa(px) + "px"})
			}
		}
		// table border=N (N>0) → a 1px border on every cell.
		if v, ok := ancestorAttr(n, "border", "table"); ok {
			if px, ok := legacyNumber(v); ok && px > 0 {
				ds = append(ds, Declaration{Property: "border-width", Value: "1px"},
					Declaration{Property: "border-style", Value: "inset"})
			}
		}
	}
	return ds
}

// appendBodyBackground maps body/td background="url" to background-image.
func appendBodyBackground(ds []Declaration, n Node) []Declaration {
	if v, ok := n.Attr("background"); ok && strings.TrimSpace(v) != "" {
		return append(ds, Declaration{Property: "background-image", Value: "url(" + strings.TrimSpace(v) + ")"})
	}
	return ds
}

// appendListHints maps the legacy list attributes (type, start, value) onto the
// list-style-type and counter mechanisms.
func appendListHints(ds []Declaration, n Node, tag string) []Declaration {
	switch tag {
	case "ol", "ul":
		if v, ok := n.Attr("type"); ok {
			if t, ok := legacyListType(v, tag); ok {
				ds = append(ds, Declaration{Property: "list-style-type", Value: t})
			}
		}
		if v, ok := n.Attr("start"); ok {
			if start, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				// The UA resets list-item to 0 and each <li> increments by 1, so a start
				// of N means resetting the counter to N-1.
				ds = append(ds, Declaration{Property: "counter-reset", Value: "list-item " + strconv.Itoa(start-1)})
			}
		}
	case "li":
		if v, ok := n.Attr("type"); ok {
			if t, ok := legacyListType(v, "ol"); ok {
				ds = append(ds, Declaration{Property: "list-style-type", Value: t})
			}
		}
		if v, ok := n.Attr("value"); ok {
			if val, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				// counter-set the item's number directly: the counter walk applies the
				// implicit list-item increment (+1) BEFORE counter-set, so set overwrites
				// it — the marker reads exactly this value. (Unlike <ol start>, which uses
				// counter-reset, applied before the increment, hence its start-1 offset.)
				ds = append(ds, Declaration{Property: "counter-set", Value: "list-item " + strconv.Itoa(val)})
			}
		}
	}
	return ds
}

// appendLinkColor maps <body link=...> to the color of descendant hyperlinks: a linked
// <a> reads its ancestor body's link= attribute. vlink/alink are not modeled (no
// history / no interactivity), matching :visited's inert behavior.
func appendLinkColor(ds []Declaration, n Node, tag string) []Declaration {
	if tag != "a" {
		return ds
	}
	if href, ok := n.Attr("href"); !ok || href == "" {
		return ds // only an actual hyperlink takes the link color
	}
	if v, ok := ancestorAttr(n, "link", "body"); ok {
		ds = appendColor(ds, "color", v)
	}
	return ds
}

// ancestorAttr climbs n's ancestors looking for the first element whose tag is in
// tags that carries attribute key, returning its value. Used for table→cell and
// body→link propagation.
func ancestorAttr(n Node, key string, tags ...string) (string, bool) {
	for p := n.Parent(); p != nil; p = p.Parent() {
		matchTag := false
		for _, t := range tags {
			if p.Tag() == t {
				matchTag = true
				break
			}
		}
		if !matchTag {
			continue
		}
		if v, ok := p.Attr(key); ok {
			return v, true
		}
	}
	return "", false
}

// --- legacy value parsers ---

// parseLegacyColor normalizes a legacy color attribute to a value parseColor accepts: a
// #-prefixed 3/6-digit hex, a bare 3/6-digit hex (→ prefixed), or a recognized named
// color. ok is false for anything else (the attribute is then ignored).
func parseLegacyColor(v string) (string, bool) {
	s := strings.TrimSpace(v)
	if s == "" {
		return "", false
	}
	if s[0] == '#' {
		if _, ok := parseColor(newTokenizer(s)); ok {
			return s, true
		}
		return "", false
	}
	// A bare hex (e.g. "cc0000" / "f60") — all hex digits, length 3 or 6.
	if (len(s) == 3 || len(s) == 6) && isHex(s) {
		return "#" + s, true
	}
	// A named color the parser knows.
	if _, ok := parseColor(newTokenizer(s)); ok {
		return s, true
	}
	return "", false
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// legacyLength interprets a legacy width/height attribute: a bare number → px, a
// trailing % → percentage. A "*" (relative) or other form yields ok=false.
func legacyLength(v string) (string, bool) {
	s := strings.TrimSpace(v)
	if s == "" {
		return "", false
	}
	if strings.HasSuffix(s, "%") {
		if n := strings.TrimSuffix(s, "%"); isNumber(n) {
			return s, true
		}
		return "", false
	}
	if isNumber(s) {
		return s + "px", true
	}
	return "", false
}

// legacyPx parses a non-negative integer attribute into a "<n>px" value.
func legacyPx(v string) (string, bool) {
	if n, ok := legacyNumber(v); ok {
		return strconv.Itoa(n) + "px", true
	}
	return "", false
}

// legacyNumber parses a non-negative integer attribute (leading digits), tolerating a
// trailing unit-ish suffix. Negative or non-numeric yields ok=false.
func legacyNumber(v string) (int, bool) {
	s := strings.TrimSpace(v)
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0, false
	}
	return n, true
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	dot := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c == '.' && !dot:
			dot = true
		default:
			return false
		}
	}
	return true
}

// legacyListType maps a legacy list type attribute to a list-style-type keyword. For
// <ol> the values are 1/a/A/i/I; for <ul> they are disc/circle/square. ok is false for
// an unrecognized value.
func legacyListType(v, listTag string) (string, bool) {
	s := strings.TrimSpace(v)
	if listTag == "ul" {
		switch strings.ToLower(s) {
		case "disc", "circle", "square":
			return strings.ToLower(s), true
		}
		return "", false
	}
	switch s {
	case "1":
		return "decimal", true
	case "a":
		return "lower-alpha", true
	case "A":
		return "upper-alpha", true
	case "i":
		return "lower-roman", true
	case "I":
		return "upper-roman", true
	}
	return "", false
}

// legacyFontSizePx maps a <font size> attribute (an absolute 1–7, or a relative +N/-N
// from the default 3) to a coarse px size. ok is false for an unparseable value.
func legacyFontSizePx(v string) (int, bool) {
	s := strings.TrimSpace(v)
	if s == "" {
		return 0, false
	}
	// Absolute 1..7 or relative +N / -N (relative to 3).
	rel := 0
	num := s
	switch s[0] {
	case '+':
		rel, num = 1, s[1:]
	case '-':
		rel, num = -1, s[1:]
	}
	n, err := strconv.Atoi(num)
	if err != nil {
		return 0, false
	}
	level := n
	if rel != 0 {
		level = 3 + rel*n
	}
	if level < 1 {
		level = 1
	}
	if level > 7 {
		level = 7
	}
	// The classic font-size table (px), roughly matching legacy browsers.
	px := []int{10, 13, 16, 18, 24, 32, 48}
	return px[level-1], true
}
