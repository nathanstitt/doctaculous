package docx

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

// Numbering is the parsed word/numbering.xml: w:num instances (numId ->
// abstractNumId) plus w:abstractNum definitions (per-level format/text). It is
// read-only after Open.
type Numbering struct {
	// numToAbstract maps a w:numId to its w:abstractNumId.
	numToAbstract map[int]int
	// abstract maps an abstractNumId to its levels by ilvl.
	abstract map[int]map[int]NumLevel
}

// NumLevel is one list level's marker definition.
type NumLevel struct {
	Format NumFmt
	// Text is the w:lvlText pattern (e.g. "%1.", "•"). %N is replaced by level N's
	// current counter value when the marker is formatted.
	Text string
}

// NumFmt is a w:numFmt list-marker format.
type NumFmt int

const (
	NumFmtDecimal NumFmt = iota
	NumFmtBullet
	NumFmtLowerRoman
	NumFmtUpperRoman
	NumFmtLowerLetter
	NumFmtUpperLetter
	NumFmtNone
)

// Level resolves a (numId, ilvl) to its level definition. ok is false when the
// numId or level is unknown.
func (n *Numbering) Level(numID, ilvl int) (NumLevel, bool) {
	if n == nil {
		return NumLevel{}, false
	}
	absID, ok := n.numToAbstract[numID]
	if !ok {
		return NumLevel{}, false
	}
	levels, ok := n.abstract[absID]
	if !ok {
		return NumLevel{}, false
	}
	lvl, ok := levels[ilvl]
	return lvl, ok
}

// parseNumbering parses a word/numbering.xml part.
func parseNumbering(data []byte) (*Numbering, error) {
	n := &Numbering{
		numToAbstract: map[int]int{},
		abstract:      map[int]map[int]NumLevel{},
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%w: numbering: %v", ErrMalformedXML, err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Space != wNS {
			continue
		}
		switch se.Name.Local {
		case "abstractNum":
			id, _ := wAttrInt(se, "abstractNumId")
			levels, err := parseAbstractNum(dec)
			if err != nil {
				return nil, err
			}
			n.abstract[id] = levels
		case "num":
			id, _ := wAttrInt(se, "numId")
			abs, err := parseNumInstance(dec)
			if err != nil {
				return nil, err
			}
			n.numToAbstract[id] = abs
		}
	}
	return n, nil
}

// parseAbstractNum reads a w:abstractNum's levels keyed by ilvl.
func parseAbstractNum(dec *xml.Decoder) (map[int]NumLevel, error) {
	levels := map[int]NumLevel{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: abstractNum: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "lvl" {
				ilvl, _ := wAttrInt(t, "ilvl")
				lvl, err := parseLvl(dec)
				if err != nil {
					return nil, err
				}
				levels[ilvl] = lvl
				continue
			}
			if err := dec.Skip(); err != nil {
				return nil, fmt.Errorf("%w: abstractNum: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "abstractNum" {
				return levels, nil
			}
		}
	}
}

// parseLvl reads one w:lvl (numFmt + lvlText).
func parseLvl(dec *xml.Decoder) (NumLevel, error) {
	var lvl NumLevel
	for {
		tok, err := dec.Token()
		if err != nil {
			return lvl, fmt.Errorf("%w: lvl: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				switch t.Name.Local {
				case "numFmt":
					lvl.Format = parseNumFmt(wVal(t))
				case "lvlText":
					lvl.Text = wVal(t)
				}
			}
			if err := dec.Skip(); err != nil {
				return lvl, fmt.Errorf("%w: lvl: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "lvl" {
				return lvl, nil
			}
		}
	}
}

// parseNumInstance reads a w:num, returning its abstractNumId.
func parseNumInstance(dec *xml.Decoder) (int, error) {
	abs := -1
	for {
		tok, err := dec.Token()
		if err != nil {
			return abs, fmt.Errorf("%w: num: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "abstractNumId" {
				if v, ok := wAttrInt(t, "val"); ok {
					abs = v
				}
			}
			if err := dec.Skip(); err != nil {
				return abs, fmt.Errorf("%w: num: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "num" {
				return abs, nil
			}
		}
	}
}

// ParseNumberingForTest exposes parseNumbering to external test packages
// (pkg/docx/cssbox). It is not part of the stable API.
func ParseNumberingForTest(data []byte) (*Numbering, error) { return parseNumbering(data) }

// parseNumFmt maps a w:numFmt value to a NumFmt.
func parseNumFmt(val string) NumFmt {
	switch val {
	case "bullet":
		return NumFmtBullet
	case "lowerRoman":
		return NumFmtLowerRoman
	case "upperRoman":
		return NumFmtUpperRoman
	case "lowerLetter":
		return NumFmtLowerLetter
	case "upperLetter":
		return NumFmtUpperLetter
	case "none":
		return NumFmtNone
	default:
		return NumFmtDecimal
	}
}
