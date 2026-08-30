package html

import "github.com/nathanstitt/omnidoc/pkg/css"

// uaSource is the minimal user-agent default stylesheet. It is the lowest cascade
// origin (OriginUA) and supplies the display defaults and a few presentational
// defaults that make HTML render as HTML; without it every element would be
// display:inline (the CSS initial value). It is intentionally small and grows as
// later sub-projects need more defaults.
const uaSource = `
html, body, div, p, section, article, header, footer, nav, main, aside,
ul, ol, blockquote, pre, form, figure, figcaption, hr, fieldset, legend,
h1, h2, h3, h4, h5, h6 {
	display: block;
}
li { display: list-item; }
/* Lists: block-level boxes with the standard left indent for the marker, the
   default marker styles, and the conventional nested-bullet rotation. */
ul, ol { margin-top: 1em; margin-bottom: 1em; padding-left: 40px; counter-reset: list-item; }
ul { list-style-type: disc; }
ol { list-style-type: decimal; }
ul ul, ol ul { list-style-type: circle; }
ul ul ul, ol ol ul { list-style-type: square; }
tr { display: table-row; }
td, th { display: table-cell; }
table { display: table; }
thead { display: table-header-group; }
tbody { display: table-row-group; }
tfoot { display: table-footer-group; }
col { display: table-column; }
colgroup { display: table-column-group; }
caption { display: table-caption; }
head, title, meta, link, style, script { display: none; }

/* Bidi isolation (HTML §15.3.3). Both are display:inline by the CSS initial value,
   so only the isolation behavior is declared here; the dir attribute itself
   arrives as a presentational hint (this engine has no attribute selectors). The
   values are stored and take effect when inline bidi reordering lands. */
bdi { unicode-bidi: isolate; }
bdo { unicode-bidi: isolate-override; }

/* Heading margins follow the W3C CSS2.1 sample UA sheet (~0.67em of the
   heading's font-size), so they decrease with font-size rather than inverting. */
h1 { font-size: 32px; font-weight: bold; margin-top: 21px; margin-bottom: 21px; }
h2 { font-size: 24px; font-weight: bold; margin-top: 16px; margin-bottom: 16px; }
h3 { font-size: 19px; font-weight: bold; margin-top: 13px; margin-bottom: 13px; }
h4 { font-size: 16px; font-weight: bold; margin-top: 11px; margin-bottom: 11px; }
h5 { font-size: 13px; font-weight: bold; margin-top: 9px; margin-bottom: 9px; }
h6 { font-size: 11px; font-weight: bold; margin-top: 7px; margin-bottom: 7px; }
p, blockquote { margin-top: 16px; margin-bottom: 16px; }
th { font-weight: bold; }

/* Inline emphasis (HTML Standard "Rendering" §15.3.3, CSS2.1 sample UA sheet).
   Without these, <strong>/<b> and <em>/<i> inherited the surrounding weight and slant,
   so emphasis was structurally present — and survived conversion to Markdown — but was
   invisible in every rasterized format.
   The spec says "bolder"/"smaller", but this engine's font-weight is the binary
   bold/normal its four-style bundled families can express and its font-size takes a
   length, so neither relative keyword parses: they would be dropped as invalid values
   and the emphasis would stay invisible. "bold" is the honest spelling of what the
   renderer can actually do. <small>/<big> are deliberately omitted rather than given a
   hardcoded px size, which would be wrong at every font-size but the default. */
strong, b { font-weight: bold; }
em, i, cite, var, dfn { font-style: italic; }
u, ins { text-decoration: underline; }
/* <mark>'s yellow highlight, per the HTML Standard's rendering section. It landed with
   inline-box background painting: before that, backgrounds on non-replaced inline boxes
   were silently dropped, so this rule would have cascaded and then done nothing. */
mark { background-color: #ffff00; color: #000000; }
/* Preformatted text preserves whitespace and uses a monospace family (CSS2.1 sample
   UA sheet). pre-wrap on textarea so a long line still wraps inside the field. */
pre { white-space: pre; font-family: monospace; }
code, kbd, samp { font-family: monospace; }

/* Hyperlinks: the classic browser default — blue and underlined. Scoped to :link
   (an <a> with href) so a bare named-anchor <a> is not styled. Author a/a:link rules
   override this (it is the lowest, UA origin). */
a:link { color: #0000ee; text-decoration: underline; }
/* Struck-through text: <s>, <strike>, and <del> render with a line-through (the
   classic browser default). This also gives the markdown/text conversion path its
   strikethrough signal via text-decoration. */
s, strike, del { text-decoration: line-through; }

input, textarea, select, button {
	display: inline-block;
	font-size: 13px;
	line-height: normal;
}
textarea { vertical-align: text-bottom; }
input, select, button { vertical-align: baseline; }
`

// UAStylesheet is the parsed user-agent default stylesheet, cascaded at
// css.OriginUA below all author styles.
var UAStylesheet = css.Parse(uaSource)
