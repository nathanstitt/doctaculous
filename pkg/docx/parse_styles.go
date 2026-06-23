package docx

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

// parseStyles walks word/styles.xml into a Styles table: the w:docDefaults plus
// every named w:style. Run and paragraph properties inside a style are parsed
// with the same helpers as direct formatting, so a style's rPr/pPr cascade
// identically to inline rPr/pPr.
func parseStyles(data []byte) (*Styles, error) {
	styles := &Styles{ByID: map[string]*Style{}}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%w: styles: %v", ErrMalformedXML, err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Space != wNS {
			continue
		}
		switch se.Name.Local {
		case "docDefaults":
			if err := parseDocDefaults(dec, styles); err != nil {
				return nil, err
			}
		case "style":
			st, err := parseStyle(dec, se)
			if err != nil {
				return nil, err
			}
			styles.ByID[st.ID] = st
			if st.Type == "paragraph" && styleIsDefault(se) {
				styles.DefaultParaID = st.ID
			}
		}
	}
	return styles, nil
}

// styleIsDefault reports whether a w:style carries w:default="1".
func styleIsDefault(e xml.StartElement) bool {
	v, _ := wAttr(e, "default")
	return parseOnOff(v) && v != ""
}

// parseDocDefaults reads w:rPrDefault/w:pPrDefault into the Styles defaults.
func parseDocDefaults(dec *xml.Decoder, styles *Styles) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: docDefaults: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "rPrDefault" {
				rPr, err := findAndParseRPr(dec, "rPrDefault")
				if err != nil {
					return err
				}
				styles.DocDefaultRun = rPr
				continue
			}
			if t.Name.Space == wNS && t.Name.Local == "pPrDefault" {
				pPr, err := findAndParsePPr(dec, "pPrDefault")
				if err != nil {
					return err
				}
				styles.DocDefaultPara = pPr
				continue
			}
			if err := dec.Skip(); err != nil {
				return fmt.Errorf("%w: docDefaults: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "docDefaults" {
				return nil
			}
		}
	}
}

// parseStyle reads a single w:style element.
func parseStyle(dec *xml.Decoder, start xml.StartElement) (*Style, error) {
	st := &Style{}
	st.Type, _ = wAttr(start, "type")
	if id, ok := wAttr(start, "styleId"); ok {
		st.ID = id
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: style: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wNS {
				if err := dec.Skip(); err != nil {
					return nil, fmt.Errorf("%w: style: %v", ErrMalformedXML, err)
				}
				continue
			}
			switch t.Name.Local {
			case "basedOn":
				st.BasedOn = wVal(t)
				if err := dec.Skip(); err != nil {
					return nil, fmt.Errorf("%w: style: %v", ErrMalformedXML, err)
				}
			case "rPr":
				rPr, err := parseRPr(dec)
				if err != nil {
					return nil, err
				}
				st.Run = rPr
			case "pPr":
				pPr, _, err := parsePPr(dec)
				if err != nil {
					return nil, err
				}
				st.Para = pPr
			default:
				if err := dec.Skip(); err != nil {
					return nil, fmt.Errorf("%w: style: %v", ErrMalformedXML, err)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "style" {
				return st, nil
			}
		}
	}
}

// findAndParseRPr scans the children of a wrapper element (e.g. rPrDefault) for a
// w:rPr and parses it, returning zero props if none is present.
func findAndParseRPr(dec *xml.Decoder, wrapper string) (RunProps, error) {
	var props RunProps
	for {
		tok, err := dec.Token()
		if err != nil {
			return props, fmt.Errorf("%w: %s: %v", ErrMalformedXML, wrapper, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "rPr" {
				props, err = parseRPr(dec)
				if err != nil {
					return props, err
				}
				continue
			}
			if err := dec.Skip(); err != nil {
				return props, fmt.Errorf("%w: %s: %v", ErrMalformedXML, wrapper, err)
			}
		case xml.EndElement:
			if t.Name.Local == wrapper {
				return props, nil
			}
		}
	}
}

// findAndParsePPr is findAndParseRPr's paragraph-property analogue.
func findAndParsePPr(dec *xml.Decoder, wrapper string) (ParagraphProps, error) {
	var props ParagraphProps
	for {
		tok, err := dec.Token()
		if err != nil {
			return props, fmt.Errorf("%w: %s: %v", ErrMalformedXML, wrapper, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "pPr" {
				props, _, err = parsePPr(dec)
				if err != nil {
					return props, err
				}
				continue
			}
			if err := dec.Skip(); err != nil {
				return props, fmt.Errorf("%w: %s: %v", ErrMalformedXML, wrapper, err)
			}
		case xml.EndElement:
			if t.Name.Local == wrapper {
				return props, nil
			}
		}
	}
}
