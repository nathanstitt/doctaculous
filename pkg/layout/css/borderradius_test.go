package css

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
)

// TestUsedRadiiResolution covers percentage resolution and the §5.1 correction at
// the LAYOUT level, where the percentages meet the used border box.
func TestUsedRadiiResolution(t *testing.T) {
	tests := []struct {
		name    string
		w, h    float64
		style   string
		want    layout.CornerRadii
		comment string
	}{
		{
			name: "pixel radii pass through", w: 100, h: 100,
			style: "border-radius:10px",
			want:  uniformLayoutRadii(10),
		},
		{
			// THE classic bug: a horizontal semi-axis resolves against WIDTH, a
			// vertical one against HEIGHT. On a 200x100 box, 50% must give 100
			// horizontally and 50 vertically -- not 100 for both.
			name: "percentages use different bases per axis", w: 200, h: 100,
			style: "border-radius:50%",
			want: layout.CornerRadii{
				TL: [2]float64{100, 50}, TR: [2]float64{100, 50},
				BR: [2]float64{100, 50}, BL: [2]float64{100, 50},
			},
			comment: "50% of width=100, 50% of height=50",
		},
		{
			name: "over-large radii are corrected to a circle", w: 80, h: 80,
			style:   "border-radius:100px",
			want:    uniformLayoutRadii(40),
			comment: "f = 80/200 = 0.4 applied to all eight components",
		},
		{
			name: "the slash form is elliptical", w: 200, h: 200,
			style: "border-radius:40px / 20px",
			want: layout.CornerRadii{
				TL: [2]float64{40, 20}, TR: [2]float64{40, 20},
				BR: [2]float64{40, 20}, BL: [2]float64{40, 20},
			},
		},
		{
			name: "per-corner longhands", w: 200, h: 200,
			style: "border-top-left-radius:10px;border-bottom-right-radius:30px",
			want: layout.CornerRadii{
				TL: [2]float64{10, 10}, BR: [2]float64{30, 30},
			},
		},
		{
			name: "no radius stays square", w: 100, h: 100,
			style: "background:red",
			want:  layout.CornerRadii{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := layoutRadii(t, tt.w, tt.h, tt.style)
			if !nearRadii(got, tt.want, 1e-6) {
				t.Errorf("radii = %v, want %v (%s)", got, tt.want, tt.comment)
			}
		})
	}
}

// TestRoundedBorderEmitsOneRing checks the item stream: a rounded box emits ONE
// BorderKind carrying a ring, never four strips (whose square corners cannot draw
// an arc), while a square box still emits its strips exactly as before.
func TestRoundedBorderEmitsOneRing(t *testing.T) {
	rounded := itemsFor(t, 100, 100, "width:80px;height:80px;border:5px solid black;border-radius:20px")
	var ringItems, stripItems int
	for _, it := range rounded {
		if it.Kind != layout.BorderKind {
			continue
		}
		if it.Border.Ring != nil {
			ringItems++
		} else {
			stripItems++
		}
	}
	if ringItems != 1 || stripItems != 0 {
		t.Errorf("rounded border emitted %d ring(s) and %d strip(s), want exactly 1 ring and 0 strips",
			ringItems, stripItems)
	}

	square := itemsFor(t, 100, 100, "width:80px;height:80px;border:5px solid black")
	ringItems, stripItems = 0, 0
	for _, it := range square {
		if it.Kind != layout.BorderKind {
			continue
		}
		if it.Border.Ring != nil {
			ringItems++
		} else {
			stripItems++
		}
	}
	if ringItems != 0 || stripItems != 4 {
		t.Errorf("square border emitted %d ring(s) and %d strip(s), want 0 rings and 4 strips (unchanged)",
			ringItems, stripItems)
	}
}

// TestRoundedBorderRingInnerRadii checks the ring's inner curve is the outer curve
// inset by the border widths and floored at zero (Backgrounds 3 §5.2).
func TestRoundedBorderRingInnerRadii(t *testing.T) {
	items := itemsFor(t, 200, 200, "width:80px;height:80px;border:5px solid black;border-radius:20px")
	ring := findRing(t, items)
	want := uniformLayoutRadii(15) // 20 - 5 on both axes of every corner
	if !nearRadii(ring.Inner, want, 1e-6) {
		t.Errorf("inner radii = %v, want %v", ring.Inner, want)
	}

	// A border thicker than the radius squares the inner corner while the outer
	// stays round -- the shape a stroke of the outer path cannot produce.
	items = itemsFor(t, 200, 200, "width:80px;height:80px;border:30px solid black;border-radius:10px")
	ring = findRing(t, items)
	if !ring.Inner.Zero() {
		t.Errorf("a border thicker than the radius should square the inner corner, got %v", ring.Inner)
	}
	if ring.Outer.Zero() {
		t.Error("the OUTER corner must stay rounded")
	}
}

// TestRoundedBackgroundImageIsClipped checks a rounded box brackets its background
// IMAGE with a rounded clip: DrawImage has no shape parameter, so the clip is the
// only thing that can round a tiled background.
func TestRoundedBackgroundImageIsClipped(t *testing.T) {
	itemsWithRadius := func(radius string) []layout.Item {
		root := layoutWithLoader(t,
			`<body><div style="width:80px;height:80px;`+radius+
				`background-image:url(img.png)"></div></body>`,
			800, pngLoader(t, 8, 8), nil)
		return root.AppendItems(nil)
	}

	// The rounded box brackets its tiled background with a ROUNDED clip.
	var pushes, roundedPushes, pops int
	for _, it := range itemsWithRadius("border-radius:20px;") {
		switch it.Kind {
		case layout.ClipPushKind:
			pushes++
			if !it.Rule.Radii.Zero() {
				roundedPushes++
			}
		case layout.ClipPopKind:
			pops++
		}
	}
	if roundedPushes != 1 {
		t.Errorf("rounded background image should push exactly 1 rounded clip, got %d (of %d pushes)",
			roundedPushes, pushes)
	}
	if pushes != pops {
		t.Errorf("clip brackets unbalanced: %d pushes, %d pops", pushes, pops)
	}

	// The SQUARE box must not gain a clip bracket it did not have before.
	pushes = 0
	for _, it := range itemsWithRadius("") {
		if it.Kind == layout.ClipPushKind {
			pushes++
		}
	}
	if pushes != 0 {
		t.Errorf("a square box with a background image should push no clip, got %d", pushes)
	}
}

// TestRoundedBorderDegradationLogged pins the honest-degradation contract: a
// rounded border whose style is not solid, or whose per-side colours differ, is
// approximated as a solid single-colour ring AND says so. A plain solid rounded
// border must stay silent (a warning on every rounded box would be noise).
func TestRoundedBorderDegradationLogged(t *testing.T) {
	tests := []struct {
		name      string
		style     string
		wantMatch string // "" => expect silence
	}{
		{
			name:      "dashed rounded border is logged",
			style:     "width:80px;height:80px;border:5px dashed black;border-radius:20px",
			wantMatch: "rounded border painted solid",
		},
		{
			name: "per-side colours are logged",
			style: "width:80px;height:80px;border:5px solid black;border-top-color:red;" +
				"border-radius:20px",
			wantMatch: "rounded border painted in one colour",
		},
		{
			name:  "a plain solid rounded border is silent",
			style: "width:80px;height:80px;border:5px solid black;border-radius:20px",
		},
		{
			name:  "a dashed SQUARE border is silent (strips still fully styled)",
			style: "width:80px;height:80px;border:5px dashed black",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, msgs := itemsAndLogs(t, 200, 200, tt.style)
			joined := strings.Join(msgs, "\n")
			if tt.wantMatch == "" {
				if strings.Contains(joined, "rounded border") {
					t.Errorf("expected silence, got:\n%s", joined)
				}
				return
			}
			if !strings.Contains(joined, tt.wantMatch) {
				t.Errorf("expected a log containing %q, got:\n%s", tt.wantMatch, joined)
			}
		})
	}
}

// --- helpers ---

// itemsFor lays out a single styled <div> and returns the first page's items.
func itemsFor(t *testing.T, pw, ph float64, style string) []layout.Item {
	t.Helper()
	items, _ := itemsAndLogs(t, pw, ph, style)
	return items
}

// itemsAndLogs is itemsFor plus everything the engine logged during layout.
func itemsAndLogs(t *testing.T, pw, ph float64, style string) ([]layout.Item, []string) {
	t.Helper()
	var msgs []string
	logf := func(f string, a ...any) { msgs = append(msgs, fmt.Sprintf(f, a...)) }
	src := `<html><body style="margin:0"><div style="` + style + `"></div></body></html>`
	root := buildRoot(t, src, logf)
	pages, err := New(nil, nil, logf).LayoutPaged(context.Background(), root, pw, ph)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) == 0 {
		t.Fatal("layout produced no pages")
	}
	return pages.Pages[0].Items, msgs
}

// layoutRadii returns the radii the engine resolved for the styled div, read back
// off whichever painted item carries them (the background fill, else the border
// ring, else the rounded clip). Reading them from the ITEM STREAM rather than from
// a fragment field is deliberate: it proves the resolved radii actually reach the
// painter, which is the only thing that matters.
func layoutRadii(t *testing.T, w, h float64, style string) layout.CornerRadii {
	t.Helper()
	full := fmt.Sprintf("width:%gpx;height:%gpx;background:black;%s", w, h, style)
	for _, it := range itemsFor(t, w+200, h+200, full) {
		if it.Kind == layout.BackgroundKind {
			return it.Rule.Radii
		}
	}
	t.Fatal("no background item found; cannot read resolved radii")
	return layout.CornerRadii{}
}

func findRing(t *testing.T, items []layout.Item) *layout.BorderRing {
	t.Helper()
	for _, it := range items {
		if it.Kind == layout.BorderKind && it.Border.Ring != nil {
			return it.Border.Ring
		}
	}
	t.Fatal("no border ring item found")
	return nil
}

func uniformLayoutRadii(r float64) layout.CornerRadii {
	c := [2]float64{r, r}
	return layout.CornerRadii{TL: c, TR: c, BR: c, BL: c}
}

func nearRadii(a, b layout.CornerRadii, tol float64) bool {
	for _, p := range [][2][2]float64{{a.TL, b.TL}, {a.TR, b.TR}, {a.BR, b.BR}, {a.BL, b.BL}} {
		if math.Abs(p[0][0]-p[1][0]) > tol || math.Abs(p[0][1]-p[1][1]) > tol {
			return false
		}
	}
	return true
}
