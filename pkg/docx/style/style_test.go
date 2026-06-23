package style

import (
	"image/color"
	"testing"
	"time"

	"github.com/nathanstitt/doctaculous/pkg/docx"
)

func TestDirectOverridesDocDefault(t *testing.T) {
	d := &docx.Document{
		Styles: &docx.Styles{
			DocDefaultRun: docx.RunProps{SizeHalfPts: 20, HasSize: true},
			ByID:          map[string]*docx.Style{},
		},
	}
	r := NewResolver(d, nil)

	// No direct size -> inherits docDefault 20 half-points = 10pt.
	if got := r.EffectiveRun(docx.ParagraphProps{}, docx.RunProps{}).SizePt; got != 10 {
		t.Errorf("inherited size = %vpt, want 10", got)
	}
	// Direct size wins.
	run := docx.RunProps{SizeHalfPts: 28, HasSize: true}
	if got := r.EffectiveRun(docx.ParagraphProps{}, run).SizePt; got != 14 {
		t.Errorf("direct size = %vpt, want 14", got)
	}
}

func TestBasedOnInheritance(t *testing.T) {
	// Normal(size 22, family Calibri) <- Body(bold) <- Quote(italic).
	d := &docx.Document{
		Styles: &docx.Styles{
			ByID: map[string]*docx.Style{
				"Normal": {ID: "Normal", Type: "paragraph", Run: docx.RunProps{
					SizeHalfPts: 22, HasSize: true, Family: "Calibri",
				}},
				"Body": {ID: "Body", Type: "paragraph", BasedOn: "Normal", Run: docx.RunProps{
					Bold: true, HasBold: true,
				}},
				"Quote": {ID: "Quote", Type: "paragraph", BasedOn: "Body", Run: docx.RunProps{
					Italic: true, HasItalic: true,
				}},
			},
		},
	}
	r := NewResolver(d, nil)

	eff := r.EffectiveRun(docx.ParagraphProps{StyleID: "Quote"}, docx.RunProps{})
	if !eff.Bold {
		t.Error("Quote should inherit bold from Body")
	}
	if !eff.Italic {
		t.Error("Quote should be italic")
	}
	if eff.SizePt != 11 {
		t.Errorf("Quote size = %vpt, want 11 (inherited from Normal)", eff.SizePt)
	}
	if eff.Family != "Calibri" {
		t.Errorf("Quote family = %q, want Calibri (inherited)", eff.Family)
	}
}

func TestBasedOnCycleTerminates(t *testing.T) {
	// A <- B <- A : a deliberate cycle. NewResolver must not loop forever.
	d := &docx.Document{
		Styles: &docx.Styles{
			ByID: map[string]*docx.Style{
				"A": {ID: "A", Type: "paragraph", BasedOn: "B", Run: docx.RunProps{
					Bold: true, HasBold: true,
				}},
				"B": {ID: "B", Type: "paragraph", BasedOn: "A", Run: docx.RunProps{
					Italic: true, HasItalic: true,
				}},
			},
		},
	}
	done := make(chan struct{})
	var r *Resolver
	go func() {
		r = NewResolver(d, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("NewResolver did not terminate on a basedOn cycle")
	}
	// Both styles' own props still resolve (the cycle is just truncated).
	eff := r.EffectiveRun(docx.ParagraphProps{StyleID: "A"}, docx.RunProps{})
	if !eff.Bold {
		t.Error("style A should still carry its own bold")
	}
}

func TestParagraphAlignmentAndIndent(t *testing.T) {
	d := &docx.Document{Styles: &docx.Styles{ByID: map[string]*docx.Style{}}}
	r := NewResolver(d, nil)
	p := docx.ParagraphProps{
		Justify: docx.JustifyCenter, HasJustify: true,
		IndentLeft: 720, HasIndentLeft: true, // 0.5in = 36pt
	}
	eff := r.EffectiveParagraph(p)
	if eff.Justify != docx.JustifyCenter {
		t.Error("want centered")
	}
	if eff.IndentLeftPt != 36 {
		t.Errorf("indent = %vpt, want 36", eff.IndentLeftPt)
	}
}

func TestDefaultsWhenNoStyles(t *testing.T) {
	d := &docx.Document{} // no Styles part
	r := NewResolver(d, nil)
	eff := r.EffectiveRun(docx.ParagraphProps{}, docx.RunProps{})
	if eff.SizePt != 11 || eff.Family != "Calibri" {
		t.Errorf("defaults = %vpt %q, want 11pt Calibri", eff.SizePt, eff.Family)
	}
	if eff.Color != (color.RGBA{A: 0xff}) {
		t.Errorf("default color = %v, want opaque black", eff.Color)
	}
}
