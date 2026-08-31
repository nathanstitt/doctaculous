package svg

import "testing"

func TestCascadeOrdering(t *testing.T) {
	resolveFor := func(src string, want map[string]string) {
		t.Helper()
		root, err := parseXML([]byte(src), nil)
		if err != nil {
			t.Fatal(err)
		}
		idx := buildIndex(root, func(string, string) {})
		ctx := &cascadeCtx{idx: idx}
		// The <rect> under test is always the last child of the root.
		var target *element
		for _, k := range root.kids {
			if k.local == "rect" {
				target = k
			}
		}
		if target == nil {
			t.Fatalf("no rect in %q", src)
		}
		lookup := ctx.resolve(target)
		for prop, exp := range want {
			got, ok := lookup(prop)
			if !ok || got != exp {
				t.Errorf("%s = %q,%v; want %q\nsrc: %s", prop, got, ok, exp, src)
			}
		}
	}
	const ns = `xmlns="http://www.w3.org/2000/svg"`

	// Sheet rule beats a presentation attribute.
	resolveFor(`<svg `+ns+`><style>rect{fill:blue}</style><rect fill="red"/></svg>`,
		map[string]string{"fill": "blue"})

	// Inline style beats a sheet rule.
	resolveFor(`<svg `+ns+`><style>rect{fill:blue}</style><rect fill="red" style="fill:lime"/></svg>`,
		map[string]string{"fill": "lime"})

	// Presentation attribute survives when nothing else sets that property.
	resolveFor(`<svg `+ns+`><style>rect{stroke:black}</style><rect fill="red"/></svg>`,
		map[string]string{"fill": "red", "stroke": "black"})

	// Specificity: .cls beats bare type even though the type rule is later.
	resolveFor(`<svg `+ns+`><style>.c{fill:lime} rect{fill:blue}</style><rect class="c"/></svg>`,
		map[string]string{"fill": "lime"})

	// Source order breaks a specificity tie: later wins.
	resolveFor(`<svg `+ns+`><style>rect{fill:blue} rect{fill:lime}</style><rect/></svg>`,
		map[string]string{"fill": "lime"})

	// !important in a sheet beats a normal inline style.
	resolveFor(`<svg `+ns+`><style>rect{fill:blue!important}</style><rect style="fill:lime"/></svg>`,
		map[string]string{"fill": "blue"})

	// Inline !important beats sheet !important.
	resolveFor(`<svg `+ns+`><style>rect{fill:blue!important}</style><rect style="fill:lime!important"/></svg>`,
		map[string]string{"fill": "lime"})

	// A descendant selector matches through the parent chain.
	resolveFor(`<svg `+ns+`><style>svg rect{fill:lime}</style><rect fill="red"/></svg>`,
		map[string]string{"fill": "lime"})

	// id beats class.
	resolveFor(`<svg `+ns+`><style>.c{fill:blue} #r{fill:lime}</style><rect id="r" class="c"/></svg>`,
		map[string]string{"fill": "lime"})
}

func TestCascadeNilContextFallsBackToAttributes(t *testing.T) {
	root, _ := parseXML([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect fill="red"/></svg>`), nil)
	var ctx *cascadeCtx
	lookup := ctx.resolve(root.kids[0])
	if v, ok := lookup("fill"); !ok || v != "red" {
		t.Errorf("nil ctx must fall back to presentation attributes; got %q,%v", v, ok)
	}
}
