package rtf

import (
	"errors"
	"strings"
	"testing"
)

func convertOK(t *testing.T, src string) string {
	t.Helper()
	out, err := ToHTML([]byte(src), nil)
	if err != nil {
		t.Fatalf("ToHTML: %v", err)
	}
	return out
}

func wants(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q\n---\n%s", w, out)
		}
	}
}

func rejects(t *testing.T, out string, rejects ...string) {
	t.Helper()
	for _, r := range rejects {
		if strings.Contains(out, r) {
			t.Errorf("output must not contain %q\n---\n%s", r, out)
		}
	}
}

func TestParagraphsAndEmphasis(t *testing.T) {
	out := convertOK(t, `{\rtf1\ansi
{\b bold} plain {\i italic \i0 upright}\par
\ul underlined\ulnone\strike struck\strike0 done\par
}`)
	wants(t, out,
		"<b>bold</b>",
		"<i>italic </i>",
		"<u>underlined</u>",
		"<s>struck</s>",
		"done",
	)
	rejects(t, out, "<b>plain", "<i>upright")
}

func TestFontColorSizeTables(t *testing.T) {
	out := convertOK(t, `{\rtf1\ansi\deff0
{\fonttbl{\f0\froman Times New Roman;}{\f1\fmodern Courier New;}}
{\colortbl ;\red255\green0\blue0;\red0\green128\blue0;}
{\f1\fs20\cf1 red courier ten}\par
{\cf2\highlight1 green on red}\par
}`)
	wants(t, out,
		// A monospace font selection reads as inline code (the semantic our own
		// writer round-trips); non-mono fonts stay font-family spans.
		"<code>red courier ten</code>",
		"font-size:10pt",
		"color:#FF0000",
		"color:#008000",
		"background-color:#FF0000",
	)
	rejects(t, out, "font-family:'Courier New'")
}

func TestAlignmentAndIndents(t *testing.T) {
	out := convertOK(t, `{\rtf1\ansi
\qc centered\par
\pard\qr right side\par
\pard\li720\fi-360\sb120\sa60 indented\par
}`)
	wants(t, out,
		"text-align:center",
		"text-align:right",
		"margin-left:36pt",
		"text-indent:-18pt",
		"margin-top:6pt",
		"margin-bottom:3pt",
	)
}

func TestEscapesAndUnicode(t *testing.T) {
	out := convertOK(t, `{\rtf1\ansi\uc1
caf\'e9 \'93quoted\'94\par
\u26085?\u26412? unicode with fallback\par
\bullet\endash\emdash\lquote\rquote\ldblquote\rdblquote\~x\par
}`)
	wants(t, out,
		"café",
		"“quoted”",
		"日本 unicode",
		"•", "–", "—", "‘", "’", " x",
	)
	rejects(t, out, "日?", "?本") // the \uN fallback '?' must be swallowed
}

func TestHyperlinkField(t *testing.T) {
	out := convertOK(t, `{\rtf1\ansi
Visit {\field{\*\fldinst HYPERLINK "https://example.com/a?b=1"}{\fldrslt the site}} now.\par
}`)
	wants(t, out, `<a href="https://example.com/a?b=1">`, "the site</a>", "Visit", "now.")
}

func TestTable(t *testing.T) {
	out := convertOK(t, `{\rtf1\ansi
\trowd\cellx2000\cellx4000
\intbl A1\cell B1\cell\row
\trowd\cellx2000\cellx4000
\intbl A2\cell B2\cell\row
\pard after table\par
}`)
	wants(t, out, "<table", "<tr>", "A1", "B1", "A2", "B2", "after table")
	if strings.Count(out, "<tr>") != 2 || strings.Count(out, "<td") != 4 {
		t.Errorf("table shape wrong:\n%s", out)
	}
	// The table must be emitted BEFORE the following paragraph.
	if strings.Index(out, "<table") > strings.Index(out, "after table") {
		t.Errorf("table emitted after the trailing paragraph")
	}
}

func TestPictureDataURI(t *testing.T) {
	// A 1x1 PNG, hex-encoded the way RTF embeds it.
	png := "89504e470d0a1a0a0000000d4948445200000001000000010802000000907753de0000000c4944415408d763f8cfc000000301010018dd8db00000000049454e44ae426082"
	out := convertOK(t, `{\rtf1\ansi
{\pict\pngblip\picwgoal1440\pichgoal720 `+png+`}\par
}`)
	wants(t, out, `<img src="data:image/png;base64,`, `width="96"`, `height="48"`)

	// An unsupported format degrades to nothing (logged), not garbage.
	var logged bool
	if _, err := ToHTML([]byte(`{\rtf1{\pict\wmetafile8 0102}x\par}`), func(string, ...any) { logged = true }); err != nil {
		t.Fatal(err)
	}
	if !logged {
		t.Error("unsupported picture should log a degradation")
	}
}

func TestPageGeometry(t *testing.T) {
	out := convertOK(t, `{\rtf1\ansi\paperw12240\paperh15840\margl1440\margr1440\margt720\margb720
body\par}`)
	wants(t, out, "@page { size: 612pt 792pt; margin: 36pt 72pt 36pt 72pt }")
}

func TestUnknownContentSkipped(t *testing.T) {
	out := convertOK(t, `{\rtf1\ansi
{\*\themedata 0011aabbcc}
{\*\unknowndest secret payload}
{\stylesheet{\s1 Heading;}}
\nosuchcontrolword42 visible\par
}`)
	wants(t, out, "visible")
	rejects(t, out, "0011aabbcc", "secret payload", "Heading")
}

func TestEscapedBracesAndBackslash(t *testing.T) {
	out := convertOK(t, `{\rtf1\ansi
braces \{ and \} and back\\slash\par
}`)
	wants(t, out, "braces { and } and back\\slash")
}

func TestNotRTF(t *testing.T) {
	if _, err := ToHTML([]byte("plain text, no signature"), nil); !errors.Is(err, ErrNotRTF) {
		t.Errorf("want ErrNotRTF, got %v", err)
	}
}

func TestStyleSheetHeadingsAndBlocks(t *testing.T) {
	out := convertOK(t, `{\rtf1\ansi
{\stylesheet{\s0 Normal;}{\s1 heading 1;}{\s2 heading 2;}{\s15 Quote;}{\s16 CodeBlock;}{\s17 HorizontalRule;}}
{\pard\s1\b\fs48 Top Title\par}
{\pard\s2\b\fs36 Section\par}
{\pard\s15\li720 a quoted line\par}
{\pard\s16 code text\par}
{\pard\s17 \par}
{\pard plain body\par}
}`)
	wants(t, out,
		// The heading tag comes from the style name; bold+size ride along.
		`<h1><span style="font-size:24pt"><b>Top Title</b></span></h1>`,
		"<h2>",
		"<blockquote><p",
		"a quoted line",
		"<pre>",
		"code text",
		"<hr>",
		"<p>plain body</p>",
	)
}

func TestOutlineLevelHeadingFallback(t *testing.T) {
	// No stylesheet at all: \outlinelevel still marks the heading (Word emits
	// it on heading paragraphs).
	out := convertOK(t, `{\rtf1\ansi
{\pard\outlinelevel0\b Fallback Title\par}
}`)
	wants(t, out, "<h1>", "Fallback Title")
}

func TestLists(t *testing.T) {
	out := convertOK(t, `{\rtf1\ansi
{\pard{\pntext \bullet\tab}\ilvl0\ls1 one\par}
{\pard{\pntext \bullet\tab}\ilvl0\ls1 two\par}
{\pard{\pntext \bullet\tab}\ilvl1\ls1 nested\par}
{\pard after\par}
{\pard{\pntext 1.\tab}\ilvl0\ls2 first\par}
{\pard{\pntext 2.\tab}\ilvl0\ls2 second\par}
}`)
	wants(t, out,
		"<ul><li>one</li><li>two<ul><li>nested</li></ul></li></ul>",
		"<ol><li>first</li><li>second</li></ol>",
	)
	// The marker text itself must not leak into the item content.
	rejects(t, out, "<li>• one", "<li>1. first")
}

func TestAdjacentListsStaySeparate(t *testing.T) {
	out := convertOK(t, `{\rtf1\ansi
{\pard{\pntext 1.\tab}\ilvl0\ls2 alpha\par}
{\pard{\pntext 1.\tab}\ilvl0\ls3 beta\par}
}`)
	wants(t, out, "<ol><li>alpha</li></ol><ol><li>beta</li></ol>")
}

func TestTableHeaderRow(t *testing.T) {
	out := convertOK(t, `{\rtf1\ansi
\trowd\trhdr\cellx4000\cellx8000
{\intbl {\b Item}\cell}{\intbl {\b Qty}\cell}\row
\trowd\cellx4000\cellx8000
{\intbl Widgets\cell}{\intbl 5\cell}\row
}`)
	wants(t, out, "<th style=", "Item", "<td style=", "Widgets")
}
