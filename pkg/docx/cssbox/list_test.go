package cssbox

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func numberedDoc() *docx.Document {
	num, _ := docxParseNumbering()
	return &docx.Document{
		Section:   docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Numbering: num,
		Body: []docx.Block{
			listItemBlock(1, 0, "first"),
			listItemBlock(1, 0, "second"),
		},
	}
}

func listItemBlock(numID, ilvl int, text string) docx.Block {
	return docx.Block{Paragraph: &docx.Paragraph{
		Props:   docx.ParagraphProps{NumID: numID, ILvl: ilvl, HasNum: true},
		Content: []docx.ParaChild{{Run: &docx.Run{Text: text}}},
	}}
}

func TestLowerDecimalListNumbersIncrement(t *testing.T) {
	d := numberedDoc()
	root := lowerDoc(t, d)
	body := root.Children[len(root.Children)-1]
	// Consecutive list items are grouped under one container box.
	if len(body.Children) != 1 {
		t.Fatalf("body children = %d, want 1 list container", len(body.Children))
	}
	list := body.Children[0]
	if len(list.Children) != 2 {
		t.Fatalf("list children = %d, want 2 list items", len(list.Children))
	}
	i0, i1 := list.Children[0], list.Children[1]
	if i0.Display != lcssbox.DisplayListItem {
		t.Fatalf("item0 Display = %v, want DisplayListItem", i0.Display)
	}
	if i0.Marker == nil || i0.Marker.Text != "1. " {
		t.Fatalf("item0 Marker = %+v, want '1. '", i0.Marker)
	}
	if i1.Marker == nil || i1.Marker.Text != "2. " {
		t.Fatalf("item1 Marker = %+v, want '2. '", i1.Marker)
	}
}

func TestLowerBulletListMarker(t *testing.T) {
	// numId 2 -> a bullet abstract; build a numbering with a bullet level.
	num, _ := docxParseBulletNumbering()
	d := &docx.Document{
		Section:   docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Numbering: num,
		Body:      []docx.Block{listItemBlock(2, 0, "bulleted")},
	}
	root := lowerDoc(t, d)
	body := root.Children[len(root.Children)-1]
	if got := body.Children[0].Children[0].Marker.Text; got != "• " {
		t.Fatalf("bullet marker = %q, want '• '", got)
	}
}

func TestLowerListPrependsMarkerText(t *testing.T) {
	// A decimal-numbered item must render its marker: the list-item box's FIRST
	// child is a real inline text box carrying the marker "1. " (the DOCX path skips
	// the HTML resolveCounters pass, so the marker has to be lowered as a child here).
	num, _ := docxParseNumbering()
	d := &docx.Document{
		Section:   docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Numbering: num,
		Body:      []docx.Block{listItemBlock(1, 0, "First item")},
	}
	root := lowerDoc(t, d)
	body := root.Children[len(root.Children)-1]
	item := body.Children[0].Children[0]
	if item.Display != lcssbox.DisplayListItem {
		t.Fatalf("item Display = %v, want DisplayListItem", item.Display)
	}
	if len(item.Children) < 2 {
		t.Fatalf("item children = %d, want the marker box + content", len(item.Children))
	}
	marker := item.Children[0]
	if marker.Kind != lcssbox.BoxText {
		t.Fatalf("marker Kind = %v, want BoxText", marker.Kind)
	}
	if marker.Display != lcssbox.DisplayInline {
		t.Fatalf("marker Display = %v, want DisplayInline", marker.Display)
	}
	if marker.Text != "1. " {
		t.Fatalf("marker Text = %q, want %q", marker.Text, "1. ")
	}
	// The item content ("First item") must still be present as a later child.
	var haveContent bool
	for _, c := range item.Children[1:] {
		if c.Kind == lcssbox.BoxText && c.Text == "First item" {
			haveContent = true
			break
		}
	}
	if !haveContent {
		t.Fatalf("item content %q missing after the prepended marker; children = %+v", "First item", item.Children)
	}
}

func docxParseNumbering() (*docx.Numbering, error) {
	return docx.ParseNumberingForTest([]byte(`<?xml version="1.0"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/></w:lvl></w:abstractNum>
  <w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
</w:numbering>`))
}

func docxParseBulletNumbering() (*docx.Numbering, error) {
	return docx.ParseNumberingForTest([]byte(`<?xml version="1.0"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="&#8226;"/></w:lvl></w:abstractNum>
  <w:num w:numId="2"><w:abstractNumId w:val="0"/></w:num>
</w:numbering>`))
}
