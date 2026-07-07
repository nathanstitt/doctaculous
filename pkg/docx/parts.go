package docx

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

// HeaderFooter is a parsed header or footer part (w:hdr / w:ftr): a sequence of
// block-level content (paragraphs, tables) rendered in the page margin band.
type HeaderFooter struct {
	Blocks []Block
}

// parseHdrFtr parses a header/footer part. root is the expected root local name
// ("hdr" or "ftr"); the body content uses the same block grammar as w:body.
func parseHdrFtr(data []byte, root string) (*HeaderFooter, error) {
	hf := &HeaderFooter{}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%w: %s: %v", ErrMalformedXML, root, err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Space != wNS || se.Name.Local != root {
			continue
		}
		// Consume the root's children with the shared block-consumption loop.
		if err := fillBlocksUntil(dec, root, &hf.Blocks); err != nil {
			return nil, err
		}
		return hf, nil
	}
	return hf, nil
}
