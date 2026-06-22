package gen

// This file exposes the embedded raw font bytes to tests in other packages
// (notably pkg/font), which need real font programs to exercise parsing and
// glyph extraction hermetically. It is a _test.go file so the accessors are
// only compiled into test binaries.

// RobotoTTF returns the embedded Roboto TrueType program bytes.
func RobotoTTF() []byte { return robotoTTF }

// SourceSansOTF returns the embedded Source Sans 3 OpenType/CFF bytes.
func SourceSansOTF() []byte { return sourceSansOTF }

// SourceSansCFF returns the bare "CFF " table extracted from Source Sans 3.
func SourceSansCFF() []byte { return extractCFFTable(sourceSansOTF) }
