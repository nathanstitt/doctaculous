package markdown

// DefaultCSS is the stylesheet embedded in the generated HTML document: a
// GitHub-flavored look layered over the UA stylesheet (which already supplies
// the heading scale, block margins, monospace pre/code, and list rendering).
// It is an author-origin <style> element — not a UA-sheet change (that would
// affect every HTML document) and not a resource-loader concern (the loader
// stays free for the source's own relative image refs). Only properties the
// CSS engine implements appear here.
const DefaultCSS = `body {
	font-family: sans-serif;
	line-height: 1.5;
	margin: 32px;
}
h1, h2 {
	border-bottom: 1px solid #d8dee4;
	padding-bottom: 8px;
}
pre {
	background-color: #f6f8fa;
	padding: 16px;
}
code {
	background-color: #f6f8fa;
}
blockquote {
	margin-left: 0;
	padding-left: 16px;
	border-left: 4px solid #d0d7de;
	color: #59636e;
}
table {
	border-collapse: collapse;
}
th, td {
	border: 1px solid #d0d7de;
	padding: 6px 13px;
}
hr {
	border: none;
	border-top: 4px solid #d8dee4;
}
img {
	max-width: 100%;
}
`
