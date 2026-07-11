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
		FormatPDF:  true,
		FormatDOCX: true,
		FormatHTML: true,
		// FormatMarkdown and FormatText flip true when their frontends land.
	}
	outputs := map[Format]bool{
		FormatPDF:      true,
		FormatHTML:     true,
		FormatMarkdown: true,
		FormatText:     true,
		FormatPNG:      true,
		FormatJPEG:     true,
		// FormatDOCX flips true when the DOCX writer lands.
	}
	all := []Format{FormatPDF, FormatDOCX, FormatHTML, FormatMarkdown, FormatText, FormatPNG, FormatJPEG}

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
