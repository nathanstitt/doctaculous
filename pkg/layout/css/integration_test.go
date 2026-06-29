package css

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// TestEndToEndBoxTree exercises a realistic document through parse -> cascade ->
// box generation -> normalization, asserting the overall tree shape the layout
// engine (sub-project 3) will consume.
func TestEndToEndBoxTree(t *testing.T) {
	src := `<!doctype html>
<html>
  <head>
    <title>t</title>
    <style>
      .lead { color: rgb(10, 20, 30); }
      em { display: inline; } /* redundant (inline is the default) but confirms the cascade keeps em inline */
    </style>
    <link rel="stylesheet" href="ext.css">
  </head>
  <body>
    <h1>Title</h1>
    <p class="lead">Hello <em>world</em>, this is text.</p>
    <div>before<p>nested</p>after</div>
    <img src="pic.png" alt="pic">
  </body>
</html>`

	loader := resource.MapLoader{
		"ext.css": {Data: []byte(`h1 { color: rgb(1,2,3); }`), ContentType: "text/css"},
	}
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	root, err := Build(context.Background(), doc, loader, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// head was display:none -> pruned; html has exactly one child (body).
	if len(root.Children) != 1 {
		t.Fatalf("html children = %d, want 1 (body): %s", len(root.Children), dump(root))
	}
	body := root.Children[0]

	// body children: h1 (block), p (block), div (block), and the inline <img>
	// wrapped in an anon block (see the img assertion below for why).
	if len(body.Children) != 4 {
		t.Fatalf("body children = %d, want 4: %s", len(body.Children), dump(body))
	}

	h1 := body.Children[0]
	if h1.Kind != cssbox.BoxBlock || !h1.Style.Bold || h1.Style.FontSizePt <= 16 {
		t.Errorf("h1 wrong: kind=%v bold=%v size=%v", h1.Kind, h1.Style.Bold, h1.Style.FontSizePt)
	}
	// h1 color from the linked sheet (author) overriding UA:
	if h1.Style.Color.R != 1 || h1.Style.Color.G != 2 || h1.Style.Color.B != 3 {
		t.Errorf("h1 color = %v, want rgb(1,2,3) from ext.css", h1.Style.Color)
	}

	p := body.Children[1]
	if p.Kind != cssbox.BoxBlock {
		t.Errorf("p kind = %v, want block", p.Kind)
	}
	if p.Style.Color.R != 10 || p.Style.Color.G != 20 || p.Style.Color.B != 30 {
		t.Errorf("p.lead color = %v, want rgb(10,20,30)", p.Style.Color)
	}
	// p has all-inline content -> inline children, no anonymous blocks.
	if len(p.Children) == 0 {
		t.Errorf("p should have inline children (text + em), got none: %s", dump(p))
	}
	for _, c := range p.Children {
		if c.Kind == cssbox.BoxAnonBlock {
			t.Errorf("p should have no anon blocks: %s", dump(p))
		}
	}

	div := body.Children[2]
	// div has mixed content -> [AnonBlock, Block(nested p), AnonBlock].
	if len(div.Children) != 3 ||
		div.Children[0].Kind != cssbox.BoxAnonBlock ||
		div.Children[1].Kind != cssbox.BoxBlock ||
		div.Children[2].Kind != cssbox.BoxAnonBlock {
		t.Errorf("div anon-wrapping wrong: %s", dump(div))
	}

	// <img> has no UA display rule, so it is inline-level (replaced) by default.
	// Among block siblings (h1/p/div) it is the lone inline-level child, so the
	// all-block-or-all-inline normalization wraps it in an anonymous block (the
	// same wrapping the inline text runs in <div> get). The replaced leaf lives
	// inside that anon block.
	imgWrap := body.Children[3]
	if imgWrap.Kind != cssbox.BoxAnonBlock || len(imgWrap.Children) != 1 {
		t.Fatalf("img should be wrapped in an anon block: %s", dump(body))
	}
	img := imgWrap.Children[0]
	if img.Kind != cssbox.BoxReplaced || img.Replaced == nil || img.Replaced.Attrs["src"] != "pic.png" {
		t.Errorf("img replaced box wrong: %+v", img.Replaced)
	}
}
