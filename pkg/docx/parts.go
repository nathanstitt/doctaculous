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
		// Consume the root's children with the shared block dispatch.
		for {
			tok, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("%w: %s: %v", ErrMalformedXML, root, err)
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Space != wNS {
					if err := dec.Skip(); err != nil {
						return nil, fmt.Errorf("%w: %s: %v", ErrMalformedXML, root, err)
					}
					continue
				}
				blk, _, err := parseBlockChild(dec, t)
				if err != nil {
					return nil, err
				}
				if blk != nil {
					hf.Blocks = append(hf.Blocks, *blk)
				}
			case xml.EndElement:
				if t.Name.Local == root {
					return hf, nil
				}
			}
		}
	}
	return hf, nil
}
