package pdf

import (
	"bytes"
	"fmt"
)

// Rectangle is a PDF rectangle in default user space units (points).
type Rectangle struct {
	LLX, LLY, URX, URY float64
}

// Width returns the rectangle width.
func (r Rectangle) Width() float64 { return r.URX - r.LLX }

// Height returns the rectangle height.
func (r Rectangle) Height() float64 { return r.URY - r.LLY }

// Page is a single resolved page with its inherited attributes.
type Page struct {
	doc       *Document
	dict      Dict
	MediaBox  Rectangle
	CropBox   Rectangle
	Rotate    int  // normalized to 0, 90, 180, or 270
	Resources Dict // inherited resource dictionary
}

// Doc returns the document this page belongs to.
func (p *Page) Doc() *Document { return p.doc }

// Dict returns the page's own dictionary.
func (p *Page) Dict() Dict { return p.dict }

// loadPages walks the page tree from the catalog and flattens it into d.pages.
func (d *Document) loadPages() error {
	root := d.GetDict(d.trailer["Root"])
	if root == nil {
		return fmt.Errorf("pdf: missing document catalog (/Root)")
	}
	pagesRoot := d.GetDict(root["Pages"])
	if pagesRoot == nil {
		return fmt.Errorf("pdf: missing page tree (/Pages)")
	}
	inherited := inheritedAttrs{}
	if err := d.walkPageTree(pagesRoot, inherited, 0); err != nil {
		return err
	}
	if len(d.pages) == 0 {
		return fmt.Errorf("pdf: document has no pages")
	}
	return nil
}

// inheritedAttrs carries attributes inheritable down the page tree.
type inheritedAttrs struct {
	mediaBox  *Rectangle
	cropBox   *Rectangle
	resources Dict
	rotate    *int
}

func (d *Document) walkPageTree(node Dict, inh inheritedAttrs, depth int) error {
	if depth > 64 {
		return fmt.Errorf("pdf: page tree too deep (possible cycle)")
	}
	// Update inherited attributes from this node.
	if mb := d.rectangle(node["MediaBox"]); mb != nil {
		inh.mediaBox = mb
	}
	if cb := d.rectangle(node["CropBox"]); cb != nil {
		inh.cropBox = cb
	}
	if res := d.GetDict(node["Resources"]); res != nil {
		inh.resources = res
	}
	if r, ok := d.GetInt(node["Rotate"]); ok {
		inh.rotate = &r
	}

	typ, _ := d.GetName(node["Type"])
	switch typ {
	case "Page":
		d.appendPage(node, inh)
		return nil
	case "Pages", "":
		kids := d.GetArray(node["Kids"])
		if kids == nil && typ == "" {
			// Some pages omit /Type; if it has Contents/MediaBox treat as a page.
			if node["Contents"] != nil || node["MediaBox"] != nil {
				d.appendPage(node, inh)
				return nil
			}
		}
		for _, kid := range kids {
			kd := d.GetDict(kid)
			if kd == nil {
				continue
			}
			if err := d.walkPageTree(kd, inh, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return nil
	}
}

func (d *Document) appendPage(node Dict, inh inheritedAttrs) {
	pg := &Page{doc: d, dict: node}
	if inh.mediaBox != nil {
		pg.MediaBox = *inh.mediaBox
	} else {
		pg.MediaBox = Rectangle{0, 0, 612, 792} // US Letter default
	}
	if inh.cropBox != nil {
		pg.CropBox = *inh.cropBox
	} else {
		pg.CropBox = pg.MediaBox
	}
	if inh.resources != nil {
		pg.Resources = inh.resources
	} else {
		pg.Resources = Dict{}
	}
	if inh.rotate != nil {
		pg.Rotate = normalizeRotate(*inh.rotate)
	}
	d.pages = append(d.pages, pg)
}

// normalizeRotate reduces a /Rotate value to one of 0, 90, 180, 270. PDF
// requires multiples of 90; malformed values are snapped to the nearest multiple
// so downstream rendering always has a well-defined orientation.
func normalizeRotate(r int) int {
	r %= 360
	if r < 0 {
		r += 360
	}
	// Snap to the nearest multiple of 90 (handles malformed values like 45).
	return ((r + 45) / 90 % 4) * 90
}

// rectangle resolves o to a Rectangle (array of 4 numbers), normalizing so LL is
// the lower-left and UR the upper-right corner.
func (d *Document) rectangle(o Object) *Rectangle {
	a := d.GetArray(o)
	if len(a) != 4 {
		return nil
	}
	var v [4]float64
	for i := range 4 {
		f, ok := Number(d.Resolve(a[i]))
		if !ok {
			return nil
		}
		v[i] = f
	}
	r := Rectangle{LLX: v[0], LLY: v[1], URX: v[2], URY: v[3]}
	if r.LLX > r.URX {
		r.LLX, r.URX = r.URX, r.LLX
	}
	if r.LLY > r.URY {
		r.LLY, r.URY = r.URY, r.LLY
	}
	return &r
}

// ContentBytes returns the concatenated, fully decoded content streams of the
// page. Multiple content streams are joined with a single space, per the spec.
func (p *Page) ContentBytes() ([]byte, error) {
	d := p.doc
	contents := d.Resolve(p.dict["Contents"])
	var streams []*Stream
	switch v := contents.(type) {
	case *Stream:
		streams = append(streams, v)
	case Array:
		for _, e := range v {
			if s := d.GetStream(e); s != nil {
				streams = append(streams, s)
			}
		}
	}
	var buf bytes.Buffer
	for i, s := range streams {
		data, imgF, err := d.DecodedStream(s)
		if err != nil {
			return nil, fmt.Errorf("pdf: page content stream %d: %w", i, err)
		}
		if imgF != "" {
			return nil, fmt.Errorf("pdf: page content stream %d unexpectedly image-encoded", i)
		}
		if i > 0 {
			buf.WriteByte('\n')
		}
		buf.Write(data)
	}
	return buf.Bytes(), nil
}
