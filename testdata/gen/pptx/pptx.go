// Package pptx generates deterministic .pptx fixtures for tests, mirroring
// the DOCX/XLSX generators: fixtures are readable Go, and two builds of the
// same fixture are byte-identical.
package pptx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"time"
)

// Builder assembles a minimal valid PresentationML package.
type Builder struct {
	slides []slide
	// layoutXML/masterXML back placeholder inheritance; defaults are minimal
	// valid parts with no placeholders.
	layoutXML, masterXML string
	// slideW/slideH are the slide size in EMU (default 16:9).
	slideW, slideH int64
	media          []mediaPart
}

type slide struct {
	xml    string
	hidden bool
	// imageRels lists media indices this slide references as rId100+i.
	imageRels []int
}

type mediaPart struct {
	name string // e.g. "image1.png"
	data []byte
}

// New returns an empty presentation builder (16:9 slide size).
func New() *Builder {
	return &Builder{slideW: 12192000, slideH: 6858000}
}

// SetSlideSize sets the slide size in EMU.
func (b *Builder) SetSlideSize(cx, cy int64) *Builder {
	b.slideW, b.slideH = cx, cy
	return b
}

// SetLayout sets the slideLayout1.xml part (raw XML) for placeholder
// inheritance tests.
func (b *Builder) SetLayout(xml string) *Builder { b.layoutXML = xml; return b }

// AddSlide appends a slide part (raw <p:sld> XML). imageRels lists indices
// into the added media, referenced from the slide XML as rId100, rId101, ...
func (b *Builder) AddSlide(xml string, imageRels ...int) *Builder {
	b.slides = append(b.slides, slide{xml: xml, imageRels: imageRels})
	return b
}

// AddMedia registers an image part (referenced from slides via AddSlide's
// imageRels), returning its index.
func (b *Builder) AddMedia(name string, data []byte) int {
	b.media = append(b.media, mediaPart{name: name, data: data})
	return len(b.media) - 1
}

const ctSlide = "application/vnd.openxmlformats-officedocument.presentationml.slide+xml"

// Bytes serializes the package deterministically.
func (b *Builder) Bytes() []byte {
	layout := b.layoutXML
	if layout == "" {
		layout = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sldLayout xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree/></p:cSld></p:sldLayout>`
	}
	master := b.masterXML
	if master == "" {
		master = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sldMaster xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree/></p:cSld><p:sldLayoutIdLst/></p:sldMaster>`
	}

	var sldIds, presRels strings.Builder
	for i := range b.slides {
		fmt.Fprintf(&sldIds, `<p:sldId id="%d" r:id="rId%d"/>`, 256+i, i+1)
		fmt.Fprintf(&presRels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide%d.xml"/>`+"\n", i+1, i+1)
	}
	presentation := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><p:sldIdLst>%s</p:sldIdLst><p:sldSz cx="%d" cy="%d"/></p:presentation>`,
		sldIds.String(), b.slideW, b.slideH)

	type part struct {
		name string
		data []byte
	}
	var parts []part
	add := func(name, data string) { parts = append(parts, part{name, []byte(data)}) }

	var ct strings.Builder
	ct.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Default Extension="png" ContentType="image/png"/>
<Default Extension="jpeg" ContentType="image/jpeg"/>
<Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/>
<Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/>
<Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/>
`)
	for i := range b.slides {
		fmt.Fprintf(&ct, `<Override PartName="/ppt/slides/slide%d.xml" ContentType="%s"/>`+"\n", i+1, ctSlide)
	}
	ct.WriteString("</Types>\n")
	add("[Content_Types].xml", ct.String())

	add("_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>
</Relationships>
`)
	add("ppt/presentation.xml", presentation)
	add("ppt/_rels/presentation.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
`+presRels.String()+`</Relationships>
`)
	add("ppt/slideLayouts/slideLayout1.xml", layout)
	add("ppt/slideLayouts/_rels/slideLayout1.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/slideMaster1.xml"/>
</Relationships>
`)
	add("ppt/slideMasters/slideMaster1.xml", master)
	for i, s := range b.slides {
		add(fmt.Sprintf("ppt/slides/slide%d.xml", i+1), s.xml)
		var rels strings.Builder
		rels.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rIdL" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>
`)
		for _, mi := range s.imageRels {
			fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/%s"/>`+"\n", 100+mi, b.media[mi].name)
		}
		rels.WriteString("</Relationships>\n")
		add(fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", i+1), rels.String())
	}
	for _, m := range b.media {
		parts = append(parts, part{"ppt/media/" + m.name, m.data})
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	stamp := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range parts {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: zip.Deflate, Modified: stamp})
		if err != nil {
			panic(err) // deterministic in-memory build
		}
		if _, err := f.Write(p.data); err != nil {
			panic(err)
		}
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
