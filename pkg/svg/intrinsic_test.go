package svg

import "testing"

// TestIntrinsicReportsPreDefaultSizing pins down that Document.Intrinsic()
// reports the sizing facts BEFORE resolveSize applies CSS's 300x150 default and
// the viewBox-extent fallback.
//
// The distinction matters only for an EMBEDDED SVG. Standalone, the SVG is its
// own sizing authority and WidthPt/HeightPt is the right answer. Embedded in an
// <img>, the host's CSS is the authority and the SVG must contribute only what it
// actually states: an absolute size, an aspect ratio, or nothing. Folding those
// three into one defaulted pair — which is all WidthPt/HeightPt can express —
// makes `<img src="ratio.svg" width="600">` size 600x150 instead of 600x300.
func TestIntrinsicReportsPreDefaultSizing(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want IntrinsicSize
		// wantResolved is what WidthPt/HeightPt says, recorded to show the two
		// genuinely differ (otherwise the accessor would be redundant).
		wantResolvedW, wantResolvedH float64
	}{{
		name:          "explicit width and height, no viewBox",
		src:           `<svg xmlns="http://www.w3.org/2000/svg" width="120" height="60"/>`,
		want:          IntrinsicSize{Width: 120, HasWidth: true, Height: 60, HasHeight: true},
		wantResolvedW: 120, wantResolvedH: 60,
	}, {
		name: "viewBox only: a ratio, and NO absolute size",
		src:  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 50"/>`,
		want: IntrinsicSize{RatioW: 100, RatioH: 50, HasRatio: true},
		// resolveSize already collapsed this to the viewBox extent; Intrinsic
		// keeps "no stated size" distinguishable from "stated 100x50".
		wantResolvedW: 100, wantResolvedH: 50,
	}, {
		name: "neither size nor viewBox: nothing at all",
		src:  `<svg xmlns="http://www.w3.org/2000/svg"/>`,
		want: IntrinsicSize{},
		// THE case the accessor exists for: resolveSize reports the CSS default,
		// which an embedded SVG must not be told is an intrinsic size.
		wantResolvedW: defaultWidth, wantResolvedH: defaultHeight,
	}, {
		name:          "width attribute plus a viewBox: both a size and a ratio",
		src:           `<svg xmlns="http://www.w3.org/2000/svg" width="80" viewBox="0 0 4 1"/>`,
		want:          IntrinsicSize{Width: 80, HasWidth: true, RatioW: 4, RatioH: 1, HasRatio: true},
		wantResolvedW: 80, wantResolvedH: 20,
	}, {
		name:          "percentage width is not an absolute size",
		src:           `<svg xmlns="http://www.w3.org/2000/svg" width="50%" height="50%" viewBox="0 0 20 10"/>`,
		want:          IntrinsicSize{RatioW: 20, RatioH: 10, HasRatio: true},
		wantResolvedW: 20, wantResolvedH: 10,
	}, {
		name:          "a zero-extent viewBox yields no usable ratio",
		src:           `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 0 0"/>`,
		want:          IntrinsicSize{},
		wantResolvedW: defaultWidth, wantResolvedH: defaultHeight,
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := Parse([]byte(tc.src), nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := doc.Intrinsic(); got != tc.want {
				t.Errorf("Intrinsic() = %+v, want %+v", got, tc.want)
			}
			if doc.WidthPt != tc.wantResolvedW || doc.HeightPt != tc.wantResolvedH {
				t.Errorf("resolved size = %gx%g, want %gx%g (the standalone answer, unchanged)",
					doc.WidthPt, doc.HeightPt, tc.wantResolvedW, tc.wantResolvedH)
			}
		})
	}
}
