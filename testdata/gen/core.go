package gen

// Core is the canonical set of fixtures that every layer's tests should operate
// on. It is intentionally small (10 files) and stable: each entry locks down one
// distinct, must-always-work code path from parsing through rasterization. Edge
// cases (most malformed inputs, extreme rotations, filter variants) live as
// targeted fixtures elsewhere and are NOT part of this set.
//
// Every Core fixture is expected to parse to a valid Document, expose exactly
// Pages pages, and rasterize without error — including BadXref, which recovers
// via the object-scan rebuild path. Tests can therefore treat the whole set
// uniformly: "for every CoreFixture, X must succeed."
//
// Use Core in table-driven tests:
//
//	for _, f := range gen.Core {
//	    doc, err := pdf.Parse(f.Bytes())
//	    ...
//	}
type CoreFixture struct {
	// Name is a short, stable identifier used in subtest names and golden
	// filenames (e.g. "text", "xref-stream").
	Name string
	// Desc explains the single feature path this fixture locks down.
	Desc string
	// Pages is the exact page count the fixture must parse to.
	Pages int
	// Build returns the fixture bytes. It is deterministic.
	Build func() []byte
}

// Bytes returns the fixture's PDF bytes.
func (f CoreFixture) Bytes() []byte { return f.Build() }

// Core lists the canonical fixtures, ordered from simplest to most complex
// structurally. See CoreFixture for the contract every entry satisfies.
var Core = []CoreFixture{
	{
		Name:  "text",
		Desc:  "classic xref table, text content stream, Helvetica resource",
		Pages: 1,
		Build: TextPDF,
	},
	{
		Name:  "vector",
		Desc:  "vector paths: filled rectangle + stroked line",
		Pages: 1,
		Build: VectorPDF,
	},
	{
		Name:  "stroke-joins",
		Desc:  "thick polylines stroked with miter/round/bevel joins and butt/round/square caps",
		Pages: 1,
		Build: StrokeJoinsPDF,
	},
	{
		Name:  "even-odd",
		Desc:  "even-odd fill (f*): square with a square hole",
		Pages: 1,
		Build: EvenOddPDF,
	},
	{
		Name:  "alpha",
		Desc:  "ExtGState constant alpha (/ca, /CA): semi-transparent fill + stroke",
		Pages: 1,
		Build: AlphaPDF,
	},
	{
		Name:  "blend-multiply",
		Desc:  "ExtGState /BM /Multiply: overlapping red+blue rects (overlap → black)",
		Pages: 1,
		Build: func() []byte { return BlendModePDF("Multiply") },
	},
	{
		Name:  "blend-screen",
		Desc:  "ExtGState /BM /Screen: overlapping red+blue rects (overlap → magenta)",
		Pages: 1,
		Build: func() []byte { return BlendModePDF("Screen") },
	},
	{
		Name:  "form-xobject",
		Desc:  "form XObject (Do) with /Matrix and scoped /Resources",
		Pages: 1,
		Build: FormXObjectPDF,
	},
	{
		Name:  "shading-axial",
		Desc:  "axial (Type 2) shading via sh: red→blue diagonal ramp, /Extend both",
		Pages: 1,
		Build: ShadingAxialPDF,
	},
	{
		Name:  "shading-radial",
		Desc:  "radial (Type 3) shading via sh: concentric green→yellow circles",
		Pages: 1,
		Build: ShadingRadialPDF,
	},
	{
		Name:  "shading-function",
		Desc:  "function-based (Type 1) shading via sh: 2-D color field from a Type 4 function",
		Pages: 1,
		Build: ShadingFunctionPDF,
	},
	{
		Name:  "shading-pattern",
		Desc:  "shading pattern (PatternType 2) via scn: a diamond filled with an axial gradient",
		Pages: 1,
		Build: ShadingPatternPDF,
	},
	{
		Name:  "shading-mesh",
		Desc:  "free-form Gouraud mesh (Type 4) via sh: a two-triangle square, color per corner",
		Pages: 1,
		Build: ShadingMeshPDF,
	},
	{
		Name:  "flate",
		Desc:  "FlateDecode-compressed content stream",
		Pages: 1,
		Build: FlateTextPDF,
	},
	{
		Name:  "multipage",
		Desc:  "three-page page tree; exercises /Count and the parallel render path",
		Pages: 3,
		Build: func() []byte { return MultiPagePDF(3) },
	},
	{
		Name:  "rotated",
		Desc:  "page with /Rotate 90 (rotation inheritance + raster transform)",
		Pages: 1,
		Build: func() []byte { return RotatedPDF(90) },
	},
	{
		Name:  "image-flate",
		Desc:  "image XObject with FlateDecode raw DeviceRGB samples",
		Pages: 1,
		Build: ImagePDF,
	},
	{
		Name:  "image-mask",
		Desc:  "1-bit /ImageMask stencil painted in the fill color",
		Pages: 1,
		Build: ImageMaskPDF,
	},
	{
		Name:  "image-jpeg",
		Desc:  "image XObject with DCTDecode (baseline JPEG) data",
		Pages: 1,
		Build: JPEGImagePDF,
	},
	{
		Name:  "image-gray",
		Desc:  "8-bit DeviceGray image (black→white gradient)",
		Pages: 1,
		Build: GrayImagePDF,
	},
	{
		Name:  "image-cmyk",
		Desc:  "8-bit DeviceCMYK image (cyan/magenta/yellow/black quadrants)",
		Pages: 1,
		Build: CMYKImagePDF,
	},
	{
		Name:  "image-indexed",
		Desc:  "4-bit Indexed image over a DeviceRGB palette",
		Pages: 1,
		Build: IndexedImagePDF,
	},
	{
		Name:  "image-ccitt",
		Desc:  "1-bpp bilevel image via CCITTFaxDecode Group 4 (K<0)",
		Pages: 1,
		Build: CCITTImagePDF,
	},
	{
		Name:  "image-smask",
		Desc:  "DeviceRGB image with a DeviceGray /SMask (left opaque, right transparent)",
		Pages: 1,
		Build: SMaskImagePDF,
	},
	{
		Name:  "inline-image",
		Desc:  "inline image (BI...ID...EI) with abbreviated keys, raw DeviceRGB samples",
		Pages: 1,
		Build: InlineImagePDF,
	},
	{
		Name:  "xref-stream",
		Desc:  "cross-reference stream (/Type /XRef), no classic table",
		Pages: 1,
		Build: XRefStreamPDF,
	},
	{
		Name:  "objstm",
		Desc:  "compressed objects in an object stream (/Type /ObjStm)",
		Pages: 1,
		Build: ObjStmPDF,
	},
	{
		Name:  "bad-xref",
		Desc:  "bogus startxref offset; must recover via object-scan rebuild",
		Pages: 1,
		Build: BadXrefOffsetPDF,
	},
	{
		Name:  "encrypted-rc4",
		Desc:  "Standard Security Handler, empty user password, RC4 (V2/R3); decrypts a filled rectangle + /Title string",
		Pages: 1,
		Build: EncryptedRC4PDF,
	},
	{
		Name:  "embedded-truetype",
		Desc:  "simple TrueType font with an embedded FontFile2 (Roboto)",
		Pages: 1,
		Build: EmbeddedTrueTypePDF,
	},
	{
		Name:  "type0",
		Desc:  "Type0/CIDFontType2 Identity-H with an embedded FontFile2 (Roboto)",
		Pages: 1,
		Build: EmbeddedType0PDF,
	},
	{
		Name:  "cff",
		Desc:  "simple Type1 font with an embedded FontFile3 Type1C (Source Sans 3 CFF)",
		Pages: 1,
		Build: EmbeddedCFFPDF,
	},
	{
		Name:  "type1",
		Desc:  "simple Type1 font with an embedded classic FontFile (TeX Gyre Termes, eexec)",
		Pages: 1,
		Build: EmbeddedType1PDF,
	},
	{
		Name:  "symbolic-truetype",
		Desc:  "symbolic embedded TrueType, no /Encoding; glyphs via raw-code/code-as-GID",
		Pages: 1,
		Build: EmbeddedSymbolicTrueTypePDF,
	},
}
