package html

import "github.com/nathanstitt/doctaculous/pkg/css"

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
/* Preformatted text preserves whitespace and uses a monospace family (CSS2.1 sample
   UA sheet). pre-wrap on textarea so a long line still wraps inside the field. */
pre { white-space: pre; font-family: monospace; }
code, kbd, samp { font-family: monospace; }

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
