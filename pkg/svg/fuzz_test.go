package svg

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// FuzzParse drives an SVG through XML parsing, the style cascade, and scene
// building -- the whole path Parse runs, including <use> instantiation, clip
// path and pattern resolution, and paint-server lookup.
//
// Parse returns a document for anything with a recognizable root, logging and
// truncating what it cannot use, so most malformed input is expected to succeed
// rather than error. The property under test is that it RETURNS: no panic, no
// unbounded recursion, no allocation sized by a number read out of the file.
//
// The scene is walked afterwards because building it is lazy in places, and a
// defect that only manifests when the tree is consumed would otherwise hide
// behind a successful Parse.
func FuzzParse(f *testing.F) {
	for _, s := range corpusSeeds() {
		f.Add(s)
	}
	for _, s := range hostileSVGSeeds() {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := Parse(data, nil)
		if err != nil || doc == nil {
			return
		}
		_, root := doc.Root()
		walkScene(root, 0)
		_ = doc.Intrinsic()
	})
}

// walkScene visits every node, bounding its own depth so the walk cannot be the
// thing that overflows -- the parser's nesting cap is what is under test, not
// this helper's recursion.
func walkScene(g *Group, depth int) {
	if g == nil || depth > 2000 {
		return
	}
	for _, kid := range g.Kids {
		if sub, ok := kid.(*Group); ok {
			walkScene(sub, depth+1)
		}
	}
}

// corpusSeeds returns a sample of the real resvg corpus: valid, structurally
// varied documents for the mutator to work from. A sample rather than all 867,
// so the seed corpus stays a reasonable size; they are sorted and strided so the
// selection is deterministic and spread across feature directories.
func corpusSeeds() [][]byte {
	var paths []string
	_ = filepath.Walk(filepath.Join("..", "..", "testdata", "svg"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".svg") {
			return nil //nolint:nilerr // an unreadable corpus is not a test failure
		}
		paths = append(paths, p)
		return nil
	})
	sort.Strings(paths)

	var out [][]byte
	const want = 40
	stride := max(len(paths)/want, 1)
	for i := 0; i < len(paths); i += stride {
		data, err := os.ReadFile(paths[i])
		if err != nil {
			continue
		}
		out = append(out, data)
	}
	return out
}

// hostileSVGSeeds returns documents aimed at the recursive and count-driven
// paths: <use> instantiation, clip-path and pattern chains, element nesting, and
// the numeric attributes that size work.
func hostileSVGSeeds() []string {
	const open = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="10" height="10">`
	return []string{
		open + `<rect width="5" height="5"/></svg>`,

		// Element nesting past the parser's 1024 cap.
		open + strings.Repeat("<g>", 5000) + `<rect width="1" height="1"/>` + strings.Repeat("</g>", 5000) + `</svg>`,
		// Unbalanced nesting: the same depth, reached through the error path.
		open + strings.Repeat("<g>", 5000) + `</svg>`,

		// <use> pointing at itself, and at its own ancestor.
		open + `<g id="a"><use xlink:href="#a"/></g></svg>`,
		open + `<use id="u" xlink:href="#u"/></svg>`,
		// A <use> chain: each instantiates the next.
		open + `<g id="a"><use xlink:href="#b"/></g><g id="b"><use xlink:href="#a"/></g></svg>`,

		// clip-path referring to itself, and a long chain.
		open + `<clipPath id="c" clip-path="url(#c)"><rect width="1" height="1"/></clipPath>` +
			`<rect width="5" height="5" clip-path="url(#c)"/></svg>`,
		open + `<pattern id="p" patternContentUnits="objectBoundingBox"><rect width="1" height="1" fill="url(#p)"/></pattern>` +
			`<rect width="5" height="5" fill="url(#p)"/></svg>`,

		// Gradients referring to each other via href.
		open + `<linearGradient id="g1" xlink:href="#g2"/><linearGradient id="g2" xlink:href="#g1"/>` +
			`<rect width="5" height="5" fill="url(#g1)"/></svg>`,

		// Numbers that size work: dash arrays, path data, huge coordinates.
		open + `<rect width="5" height="5" stroke-dasharray="` + strings.Repeat("1,", 10000) + `1"/></svg>`,
		open + `<path d="M0 0` + strings.Repeat(" L1 1", 20000) + `"/></svg>`,
		open + `<rect width="1e400" height="1e400"/></svg>`,
		open + `<rect width="99999999999999999999" height="1"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1e400 1e400" width="10" height="10"><rect width="1" height="1"/></svg>`,
		open + `<text font-size="1e400">x</text></svg>`,

		// Transform lists and nested transforms.
		open + `<g transform="` + strings.Repeat("translate(1,1) ", 10000) + `"><rect width="1" height="1"/></g></svg>`,

		// XML-layer edge cases: entities, unterminated tags, no root.
		`<?xml version="1.0"?><!DOCTYPE svg [<!ENTITY a "aaaaaaaaaa">]>` + open + `<rect width="&a;" height="1"/></svg>`,
		open + `<rect width="1" height="1"`,
		`<svg`,
		``,
		`not xml at all`,
	}
}
