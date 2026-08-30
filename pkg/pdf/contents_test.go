package pdf

import (
	"strings"
	"testing"

	"github.com/nathanstitt/omnidoc/testdata/gen"
)

// TestContentBytesDistinguishesBlankFromBroken covers the difference between a
// page that HAS no content and a page whose content cannot be read.
//
// Both used to return no bytes and no error, which is how a broken PDF converted
// to an empty output file with exit code 0 -- the caller had no way to tell "this
// page is blank" from "this page's content is gone". A blank page is legal and
// common; an unreadable one is a failure and now says so.
func TestContentBytesDistinguishesBlankFromBroken(t *testing.T) {
	const head = "%PDF-1.7\n" +
		"1 0 obj <</Type/Catalog/Pages 2 0 R>> endobj\n" +
		"2 0 obj <</Type/Pages/Kids[3 0 R]/Count 1>> endobj\n"
	const tail = "trailer <</Root 1 0 R/Size 5>>\n"

	cases := []struct {
		name    string
		pageObj string
		extra   string
		wantErr string // "" means no error expected
	}{
		{
			name:    "no /Contents at all is a blank page",
			pageObj: "3 0 obj <</Type/Page/Parent 2 0 R/MediaBox[0 0 10 10]>> endobj\n",
		},
		{
			name:    "empty /Contents array is a blank page",
			pageObj: "3 0 obj <</Type/Page/Parent 2 0 R/MediaBox[0 0 10 10]/Contents[]>> endobj\n",
		},
		{
			name:    "dangling /Contents reference is an error",
			pageObj: "3 0 obj <</Type/Page/Parent 2 0 R/MediaBox[0 0 10 10]/Contents 9 0 R>> endobj\n",
			wantErr: "not a stream or array",
		},
		{
			name:    "/Contents array of dangling references is an error",
			pageObj: "3 0 obj <</Type/Page/Parent 2 0 R/MediaBox[0 0 10 10]/Contents[9 0 R 8 0 R]>> endobj\n",
			wantErr: "no readable streams",
		},
		{
			name:    "/Contents of the wrong type is an error",
			pageObj: "3 0 obj <</Type/Page/Parent 2 0 R/MediaBox[0 0 10 10]/Contents 42>> endobj\n",
			wantErr: "not a stream or array",
		},
		{
			name: "a real content stream reads normally",
			pageObj: "3 0 obj <</Type/Page/Parent 2 0 R/MediaBox[0 0 10 10]/Contents 4 0 R>> endobj\n" +
				"4 0 obj <</Length 12>> stream\n0 0 m 5 5 l\nendstream endobj\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, err := Parse([]byte(head + c.pageObj + c.extra + tail))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			pg, err := doc.Page(0)
			if err != nil {
				t.Fatalf("Page(0): %v", err)
			}
			data, err := pg.ContentBytes()
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("ContentBytes: unexpected error %v", err)
			case c.wantErr != "" && err == nil:
				t.Fatalf("ContentBytes returned %d bytes and no error; want an error mentioning %q",
					len(data), c.wantErr)
			case c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr):
				t.Errorf("error = %v, want it to mention %q", err, c.wantErr)
			}
		})
	}
}

// TestContentBytesRealCorpusUnaffected guards the change against over-reach: the
// error paths above must never fire on a document a real producer wrote. Every
// core fixture's pages must still read cleanly.
func TestContentBytesRealCorpusUnaffected(t *testing.T) {
	for _, fx := range gen.Core {
		doc, err := Parse(fx.Bytes())
		if err != nil {
			t.Errorf("%s: Parse: %v", fx.Name, err)
			continue
		}
		for i := range doc.PageCount() {
			pg, err := doc.Page(i)
			if err != nil {
				continue
			}
			if _, err := pg.ContentBytes(); err != nil {
				t.Errorf("%s page %d: ContentBytes: %v", fx.Name, i, err)
			}
		}
	}
}
