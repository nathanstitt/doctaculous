// Package rtf reads Rich Text Format documents into HTML for the reflow
// pipeline — the same shape the CSV and XLSX frontends use: a dependency-free
// hand-rolled parser producing markup the HTML engine lays out, so every
// output format follows. The reader covers the document vocabulary RTF
// writers actually emit — paragraphs and character formatting, alignment and
// indents, font/color tables, hyperlink fields, tables, embedded PNG/JPEG
// pictures, page geometry, and the cp1252/\u escapes — and follows the RTF
// specification's own resilience rule for everything else: an unknown control
// word is skipped, an unknown {\*...} destination is ignored wholesale.
package rtf

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"strconv"
	"strings"
)

// ErrNotRTF reports input that does not start with an RTF signature.
var ErrNotRTF = errors.New("rtf: not an RTF document")

// destination classifies the group the converter is inside.
type destination int

const (
	destNone destination = iota
	destSkip
	destFontTbl
	destColorTbl
	destFldInst
	destPict
)

// charState is the character formatting the group stack scopes.
type charState struct {
	bold, italic, underline, strike bool
	sizePt                          float64
	colorIdx, highlightIdx, fontIdx int
	href                            string
}

// paraState is the paragraph formatting (reset by \pard).
type paraState struct {
	align                  string
	leftTw, firstTw        int
	spaceBeforeTw, afterTw int
	inTable                bool
}

// groupState is one {...} scope.
type groupState struct {
	char   charState
	para   paraState
	dest   destination
	ucSkip int
	field  *fieldCtx
}

// fieldCtx carries a {\field ...} group's instruction across its subgroups.
type fieldCtx struct {
	inst string
}

// run is one styled text span.
type run struct {
	state charState
	text  strings.Builder
}

// pictCtx accumulates an embedded picture.
type pictCtx struct {
	kind         string // "png" or "jpeg"; "" = unsupported
	hex          strings.Builder
	wGoal, hGoal int // twips
}

// converter drives the token stream into HTML blocks.
type converter struct {
	tz    tokenizer
	stack []groupState

	fonts    map[int]string
	fontName strings.Builder
	fontCur  int
	colors   []string
	colorCur struct {
		r, g, b int
		set     bool
	}

	logf func(string, ...any)

	// Document accumulation.
	blocks []string
	runs   []*run
	// Table accumulation.
	cellBlocks []string   // paragraphs of the current cell
	rowCells   []string   // completed cells of the current row
	tableRows  [][]string // completed rows
	cellxTw    []int      // current row's cell right-edges

	pict *pictCtx
	// pendingSkip counts \u fallback characters still to swallow.
	pendingSkip int
	// pendingStar marks a just-seen \* awaiting its destination word.
	pendingStar bool

	// Page geometry (twips; 0 = unset).
	paperW, paperH             int
	margL, margR, margT, margB int
}

// ToHTML converts RTF bytes to a standalone HTML document. logf (nil = no-op)
// receives degradation diagnostics (an unsupported picture format, etc.).
func ToHTML(data []byte, logf func(string, ...any)) (string, error) {
	if !strings.HasPrefix(strings.TrimLeft(string(data[:min(len(data), 16)]), "\uFEFF \t\r\n"), `{\rtf`) {
		return "", ErrNotRTF
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	c := &converter{
		tz:    tokenizer{src: data},
		fonts: map[int]string{},
		logf:  logf,
	}
	c.stack = []groupState{{ucSkip: 1, char: charState{colorIdx: -1, highlightIdx: -1, fontIdx: -1}}}
	c.convert()
	return c.document(), nil
}

// top returns the current group scope.
func (c *converter) top() *groupState { return &c.stack[len(c.stack)-1] }

func (c *converter) convert() {
	for {
		tok := c.tz.next()
		switch tok.kind {
		case tokEOF:
			c.flushParagraph()
			c.flushTable()
			return
		case tokGroupOpen:
			cur := *c.top()
			cur.field = c.top().field // share the field context by pointer
			c.stack = append(c.stack, cur)
			c.pendingStar = false
		case tokGroupClose:
			if len(c.stack) > 1 {
				closing := *c.top()
				c.stack = c.stack[:len(c.stack)-1]
				c.closeDestination(closing)
			}
			c.pendingStar = false
		case tokControl:
			c.control(tok)
		case tokText:
			c.text(tok.text)
		case tokHexByte:
			if c.swallowFallback() {
				continue
			}
			c.text(string(cp1252Rune(tok.b)))
		}
	}
}

// closeDestination finalizes a destination group as its scope pops.
func (c *converter) closeDestination(closing groupState) {
	switch closing.dest {
	case destFontTbl:
		c.flushFontName()
	case destColorTbl:
		// entries flush on ';' as they appear
	case destPict:
		if c.pict != nil && c.top().dest != destPict {
			c.flushPict()
		}
	case destFldInst:
		// the instruction text lives on the shared field context
	}
}

// swallowFallback consumes one post-\u fallback character.
func (c *converter) swallowFallback() bool {
	if c.pendingSkip > 0 {
		c.pendingSkip--
		return true
	}
	return false
}

// control dispatches one control word/symbol.
func (c *converter) control(tok token) {
	t := c.top()

	// Inside a skipped destination only group structure matters.
	if t.dest == destSkip {
		return
	}
	if c.pendingStar {
		c.pendingStar = false
		// \*\fldinst is the one ignorable destination we understand.
		if tok.word == "fldinst" {
			t.dest = destFldInst
			return
		}
		t.dest = destSkip
		return
	}

	switch tok.word {
	case "*":
		c.pendingStar = true
	case "rtf", "ansi", "deff", "ansicpg", "viewkind", "deflang", "deflangfe", "uc1":
		// header noise (uc1 without param path handled below via "uc")
	case "uc":
		t.ucSkip = tok.param
	case "u":
		if c.swallowFallback() {
			// a \u inside another \u's fallback — count it and move on
			return
		}
		r := rune(tok.param)
		if tok.param < 0 {
			r = rune(tok.param + 65536)
		}
		c.text(string(r))
		c.pendingSkip = t.ucSkip
	case "fonttbl":
		t.dest = destFontTbl
	case "colortbl":
		// Entries flush at each ';' — the conventional first ";" yields the
		// empty index-0 "auto" entry.
		t.dest = destColorTbl
	case "pict":
		t.dest = destPict
		c.pict = &pictCtx{}
	case "field":
		t.field = &fieldCtx{}
	case "fldrslt":
		if t.field != nil {
			if url, ok := hyperlinkURL(t.field.inst); ok {
				t.char.href = url
			}
		}
	case "stylesheet", "info", "header", "footer", "headerl", "headerr", "headerf",
		"footerl", "footerr", "footerf", "ftnsep", "ftnsepc", "aftnsep", "aftnsepc",
		"listtable", "listoverridetable", "revtbl", "generator", "themedata",
		"colorschememapping", "latentstyles", "datastore", "rsidtbl", "xmlnstbl",
		"filetbl", "objdata", "listtext", "pntext":
		t.dest = destSkip

	// Destination-specific controls.
	case "f":
		if t.dest == destFontTbl {
			c.flushFontName()
			c.fontCur = tok.param
		} else {
			t.char.fontIdx = tok.param
		}
	case "red":
		c.colorCur.r, c.colorCur.set = clamp255(tok.param), true
	case "green":
		c.colorCur.g, c.colorCur.set = clamp255(tok.param), true
	case "blue":
		c.colorCur.b, c.colorCur.set = clamp255(tok.param), true
	case "pngblip":
		if c.pict != nil {
			c.pict.kind = "png"
		}
	case "jpegblip":
		if c.pict != nil {
			c.pict.kind = "jpeg"
		}
	case "picwgoal":
		if c.pict != nil {
			c.pict.wGoal = tok.param
		}
	case "pichgoal":
		if c.pict != nil {
			c.pict.hGoal = tok.param
		}

	// Paragraphs.
	case "par":
		c.flushParagraph()
	case "pard":
		t.para = paraState{}
	case "line":
		c.appendRaw("<br>")
	case "tab":
		c.appendText("\t")
	case "ql":
		t.para.align = ""
	case "qc":
		t.para.align = "center"
	case "qr":
		t.para.align = "right"
	case "qj":
		t.para.align = "justify"
	case "li":
		t.para.leftTw = tok.param
	case "fi":
		t.para.firstTw = tok.param
	case "sb":
		t.para.spaceBeforeTw = tok.param
	case "sa":
		t.para.afterTw = tok.param

	// Character formatting.
	case "plain":
		t.char = charState{colorIdx: -1, highlightIdx: -1, fontIdx: -1, href: t.char.href}
	case "b":
		t.char.bold = !tok.hasParam || tok.param != 0
	case "i":
		t.char.italic = !tok.hasParam || tok.param != 0
	case "ul":
		t.char.underline = !tok.hasParam || tok.param != 0
	case "ulnone":
		t.char.underline = false
	case "strike":
		t.char.strike = !tok.hasParam || tok.param != 0
	case "fs":
		t.char.sizePt = float64(tok.param) / 2
	case "cf":
		t.char.colorIdx = tok.param
	case "highlight", "cb", "chcbpat":
		t.char.highlightIdx = tok.param

	// Tables.
	case "intbl":
		t.para.inTable = true
	case "trowd":
		c.cellxTw = nil
	case "cellx":
		c.cellxTw = append(c.cellxTw, tok.param)
	case "cell":
		c.endCell()
	case "row":
		c.endRow()

	// Page geometry.
	case "paperw":
		c.paperW = tok.param
	case "paperh":
		c.paperH = tok.param
	case "margl":
		c.margL = tok.param
	case "margr":
		c.margR = tok.param
	case "margt":
		c.margT = tok.param
	case "margb":
		c.margB = tok.param

	// Special characters.
	case "~":
		c.appendText(" ")
	case "_":
		c.appendText("‑")
	case "-":
		// optional hyphen: nothing
	case "bullet":
		c.appendText("•")
	case "endash":
		c.appendText("–")
	case "emdash":
		c.appendText("—")
	case "lquote":
		c.appendText("‘")
	case "rquote":
		c.appendText("’")
	case "ldblquote":
		c.appendText("“")
	case "rdblquote":
		c.appendText("”")

	default:
		// The RTF resilience rule: unknown control words are skipped.
	}
}

// hyperlinkURL extracts the target of a HYPERLINK field instruction.
func hyperlinkURL(inst string) (string, bool) {
	s := strings.TrimSpace(inst)
	i := strings.Index(strings.ToUpper(s), "HYPERLINK")
	if i < 0 {
		return "", false
	}
	s = strings.TrimSpace(s[i+len("HYPERLINK"):])
	s = strings.TrimPrefix(s, `\l `) // local anchors degrade to plain text
	if strings.HasPrefix(s, `"`) {
		if end := strings.Index(s[1:], `"`); end >= 0 {
			return s[1 : 1+end], true
		}
	}
	if fields := strings.Fields(s); len(fields) > 0 {
		return fields[0], true
	}
	return "", false
}

// text routes document text by destination.
func (c *converter) text(s string) {
	t := c.top()
	switch t.dest {
	case destSkip:
		return
	case destFontTbl:
		c.fontName.WriteString(s)
	case destColorTbl:
		for range strings.Count(s, ";") {
			c.flushColorEntry()
		}
	case destFldInst:
		if t.field != nil {
			t.field.inst += s
		}
	case destPict:
		c.pict.hex.WriteString(strings.Map(dropSpace, s))
	default:
		if c.pendingSkip > 0 {
			// Swallow \u fallback characters from plain text.
			rs := []rune(s)
			n := min(c.pendingSkip, len(rs))
			c.pendingSkip -= n
			s = string(rs[n:])
			if s == "" {
				return
			}
		}
		c.appendText(s)
	}
}

func dropSpace(r rune) rune {
	if r == ' ' || r == '\t' {
		return -1
	}
	return r
}

// flushFontName finalizes a font-table entry.
func (c *converter) flushFontName() {
	name := strings.TrimSuffix(strings.TrimSpace(c.fontName.String()), ";")
	if name != "" {
		c.fonts[c.fontCur] = name
	}
	c.fontName.Reset()
}

// flushColorEntry finalizes one color-table entry at its ';'.
func (c *converter) flushColorEntry() {
	if !c.colorCur.set {
		c.colors = append(c.colors, "") // an empty entry is "auto"
	} else {
		c.colors = append(c.colors, fmt.Sprintf("%02X%02X%02X",
			c.colorCur.r, c.colorCur.g, c.colorCur.b))
	}
	c.colorCur.r, c.colorCur.g, c.colorCur.b, c.colorCur.set = 0, 0, 0, false
}

// clamp255 bounds a color component to a byte.
func clamp255(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// appendText adds escaped text to the current run.
func (c *converter) appendText(s string) {
	c.appendRaw(html.EscapeString(s))
}

// appendRaw adds raw HTML to the current run, splitting runs on state change.
func (c *converter) appendRaw(s string) {
	st := c.top().char
	if len(c.runs) == 0 || c.runs[len(c.runs)-1].state != st {
		c.runs = append(c.runs, &run{state: st})
	}
	c.runs[len(c.runs)-1].text.WriteString(s)
}

// flushParagraph closes the current paragraph into a block (or table cell).
func (c *converter) flushParagraph() {
	para := c.top().para
	inner := c.renderRuns()
	c.runs = nil
	if inner == "" && !para.inTable {
		if para.inTable {
			return
		}
		// An empty paragraph still occupies a line.
		inner = ""
	}
	block := "<p" + paraStyleAttr(para) + ">" + inner + "</p>"
	if para.inTable {
		c.cellBlocks = append(c.cellBlocks, block)
		return
	}
	c.flushTable()
	c.blocks = append(c.blocks, block)
}

// renderRuns emits the pending runs as styled spans.
func (c *converter) renderRuns() string {
	var sb strings.Builder
	for _, r := range c.runs {
		text := r.text.String()
		if text == "" {
			continue
		}
		open, close := c.runWrappers(r.state)
		sb.WriteString(open)
		sb.WriteString(text)
		sb.WriteString(close)
	}
	return sb.String()
}

// runWrappers builds the open/close tags for a run's state.
func (c *converter) runWrappers(st charState) (string, string) {
	var styles []string
	if st.sizePt > 0 {
		styles = append(styles, "font-size:"+trimPt(st.sizePt)+"pt")
	}
	if st.fontIdx >= 0 {
		if name, ok := c.fonts[st.fontIdx]; ok {
			styles = append(styles, "font-family:"+quoteFontFamily(name))
		}
	}
	if rgb := c.colorAt(st.colorIdx); rgb != "" {
		styles = append(styles, "color:#"+rgb)
	}
	if rgb := c.colorAt(st.highlightIdx); rgb != "" {
		styles = append(styles, "background-color:#"+rgb)
	}
	var open, closer strings.Builder
	if st.href != "" {
		open.WriteString(`<a href="` + html.EscapeString(st.href) + `">`)
	}
	if len(styles) > 0 {
		open.WriteString(`<span style="` + strings.Join(styles, ";") + `">`)
	}
	if st.bold {
		open.WriteString("<b>")
	}
	if st.italic {
		open.WriteString("<i>")
	}
	if st.underline {
		open.WriteString("<u>")
	}
	if st.strike {
		open.WriteString("<s>")
	}
	if st.strike {
		closer.WriteString("</s>")
	}
	if st.underline {
		closer.WriteString("</u>")
	}
	if st.italic {
		closer.WriteString("</i>")
	}
	if st.bold {
		closer.WriteString("</b>")
	}
	if len(styles) > 0 {
		closer.WriteString("</span>")
	}
	if st.href != "" {
		closer.WriteString("</a>")
	}
	return open.String(), closer.String()
}

// colorAt resolves a color-table index ("" for auto/unset/out of range).
func (c *converter) colorAt(idx int) string {
	if idx <= 0 || idx >= len(c.colors) {
		return ""
	}
	return c.colors[idx]
}

// quoteFontFamily quotes a family name for CSS.
func quoteFontFamily(name string) string {
	return `'` + strings.ReplaceAll(name, `'`, "") + `'`
}

// paraStyleAttr renders paragraph formatting as a style attribute.
func paraStyleAttr(p paraState) string {
	var styles []string
	if p.align != "" {
		styles = append(styles, "text-align:"+p.align)
	}
	if p.leftTw != 0 {
		styles = append(styles, "margin-left:"+twipsPt(p.leftTw)+"pt")
	}
	if p.firstTw != 0 {
		styles = append(styles, "text-indent:"+twipsPt(p.firstTw)+"pt")
	}
	if p.spaceBeforeTw != 0 {
		styles = append(styles, "margin-top:"+twipsPt(p.spaceBeforeTw)+"pt")
	}
	if p.afterTw != 0 {
		styles = append(styles, "margin-bottom:"+twipsPt(p.afterTw)+"pt")
	}
	if len(styles) == 0 {
		return ""
	}
	return ` style="` + strings.Join(styles, ";") + `"`
}

// endCell closes the current cell.
func (c *converter) endCell() {
	// The pending runs are the cell's last (often only) paragraph.
	para := c.top().para
	if inner := c.renderRuns(); inner != "" || len(c.cellBlocks) == 0 {
		c.cellBlocks = append(c.cellBlocks, "<p"+paraStyleAttr(para)+">"+inner+"</p>")
	}
	c.runs = nil
	c.rowCells = append(c.rowCells, strings.Join(c.cellBlocks, ""))
	c.cellBlocks = nil
}

// endRow closes the current row.
func (c *converter) endRow() {
	if len(c.rowCells) == 0 {
		return
	}
	c.tableRows = append(c.tableRows, c.rowCells)
	c.rowCells = nil
}

// flushTable emits any accumulated table rows.
func (c *converter) flushTable() {
	if len(c.tableRows) == 0 {
		return
	}
	var sb strings.Builder
	sb.WriteString(`<table style="border-collapse:collapse">`)
	for _, row := range c.tableRows {
		sb.WriteString("<tr>")
		for _, cell := range row {
			sb.WriteString(`<td style="border:1px solid #808080;padding:2pt 4pt">` + cell + "</td>")
		}
		sb.WriteString("</tr>")
	}
	sb.WriteString("</table>")
	c.blocks = append(c.blocks, sb.String())
	c.tableRows = nil
}

// flushPict emits an accumulated picture as a data-URI image.
func (c *converter) flushPict() {
	p := c.pict
	c.pict = nil
	if p.kind == "" {
		c.logf("rtf: skipping picture in an unsupported format (only png/jpeg embed)")
		return
	}
	raw, err := hex.DecodeString(p.hex.String())
	if err != nil || len(raw) == 0 {
		c.logf("rtf: skipping malformed picture data: %v", err)
		return
	}
	mime := "image/png"
	if p.kind == "jpeg" {
		mime = "image/jpeg"
	}
	attrs := ""
	if p.wGoal > 0 {
		attrs += fmt.Sprintf(` width="%d"`, p.wGoal/15) // twips → px (96dpi)
	}
	if p.hGoal > 0 {
		attrs += fmt.Sprintf(` height="%d"`, p.hGoal/15)
	}
	c.appendRaw(`<img src="data:` + mime + `;base64,` + base64.StdEncoding.EncodeToString(raw) + `"` + attrs + `>`)
}

// document assembles the final HTML.
func (c *converter) document() string {
	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n<style>\n")
	sb.WriteString("body { font-family: 'Times New Roman', serif; font-size: 12pt; }\n")
	if c.paperW > 0 && c.paperH > 0 {
		sb.WriteString("@page { size: " + twipsPt(c.paperW) + "pt " + twipsPt(c.paperH) + "pt; margin: " +
			twipsPtDefault(c.margT, 1440) + "pt " + twipsPtDefault(c.margR, 1800) + "pt " +
			twipsPtDefault(c.margB, 1440) + "pt " + twipsPtDefault(c.margL, 1800) + "pt }\n")
	}
	sb.WriteString("</style>\n</head>\n<body>\n")
	for _, b := range c.blocks {
		sb.WriteString(b)
		sb.WriteString("\n")
	}
	sb.WriteString("</body>\n</html>\n")
	return sb.String()
}

// twipsPt renders twips as points.
func twipsPt(tw int) string { return trimPt(float64(tw) / 20) }

// twipsPtDefault substitutes RTF's implicit default when a margin is unset.
func twipsPtDefault(tw, def int) string {
	if tw == 0 {
		tw = def
	}
	return twipsPt(tw)
}

// trimPt formats a point value tersely.
func trimPt(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
