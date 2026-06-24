package html

import "github.com/nathanstitt/doctaculous/pkg/css"

// uaSource is the minimal user-agent default stylesheet. It is the lowest cascade
// origin (OriginUA) and supplies the display defaults and a few presentational
// defaults that make HTML render as HTML; without it every element would be
// display:inline (the CSS initial value). It is intentionally small and grows as
// later sub-projects need more defaults.
const uaSource = `
html, body, div, p, section, article, header, footer, nav, main, aside,
ul, ol, blockquote, pre, table, form, figure, figcaption, hr, h1, h2, h3, h4, h5, h6 {
	display: block;
}
li { display: list-item; }
tr { display: table-row; }
td, th { display: table-cell; }
head, title, meta, link, style, script { display: none; }

h1 { font-size: 32px; font-weight: bold; margin-top: 21px; margin-bottom: 21px; }
h2 { font-size: 24px; font-weight: bold; margin-top: 20px; margin-bottom: 20px; }
h3 { font-size: 19px; font-weight: bold; margin-top: 18px; margin-bottom: 18px; }
h4 { font-size: 16px; font-weight: bold; margin-top: 21px; margin-bottom: 21px; }
h5 { font-size: 13px; font-weight: bold; margin-top: 22px; margin-bottom: 22px; }
h6 { font-size: 11px; font-weight: bold; margin-top: 24px; margin-bottom: 24px; }
p, blockquote { margin-top: 16px; margin-bottom: 16px; }
th { font-weight: bold; }
`

// UAStylesheet is the parsed user-agent default stylesheet, cascaded at
// css.OriginUA below all author styles.
var UAStylesheet = css.Parse(uaSource)
