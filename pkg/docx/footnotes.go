package docx

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

// Footnotes is the parsed word/footnotes.xml: note id -> block content.
type Footnotes struct {
	byID map[int]*HeaderFooter // reuse HeaderFooter's Blocks container
}

// Note returns a footnote's content by id.
func (f *Footnotes) Note(id int) (*HeaderFooter, bool) {
	if f == nil {
		return nil, false
	}
	n, ok := f.byID[id]
	return n, ok
}

// parseFootnotes parses a word/footnotes.xml part. Separator/continuation notes
// (negative or special ids) are parsed like any other; the lowering ignores ids
// it has no reference for.
func parseFootnotes(data []byte) (*Footnotes, error) {
	f := &Footnotes{byID: map[int]*HeaderFooter{}}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%w: footnotes: %v", ErrMalformedXML, err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Space != wNS || se.Name.Local != "footnote" {
			continue
		}
		id, _ := wAttrInt(se, "id")
		note := &HeaderFooter{}
		if err := fillBlocksUntil(dec, "footnote", &note.Blocks); err != nil {
			return nil, err
		}
		f.byID[id] = note
	}
	return f, nil
}

// fillBlocksUntil consumes block content until the named end element, appending
// to blocks. Shared by footnote parsing.
func fillBlocksUntil(dec *xml.Decoder, end string, blocks *[]Block) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrMalformedXML, end, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wNS {
				if err := dec.Skip(); err != nil {
					return fmt.Errorf("%w: %s: %v", ErrMalformedXML, end, err)
				}
				continue
			}
			blk, _, err := parseBlockChild(dec, t)
			if err != nil {
				return err
			}
			if blk != nil {
				*blocks = append(*blocks, *blk)
			}
		case xml.EndElement:
			if t.Name.Local == end {
				return nil
			}
		}
	}
}
