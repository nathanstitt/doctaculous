package docx

import "errors"

// Sentinel errors describing why a .docx could not be opened or parsed. Callers
// branch on these to distinguish "not a DOCX at all" from "a DOCX we couldn't
// fully read." Per the project's degradation policy, malformed input returns an
// error here rather than panicking; unsupported in-document features are skipped
// (and logged) during lowering, not surfaced as errors.
var (
	// ErrNotDocx is returned when the input is not a readable OOXML package — not
	// a ZIP, or a ZIP without the [Content_Types].xml that every OPC package must
	// contain.
	ErrNotDocx = errors.New("docx: not an OOXML package")

	// ErrMissingPart is returned when a required part is absent, most importantly
	// the main document part (word/document.xml).
	ErrMissingPart = errors.New("docx: required part missing")

	// ErrMalformedXML is returned when a part that must be valid XML (the main
	// document or the styles part) cannot be decoded.
	ErrMalformedXML = errors.New("docx: malformed XML")
)
