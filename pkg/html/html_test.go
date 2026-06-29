package html

import (
	"strings"
	"testing"
)

func TestParseBuildsOwnedTree(t *testing.T) {
	doc, err := Parse([]byte(`<html><body><p id="x" class="a b">hi</p></body></html>`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Root.Tag() != "html" {
		t.Fatalf("root = %q, want html", doc.Root.Tag())
	}
	body := firstChildElement(doc.Root, "body")
	if body == nil {
		t.Fatal("no body")
	}
	p := firstChildElement(body, "p")
	if p == nil {
		t.Fatal("no p")
	}
	if p.ID() != "x" || len(p.Classes()) != 2 || p.Classes()[1] != "b" {
		t.Errorf("p id/classes = %q/%v", p.ID(), p.Classes())
	}
	if p.ParentElement() != body {
		t.Error("p parent should be body")
	}
	var txt *Text
	for _, c := range p.Children() {
		if tn, ok := c.(*Text); ok {
			txt = tn
		}
	}
	if txt == nil || strings.TrimSpace(txt.Data) != "hi" {
		t.Errorf("text child = %+v", txt)
	}
}

func TestParseCollectsStyleSheets(t *testing.T) {
	doc, err := Parse([]byte(`<html><head><style>p{color:red}</style><style>div{color:blue}</style></head><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.StyleSheets) != 2 {
		t.Fatalf("got %d stylesheets, want 2", len(doc.StyleSheets))
	}
	if len(doc.StyleSheets[0].Rules) != 1 || doc.StyleSheets[0].Rules[0].Declarations[0].Value != "red" {
		t.Errorf("first sheet = %+v", doc.StyleSheets[0])
	}
}

func TestParseSkipsBlankStyle(t *testing.T) {
	// A whitespace-only <style> must not produce a stylesheet (the TrimSpace guard).
	doc, err := Parse([]byte("<html><head><style>   \n\t </style><style>p{color:red}</style></head><body></body></html>"))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.StyleSheets) != 1 {
		t.Errorf("got %d stylesheets, want 1 (blank <style> skipped)", len(doc.StyleSheets))
	}
}

func TestParseCollectsLinkRefs(t *testing.T) {
	doc, err := Parse([]byte(`<html><head><link rel="stylesheet" href="a.css"><link rel="icon" href="favicon.ico"></head><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.LinkRefs) != 1 || doc.LinkRefs[0] != "a.css" {
		t.Errorf("LinkRefs = %v, want [a.css] (only rel=stylesheet)", doc.LinkRefs)
	}
}

func TestParseLinkRelIsCaseInsensitive(t *testing.T) {
	// rel matching is case-insensitive; an href-less stylesheet link is ignored.
	doc, err := Parse([]byte(`<html><head><link rel="StyleSheet" href="up.css"><link rel="stylesheet"></head><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.LinkRefs) != 1 || doc.LinkRefs[0] != "up.css" {
		t.Errorf("LinkRefs = %v, want [up.css] (case-insensitive rel; href-less link skipped)", doc.LinkRefs)
	}
}

func TestParseLeavesInlineStyleOnElement(t *testing.T) {
	doc, err := Parse([]byte(`<html><body><p style="color:red">x</p></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	body := firstChildElement(doc.Root, "body")
	p := firstChildElement(body, "p")
	if v, ok := p.Attr("style"); !ok || v != "color:red" {
		t.Errorf("inline style = %q,%v", v, ok)
	}
}

func TestParseMalformedDoesNotPanic(t *testing.T) {
	inputs := []string{
		`<html><body><p>open`,
		`<div><span></div></span>`,
		``,
		`just text no tags`,
		`<<<>>>`,
	}
	for _, in := range inputs {
		doc, err := Parse([]byte(in))
		if err != nil {
			t.Errorf("Parse(%q) errored: %v", in, err)
			continue
		}
		if doc.Root == nil {
			t.Errorf("Parse(%q) gave nil root", in)
		}
	}
}

// firstChildElement returns the first direct child element with the given tag.
func firstChildElement(e *Element, tag string) *Element {
	for _, c := range e.Children() {
		if el, ok := c.(*Element); ok && el.Tag() == tag {
			return el
		}
	}
	return nil
}
