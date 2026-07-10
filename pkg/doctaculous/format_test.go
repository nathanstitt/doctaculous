package doctaculous

import (
	"errors"
	"testing"
)

func TestParseFormat(t *testing.T) {
	cases := []struct {
		in   string
		want Format
	}{
		{"pdf", FormatPDF},
		{"PDF", FormatPDF},
		{".pdf", FormatPDF},
		{"docx", FormatDOCX},
		{"html", FormatHTML},
		{"htm", FormatHTML},
		{"xhtml", FormatHTML},
		{"markdown", FormatMarkdown},
		{"md", FormatMarkdown},
		{"MD", FormatMarkdown},
		{"text", FormatText},
		{"txt", FormatText},
		{"plain", FormatText},
		{"csv", FormatCSV},
		{"tsv", FormatTSV},
		{"tab", FormatTSV},
		{"xlsx", FormatXLSX},
		{"xlsm", FormatXLSX},
		{"png", FormatPNG},
		{"jpeg", FormatJPEG},
		{"jpg", FormatJPEG},
		{" jpg ", FormatJPEG},
	}
	for _, c := range cases {
		got, err := ParseFormat(c.in)
		if err != nil {
			t.Errorf("ParseFormat(%q): unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseFormat(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "rtf", "word", "image"} {
		got, err := ParseFormat(bad)
		if !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("ParseFormat(%q): want ErrUnknownFormat, got (%q, %v)", bad, got, err)
		}
	}
}

func TestFormatFromPath(t *testing.T) {
	cases := []struct {
		path string
		want Format
	}{
		{"doc.pdf", FormatPDF},
		{"/a/b/Doc.PDF", FormatPDF},
		{"doc.docx", FormatDOCX},
		{"page.html", FormatHTML},
		{"page.htm", FormatHTML},
		{"page.xhtml", FormatHTML},
		{"README.md", FormatMarkdown},
		{"notes.markdown", FormatMarkdown},
		{"notes.txt", FormatText},
		{"notes.text", FormatText},
		{"data.csv", FormatCSV},
		{"data.tsv", FormatTSV},
		{"data.tab", FormatTSV},
		{"book.xlsx", FormatXLSX},
		{"book.xlsm", FormatXLSX},
		{"img.png", FormatPNG},
		{"img.jpg", FormatJPEG},
		{"img.jpeg", FormatJPEG},
		{"noext", FormatUnknown},
		{"", FormatUnknown},
		{"archive.zip", FormatUnknown},
		{"doc.rtf", FormatUnknown},
	}
	for _, c := range cases {
		if got := FormatFromPath(c.path); got != c.want {
			t.Errorf("FormatFromPath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// TestCanConvertMatrix pins the full conversion matrix. The role tables below
// are the deliberate expectation, independent of the implementation's
// capability map: when a new frontend or writer lands, flipping its bit here
// is part of that PR.
func TestCanConvertMatrix(t *testing.T) {
	inputs := map[Format]bool{
		FormatPDF:      true,
		FormatDOCX:     true,
		FormatHTML:     true,
		FormatMarkdown: true,
		FormatText:     true,
		FormatCSV:      true,
		FormatTSV:      true,
		FormatXLSX:     true,
	}
	outputs := map[Format]bool{
		FormatPDF:      true,
		FormatDOCX:     true,
		FormatHTML:     true,
		FormatMarkdown: true,
		FormatText:     true,
		FormatCSV:      true,
		FormatTSV:      true,
		FormatXLSX:     true,
		FormatPNG:      true,
		FormatJPEG:     true,
	}
	all := []Format{FormatPDF, FormatDOCX, FormatHTML, FormatMarkdown, FormatText, FormatCSV, FormatTSV, FormatXLSX, FormatPNG, FormatJPEG}

	for _, from := range all {
		for _, to := range all {
			err := CanConvert(from, to)
			var want error
			switch {
			case !inputs[from], !outputs[to]:
				want = ErrUnsupportedFormat
			case from == to:
				want = ErrSameFormat
			}
			if want == nil {
				if err != nil {
					t.Errorf("CanConvert(%s, %s): want nil, got %v", from, to, err)
				}
			} else if !errors.Is(err, want) {
				t.Errorf("CanConvert(%s, %s): want %v, got %v", from, to, want, err)
			}
		}
	}

	// Unknown or unrecognized formats on either side.
	for _, pair := range [][2]Format{
		{FormatUnknown, FormatPDF},
		{FormatPDF, FormatUnknown},
		{Format("rtf"), FormatPDF},
		{FormatPDF, Format("rtf")},
	} {
		if err := CanConvert(pair[0], pair[1]); !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("CanConvert(%q, %q): want ErrUnknownFormat, got %v", pair[0], pair[1], err)
		}
	}
}

func TestFormatFromMIME(t *testing.T) {
	cases := []struct {
		in   string
		want Format
	}{
		{"application/pdf", FormatPDF},
		{"application/x-pdf", FormatPDF},
		{"APPLICATION/PDF", FormatPDF},
		{`application/pdf; name="doc.pdf"`, FormatPDF},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", FormatDOCX},
		{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", FormatXLSX},
		{"text/html", FormatHTML},
		{"TEXT/HTML; Charset=UTF-8", FormatHTML},
		{"application/xhtml+xml", FormatHTML},
		{"text/markdown", FormatMarkdown},
		{"text/x-markdown", FormatMarkdown},
		{"text/plain", FormatText},
		{"text/plain; charset=utf-8", FormatText},
		{"text/csv", FormatCSV},
		{"application/csv", FormatCSV},
		{"text/tab-separated-values", FormatTSV},
		{"image/png", FormatPNG},
		{"image/jpeg", FormatJPEG},
		{"image/jpg", FormatJPEG},
		// An unlisted text/* subtype falls back to plain text (the browser rule).
		{"text/x-log", FormatText},
		{"text/vcard", FormatText},
		// A malformed parameter section still matches on the media type.
		{"text/html; charset", FormatHTML},
	}
	for _, c := range cases {
		if got := FormatFromMIME(c.in); got != c.want {
			t.Errorf("FormatFromMIME(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// Deliberate FormatUnknown rows. The legacy binary Office types must NEVER
	// map to their OOXML cousins — a .doc is not a .docx — and generic
	// containers say nothing about their content.
	unknown := []string{
		"application/msword",
		"application/vnd.ms-word",
		"application/vnd.ms-excel",
		"application/vnd.ms-powerpoint",
		// Flips to its Format when the corresponding frontend lands.
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"application/epub+zip",
		"application/rtf",
		"text/rtf", // the text/* fallback exception
		"image/heic",
		"image/heif",
		"image/heic-sequence",
		"image/heif-sequence",
		"application/zip",
		"application/octet-stream",
		"image/webp",
		"application/json",
		"",
		"garbage",
	}
	for _, mt := range unknown {
		if got := FormatFromMIME(mt); got != FormatUnknown {
			t.Errorf("FormatFromMIME(%q) = %q, want FormatUnknown", mt, got)
		}
	}
}

// TestFormatMIMERoundTrip pins the documented invariant: every format the
// toolkit knows has a canonical MIME type that maps back to itself.
func TestFormatMIMERoundTrip(t *testing.T) {
	for f := range formatCaps {
		mt := f.MIME()
		if mt == "" {
			t.Errorf("%s.MIME() = %q, want a canonical type", f, mt)
			continue
		}
		if got := FormatFromMIME(mt); got != f {
			t.Errorf("FormatFromMIME(%s.MIME() = %q) = %q, want %s", f, mt, got, f)
		}
	}
	if got := FormatUnknown.MIME(); got != "" {
		t.Errorf("FormatUnknown.MIME() = %q, want \"\"", got)
	}
}

func TestFormatRoles(t *testing.T) {
	if !FormatPDF.ValidInput() || !FormatPDF.ValidOutput() {
		t.Errorf("FormatPDF should be a valid input and output")
	}
	if FormatPNG.ValidInput() {
		t.Errorf("FormatPNG should not be a valid input")
	}
	if FormatUnknown.ValidInput() || FormatUnknown.ValidOutput() {
		t.Errorf("FormatUnknown should have no roles")
	}
}
