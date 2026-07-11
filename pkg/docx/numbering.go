package docx

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

// Numbering is the parsed word/numbering.xml: w:num instances (numId ->
// abstract reference + per-level overrides) plus w:abstractNum definitions
// (per-level format/text/start). It is read-only after Open; the exported maps
// exist so a caller can construct one for the writer.
type Numbering struct {
	// Abstract maps an abstractNumId to its levels by ilvl.
	Abstract map[int]map[int]NumLevel
	// Instances maps a w:numId to its instance definition.
	Instances map[int]NumInstance
}

// NewNumbering returns an empty Numbering, for callers constructing a document
// for the writer.
func NewNumbering() *Numbering {
	return &Numbering{Abstract: map[int]map[int]NumLevel{}, Instances: map[int]NumInstance{}}
}

// NumInstance is one w:num: the abstract definition it references plus any
// per-level overrides (w:lvlOverride).
type NumInstance struct {
	AbstractID int
	// Overrides maps an ilvl to its override; nil/absent = none.
	Overrides map[int]LevelOverride
}

// LevelOverride is a w:lvlOverride: currently the w:startOverride restart
// value (per-instance ordered lists restart their counters through this).
type LevelOverride struct {
	Start    int
	HasStart bool
}

// NumLevel is one list level's marker definition.
type NumLevel struct {
	Format NumFmt
	// Text is the w:lvlText pattern (e.g. "%1.", "•"). %N is replaced by level N's
	// current counter value when the marker is formatted.
	Text string
	// Start is the level's starting counter value (w:start); HasStart marks it
	// explicitly declared (Word's effective default is 1).
	Start    int
	HasStart bool
	// IndentLeft/Hanging are the level's paragraph indent (w:lvl > w:pPr >
	// w:ind left/hanging) in twips; Has* mark them declared. Word uses these
	// for per-level list indentation; rendering here nests by ilvl instead,
	// so the values round-trip for conversion.
	IndentLeft    Twips
	HasIndentLeft bool
	Hanging       Twips
	HasHanging    bool
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
	inst, ok := n.Instances[numID]
	if !ok {
		return NumLevel{}, false
	}
	levels, ok := n.Abstract[inst.AbstractID]
	if !ok {
		return NumLevel{}, false
	}
	lvl, ok := levels[ilvl]
	return lvl, ok
}

// StartAt resolves the effective starting counter value for (numID, ilvl): a
// w:startOverride on the instance wins, then the abstract level's w:start,
// else 1 (Word's default).
func (n *Numbering) StartAt(numID, ilvl int) int {
	if n == nil {
		return 1
	}
	inst, ok := n.Instances[numID]
	if !ok {
		return 1
	}
	if ov, ok := inst.Overrides[ilvl]; ok && ov.HasStart {
		return ov.Start
	}
	if levels, ok := n.Abstract[inst.AbstractID]; ok {
		if lvl, ok := levels[ilvl]; ok && lvl.HasStart {
			return lvl.Start
		}
	}
	return 1
}

// parseNumbering parses a word/numbering.xml part.
func parseNumbering(data []byte) (*Numbering, error) {
	n := NewNumbering()
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
			n.Abstract[id] = levels
		case "num":
			id, _ := wAttrInt(se, "numId")
			inst, err := parseNumInstance(dec)
			if err != nil {
				return nil, err
			}
			n.Instances[id] = inst
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

// parseLvl reads one w:lvl (numFmt + lvlText + start).
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
				case "start":
					if v, ok := wAttrInt(t, "val"); ok {
						lvl.Start = v
						lvl.HasStart = true
					}
				case "pPr":
					applyLvlPPr(&lvl, dec)
					continue
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

// applyLvlPPr consumes a w:lvl's w:pPr, capturing the level indent (w:ind
// left/hanging); other level paragraph properties are dropped, as before.
func applyLvlPPr(lvl *NumLevel, dec *xml.Decoder) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "ind" {
				if v, ok := wAttrInt(t, "left"); ok {
					lvl.IndentLeft, lvl.HasIndentLeft = Twips(v), true
				}
				if v, ok := wAttrInt(t, "hanging"); ok {
					lvl.Hanging, lvl.HasHanging = Twips(v), true
				}
			}
			_ = dec.Skip()
		case xml.EndElement:
			if t.Name.Local == "pPr" {
				return
			}
		}
	}
}

// parseNumInstance reads a w:num: its abstractNumId plus any w:lvlOverride
// children (per-level startOverride values).
func parseNumInstance(dec *xml.Decoder) (NumInstance, error) {
	inst := NumInstance{AbstractID: -1}
	for {
		tok, err := dec.Token()
		if err != nil {
			return inst, fmt.Errorf("%w: num: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				switch t.Name.Local {
				case "abstractNumId":
					if v, ok := wAttrInt(t, "val"); ok {
						inst.AbstractID = v
					}
				case "lvlOverride":
					ilvl, _ := wAttrInt(t, "ilvl")
					ov, err := parseLvlOverride(dec)
					if err != nil {
						return inst, err
					}
					if inst.Overrides == nil {
						inst.Overrides = map[int]LevelOverride{}
					}
					inst.Overrides[ilvl] = ov
					continue
				}
			}
			if err := dec.Skip(); err != nil {
				return inst, fmt.Errorf("%w: num: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "num" {
				return inst, nil
			}
		}
	}
}

// parseLvlOverride reads a w:lvlOverride's w:startOverride.
func parseLvlOverride(dec *xml.Decoder) (LevelOverride, error) {
	var ov LevelOverride
	for {
		tok, err := dec.Token()
		if err != nil {
			return ov, fmt.Errorf("%w: lvlOverride: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "startOverride" {
				if v, ok := wAttrInt(t, "val"); ok {
					ov.Start = v
					ov.HasStart = true
				}
			}
			if err := dec.Skip(); err != nil {
				return ov, fmt.Errorf("%w: lvlOverride: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "lvlOverride" {
				return ov, nil
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
