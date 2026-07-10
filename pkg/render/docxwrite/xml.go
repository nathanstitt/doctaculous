package docxwrite

import "strings"

// The writers build WordprocessingML by direct string assembly with explicit
// escapers — matching the repo's other serializers (pdfwrite's object model,
// htmlwrite's replacers) — because encoding/xml cannot emit the prefixed form
// (<w:p>) real-world OOXML consumers expect: it rewrites each element with a
// default-namespace declaration, ballooning output and hurting interop.

// escText escapes character data.
var escText = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// escAttr escapes attribute values.
var escAttr = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
