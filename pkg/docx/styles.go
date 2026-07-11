package docx

// Styles is the parsed style table from word/styles.xml: the document defaults
// plus named paragraph/character styles. It is read-only after Open. The style
// cascade (pkg/docx/style) consumes this to compute effective properties; this
// package only parses and stores it.
type Styles struct {
	// DocDefaultRun and DocDefaultPara are the document-wide defaults
	// (w:docDefaults), the lowest layer of the cascade.
	DocDefaultRun  RunProps
	DocDefaultPara ParagraphProps
	// ByID maps a styleId to its definition.
	ByID map[string]*Style
	// DefaultParaID is the styleId of the default paragraph style (w:default="1"
	// on a w:type="paragraph" style), or "" if none is marked.
	DefaultParaID string
}

// Style is one named style definition (w:style). A style contributes its own
// run/paragraph properties and inherits from BasedOn.
type Style struct {
	// ID is the w:styleId, the key other elements reference.
	ID string
	// Name is the style's display name (w:name), shown in Word's style UI; ""
	// when the part omits it.
	Name string
	// Type is "paragraph", "character", "table", or "numbering".
	Type string
	// BasedOn is the parent styleId (w:basedOn), or "" if this is a root style.
	BasedOn string
	// Run and Para are the properties this style itself specifies (w:rPr/w:pPr).
	Run  RunProps
	Para ParagraphProps
}
