package doctaculous

import (
	"fmt"
	"os"

	"github.com/nathanstitt/doctaculous/pkg/rtf"
)

// OpenRTF reads and renders a Rich Text Format document at path, laid out at
// the default viewport width. For additional options (e.g. WithPageSize) use
// OpenRTFFile.
func OpenRTF(path string) (*Document, error) {
	return OpenRTFFile(path)
}

// OpenRTFFile reads and renders an .rtf file at path, applying any options.
func OpenRTFFile(path string, opts ...HTMLOption) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open rtf %q: %w", path, err)
	}
	return OpenRTFBytes(data, opts...)
}

// OpenRTFBytes renders an in-memory RTF document, applying any options, and
// returns a Document ready to rasterize or convert. Paragraph and character
// formatting, alignment/indents, hyperlink fields, tables, embedded PNG/JPEG
// pictures, and the document's page geometry (as an @page rule) carry through;
// unknown control words and destinations are skipped per the RTF resilience
// rule. The document flows through the HTML pipeline, so every HTMLOption
// applies and every output format follows.
func OpenRTFBytes(data []byte, opts ...HTMLOption) (*Document, error) {
	// Surface the converter's degradation diagnostics through WithLogf.
	cfg := defaultHTMLConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	htmlSrc, err := rtf.ToHTML(data, cfg.logf)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: %w", err)
	}
	doc, err := OpenHTMLBytes([]byte(htmlSrc), opts...)
	if err != nil {
		return nil, err
	}
	doc.format = FormatRTF
	return doc, nil
}
