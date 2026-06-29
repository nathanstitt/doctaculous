# Web fonts (`@font-face` + WOFF/WOFF2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a CSS `@font-face`-declared family resolve to a real downloaded font (raw TTF/OTF, WOFF1, or WOFF2) fetched through the existing `ResourceLoader`, instead of falling through to the bundled base-14 substitutes.

**Architecture:** Font-infrastructure slice — the layout algorithm, the `render.Device` seam, and the PDF pipeline are untouched (shaping already goes through `FaceCache`). Four layers change: `pkg/css` captures the `@font-face` at-rule (today discarded); `pkg/font` gains WOFF1/WOFF2 decoders that unwrap a container to sfnt bytes plus a `LoadSFNT` entry; `pkg/layout/font` gains a `SystemFontProvider` (for `local()`) and a `FaceCache` that resolves `@font-face` sources lazily (cached, negative results included) before falling back to `LoadStandard`; `pkg/layout/css` + `pkg/doctaculous` thread the collected `@font-face` table into the cache. Every existing page (no `@font-face`) and all DOCX stay byte-identical.

**Tech Stack:** Go (stdlib `compress/zlib` for WOFF1); one new dependency `github.com/andybalholm/brotli` (MIT, pure-Go) for WOFF2 Brotli decompression only — the WOFF2 container parse + glyf/loca transform are written in-repo. Tests are hermetic (`MapLoader`/`DiskFontProvider` serve font bytes; no network).

**Spec:** `docs/superpowers/specs/2026-06-26-html-webfonts-design.md` — read it before starting.

---

## Cross-cutting rules for every task (read once, apply throughout)

- **Branch:** you are on `feat/html-webfonts`. Do **not** checkout/stash/switch branches. Do **not** commit unless a step says to. Before finishing a task, confirm `git status` is clean of scratch and `find . -name 'zz_*' -o -name '*probe*'` is empty — delete any throwaway probe/scratch files you (or a reviewer) created.
- **Sandbox:** run every `go`, `gofmt`, `golangci-lint` command (and any font download, `git push`, `gh`) with `dangerouslyDisableSandbox: true`. The sandbox blocks the Go build cache + TLS; a sandboxed failure with cache/permission/"no go files to analyze" errors is the sandbox, not a real failure — re-run disabled.
- **gofmt is separate from lint:** after editing, run `gofmt -l <changed dirs>` (must print nothing) AND `golangci-lint run ./pkg/css/... ./pkg/font/... ./pkg/layout/... ./pkg/doctaculous/...`. NO `//nolint`. The repo declines all "modernize" hints: keep explicit `if x < y { x = y }` clamps, indexed `for i := 0; i < n; i++` loops, `sort.SliceStable`. golangci-lint flags `if !(a && b)` (QF1001 → write the De Morgan form `if !a || !b`).
- **`unused` linter is enforced:** a struct field you add must be *read* by code in the same task/PR. If a field is only for a later task, defer adding it until that task reads it.
- **Editor diagnostics lag:** trust `go build ./...` / `go test`, not the editor panel's stale "undefined"/"unused"/"redeclared"/phantom-`zz_*` errors.
- **Byte-identical guard (run at the end of every task that touches resolution or rendering):** `go test ./pkg/doctaculous/... ./pkg/render/raster/...` (without `-update`) must pass, and `git status --short pkg/doctaculous/testdata pkg/render/raster/testdata` must show **only NEW files** (never a modified existing golden). A changed existing golden = web-font resolution leaked into the base-14 path; fix before proceeding.

---

## File Structure (what each new/changed file is responsible for)

- `pkg/css/fontface.go` (new) — `FontFace`/`FontSource` types + `parseFontFace(decls)` + the `src:` tokenizer.
- `pkg/css/parse.go` (modify) — capture the `@font-face` at-rule into `Stylesheet.FontFaces` (every other at-rule still skipped).
- `pkg/css/fontface_test.go` (new) — `@font-face` parse assertions + degradation.
- `pkg/font/sfnt.go` (new) — `LoadSFNT(data) (*Face, error)`: sniff the leading tag → sfnt pass-through / WOFF1 / WOFF2 → `parseProgram` + `Face` build. New `ErrInvalidWOFF` sentinel.
- `pkg/font/woff1.go` (new) — `decodeWOFF1(data) ([]byte, error)`: WOFF1 container → sfnt bytes (stdlib zlib).
- `pkg/font/woff2.go` (new) — `decodeWOFF2(data) ([]byte, error)`: WOFF2 container (Brotli + glyf/loca transform) → sfnt bytes.
- `pkg/font/sfntbuild.go` (new) — `buildSFNT(tables) []byte`: shared sfnt reassembly (offset table + sorted, aligned directory + data) used by both WOFF decoders.
- `pkg/font/{sfnt,woff1,woff2}_test.go` (new) — round-trip + degradation tests against committed fixtures.
- `pkg/layout/font/system.go` (new) — `SystemFontProvider` interface + `DiskFontProvider`.
- `pkg/layout/font/cache.go` (modify) — `NewFaceCacheWithFonts(...)`; `Resolve` consults `@font-face` sources first, lazy + cached.
- `pkg/layout/font/{system,cache}_test.go` (new/extend) — resolution + no-re-fetch + `local()` tests.
- `pkg/layout/css/build.go` (modify) — `assembleSheets` also returns aggregated `[]gcss.FontFace`; new `BuildWithFonts(...)`; `Build` delegates (signature unchanged → existing callers untouched).
- `pkg/doctaculous/html_backend.go` (modify) — call `BuildWithFonts`; wire the table + loader + system provider into `NewFaceCacheWithFonts`; add `WithSystemFontProvider`.
- `testdata/fonts/` (new) — the committed font fixtures (TTF + WOFF1 + WOFF2 from one ground truth) + a `LICENSE`/provenance note; README attribution if CC.
- `pkg/doctaculous/html_golden_test.go` (modify) — a new `@font-face` golden entry.
- `README.md` (modify, only if a CC font is used) — fixture attribution.
- `CLAUDE.md` (modify, final task) — move web fonts to Done; add the dep; remove from TODO.

---

## Task 0: Acquire the font fixtures (prerequisite — blocks all decode/resolution/golden tests)

**Files:**
- Create: `testdata/fonts/webfont.ttf`, `testdata/fonts/webfont.woff`, `testdata/fonts/webfont.woff2`
- Create: `testdata/fonts/PROVENANCE.md` (source URL + license + how the WOFF/WOFF2 were generated)

This task is **interactive** — the implementer does NOT decide the font alone or commit the binary unreviewed.

- [ ] **Step 1: Choose a candidate font**

Pick a small, permissively-licensed font whose glyph shapes are **obviously distinct** from the base-14 substitutes (TeX Gyre Heros/Termes, Inconsolata) — so a golden proves the *substitution* happened. A geometric/display face (e.g. a single-weight grotesque or a slab with strong distinguishing letterforms) works well. **Acceptable licenses: OFL, Apache, MIT, or Creative Commons (CC).** Prefer a font that ships only a handful of glyphs or can be subset to the golden's text (keep the fixture small).

- [ ] **Step 2: STOP and confirm the font + license with the controller**

Report: the font name, source URL, exact license, and (if CC) that attribution will go in `README.md`. **Do not download or commit until the controller confirms.** (Committing a binary asset with a license obligation warrants a human glance.)

- [ ] **Step 3: Download the TTF and generate WOFF1 + WOFF2 (sandbox disabled)**

With `dangerouslyDisableSandbox: true`, download the bare `.ttf`. Generate `.woff` and `.woff2` **from that same TTF** so all three share one ground truth — e.g. with `fonttools` if available:
```bash
# example only — adapt to the chosen tool; all run sandbox-disabled
pip install fonttools brotli   # brotli extra needed for woff2
fonttools ttLib.woff2 compress -o testdata/fonts/webfont.woff2 testdata/fonts/webfont.ttf
# woff1:
python3 -c "from fontTools.ttLib import TTFont; f=TTFont('testdata/fonts/webfont.ttf'); f.flavor='woff'; f.save('testdata/fonts/webfont.woff')"
```
If `fonttools` is unavailable, source the `.woff`/`.woff2` from the font's official distribution (same family/weight) and note that in PROVENANCE.

- [ ] **Step 4: Verify the three files are real and subset-small**

Run (sandbox disabled):
```bash
ls -l testdata/fonts/
file testdata/fonts/webfont.*
```
Expected: a TTF (`TrueType` / sfnt), a WOFF (`Web Open Font Format`), and a WOFF2. Each should be small (ideally < ~50KB; subset further if large). Confirm the WOFF2 actually uses the glyf transform if the tool produced it (the default for `fonttools` compress) — this is what Task 6 reconstructs.

- [ ] **Step 5: Write PROVENANCE.md and (if CC) README attribution**

Create `testdata/fonts/PROVENANCE.md` with: font name, source URL, license (with a link), and the exact commands used to generate the WOFF/WOFF2. If the font is **CC-licensed**, also add an attribution line to `README.md` (the project root README) under a "Test fixtures" / "Acknowledgements" section (create the section if absent), naming the font, its author, and its license.

- [ ] **Step 6: Commit**

```bash
git add testdata/fonts/ README.md
git commit -m "test: add hermetic web-font fixtures (TTF + WOFF1 + WOFF2)"
```

---

## Task 1: Capture `@font-face` in `pkg/css` — types + `src:` tokenizer

**Files:**
- Create: `pkg/css/fontface.go`
- Create: `pkg/css/fontface_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/css/fontface_test.go`:
```go
package css

import "testing"

func TestParseFontFaceSrcList(t *testing.T) {
	// A full src list: local() first, then two url()s with format hints, then a
	// bare url() with no format. Order and per-entry fields must be preserved.
	srcs := parseSrcList(`local("My Face"), url(my.woff2) format("woff2"), url('my.woff') format(woff), url(my.ttf)`)
	if len(srcs) != 4 {
		t.Fatalf("got %d sources, want 4: %+v", len(srcs), srcs)
	}
	if srcs[0].Local != "My Face" || srcs[0].URL != "" {
		t.Errorf("src[0] = %+v, want Local=\"My Face\"", srcs[0])
	}
	if srcs[1].URL != "my.woff2" || srcs[1].Format != "woff2" {
		t.Errorf("src[1] = %+v, want URL=my.woff2 Format=woff2", srcs[1])
	}
	if srcs[2].URL != "my.woff" || srcs[2].Format != "woff" {
		t.Errorf("src[2] = %+v, want URL=my.woff Format=woff", srcs[2])
	}
	if srcs[3].URL != "my.ttf" || srcs[3].Format != "" {
		t.Errorf("src[3] = %+v, want URL=my.ttf Format=\"\"", srcs[3])
	}
}

func TestParseSrcListSkipsMalformedEntry(t *testing.T) {
	// A garbage middle entry is skipped; the valid entries survive.
	srcs := parseSrcList(`url(a.ttf), not-a-source, url(b.ttf)`)
	if len(srcs) != 2 {
		t.Fatalf("got %d sources, want 2 (garbage entry skipped): %+v", len(srcs), srcs)
	}
	if srcs[0].URL != "a.ttf" || srcs[1].URL != "b.ttf" {
		t.Errorf("sources = %+v, want a.ttf then b.ttf", srcs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/css/ -run TestParseFontFace -v`
Expected: FAIL — `undefined: parseSrcList` / `undefined: FontSource`.

- [ ] **Step 3: Write the types + tokenizer**

Create `pkg/css/fontface.go`:
```go
package css

import "strings"

// FontFace is one captured @font-face rule. The cascade does not use it; it is a
// side table consumed at face-resolution time (which face a family name maps to).
type FontFace struct {
	Family  string       // font-family descriptor, unquoted and trimmed
	Sources []FontSource // src: list, in declared (fallback) order
	Weight  string       // font-weight descriptor (e.g. "normal","bold","700"); "" if absent
	Style   string       // font-style descriptor (e.g. "normal","italic"); "" if absent
}

// FontSource is one entry in an @font-face src: list: either a url() reference or
// a local() family name (mutually exclusive).
type FontSource struct {
	URL    string // url() ref; "" for a local() source
	Local  string // local() family name; "" for a url() source
	Format string // format(...) hint, lowercased and unquoted; "" if absent
}

// parseSrcList parses an @font-face src descriptor value into ordered sources.
// Entries are comma-separated at the top level (commas inside (), "" or '' do not
// split). A malformed entry (neither url() nor local()) is skipped; the rest
// survive.
func parseSrcList(val string) []FontSource {
	var out []FontSource
	for _, entry := range splitTopLevel(val, ',') {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		src, ok := parseSrcEntry(entry)
		if !ok {
			continue
		}
		out = append(out, src)
	}
	return out
}

// parseSrcEntry parses one src entry: "url(x) format(y)", "local(x)", or "url(x)".
func parseSrcEntry(entry string) (FontSource, bool) {
	switch {
	case strings.HasPrefix(entry, "url("):
		ref, rest, ok := takeFunc(entry, "url")
		if !ok {
			return FontSource{}, false
		}
		src := FontSource{URL: unquote(strings.TrimSpace(ref))}
		// Optional trailing format(...).
		rest = strings.TrimSpace(rest)
		if strings.HasPrefix(rest, "format(") {
			if f, _, ok := takeFunc(rest, "format"); ok {
				src.Format = strings.ToLower(unquote(strings.TrimSpace(f)))
			}
		}
		if src.URL == "" {
			return FontSource{}, false
		}
		return src, true
	case strings.HasPrefix(entry, "local("):
		name, _, ok := takeFunc(entry, "local")
		if !ok {
			return FontSource{}, false
		}
		name = unquote(strings.TrimSpace(name))
		if name == "" {
			return FontSource{}, false
		}
		return FontSource{Local: name}, true
	default:
		return FontSource{}, false
	}
}

// takeFunc consumes a leading fn(...) from s, returning the inner argument text
// and the remainder after the closing paren. ok is false if s does not start with
// fn( or has no matching ).
func takeFunc(s, fn string) (arg, rest string, ok bool) {
	prefix := fn + "("
	if !strings.HasPrefix(s, prefix) {
		return "", "", false
	}
	depth := 0
	for i := len(fn); i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[len(prefix):i], s[i+1:], true
			}
		}
	}
	return "", "", false
}

// splitTopLevel splits s on sep, ignoring sep inside (), "" or ''.
func splitTopLevel(s string, sep byte) []string {
	var parts []string
	depth := 0
	var quote byte
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case c == sep && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// unquote strips a single matching pair of surrounding ASCII quotes.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/css/ -run TestParseFontFace -v` and `go test ./pkg/css/ -run TestParseSrcList -v`
Expected: PASS.

- [ ] **Step 5: gofmt + lint + commit**

Run (sandbox disabled): `gofmt -l pkg/css/` (nothing) and `golangci-lint run ./pkg/css/...` (clean).
```bash
git add pkg/css/fontface.go pkg/css/fontface_test.go
git commit -m "css: @font-face src: tokenizer + FontFace/FontSource types"
```

---

## Task 2: Wire `@font-face` capture into `Parse`

**Files:**
- Modify: `pkg/css/parse.go` (the `@`-prefix branch ~line 42; add `FontFaces` to `Stylesheet` ~line 22)
- Create: `parseFontFace` in `pkg/css/fontface.go`
- Modify: `pkg/css/fontface_test.go`

- [ ] **Step 1: Write the failing test**

Append to `pkg/css/fontface_test.go`:
```go
func TestParseStylesheetCapturesFontFace(t *testing.T) {
	src := `
		p { color: red }
		@font-face {
			font-family: "My Face";
			src: url(my.woff2) format("woff2"), url(my.ttf);
			font-weight: bold;
			font-style: italic;
		}
		@media print { p { color: black } }
	`
	sheet := Parse(src)
	// The normal rule still parses; @media is still skipped (regression guard).
	if len(sheet.Rules) != 1 {
		t.Fatalf("got %d rules, want 1: %+v", len(sheet.Rules), sheet.Rules)
	}
	if len(sheet.FontFaces) != 1 {
		t.Fatalf("got %d font faces, want 1: %+v", len(sheet.FontFaces), sheet.FontFaces)
	}
	ff := sheet.FontFaces[0]
	if ff.Family != "My Face" {
		t.Errorf("family = %q, want \"My Face\"", ff.Family)
	}
	if len(ff.Sources) != 2 || ff.Sources[0].URL != "my.woff2" || ff.Sources[0].Format != "woff2" || ff.Sources[1].URL != "my.ttf" {
		t.Errorf("sources = %+v, want [my.woff2(woff2), my.ttf]", ff.Sources)
	}
	if ff.Weight != "bold" || ff.Style != "italic" {
		t.Errorf("weight/style = %q/%q, want bold/italic", ff.Weight, ff.Style)
	}
}

func TestParseFontFaceDroppedWhenNoFamilyOrSrc(t *testing.T) {
	// No family -> dropped. No src -> dropped.
	sheet := Parse(`@font-face { src: url(x.ttf) } @font-face { font-family: Foo }`)
	if len(sheet.FontFaces) != 0 {
		t.Fatalf("got %d font faces, want 0 (both incomplete): %+v", len(sheet.FontFaces), sheet.FontFaces)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/css/ -run 'TestParseStylesheetCapturesFontFace|TestParseFontFaceDropped' -v`
Expected: FAIL — `sheet.FontFaces undefined`.

- [ ] **Step 3: Add the `FontFaces` field and `parseFontFace`**

In `pkg/css/parse.go`, extend `Stylesheet` (around line 22):
```go
// Stylesheet is a parsed CSS document: an ordered list of style rules plus any
// captured @font-face rules. Source order is preserved (the cascade uses it as a
// tie-breaker; @font-face order is the fallback order within a family).
type Stylesheet struct {
	Rules     []Rule
	FontFaces []FontFace
}
```

In `pkg/css/parse.go`, change the at-rule branch in `Parse` (around line 42) from the bare `continue` to:
```go
		if strings.HasPrefix(prelude, "@") {
			if strings.EqualFold(strings.TrimSpace(prelude), "@font-face") {
				if ff, ok := parseFontFace(parseDeclarations(body)); ok {
					sheet.FontFaces = append(sheet.FontFaces, ff)
				}
			}
			continue // any other at-rule: block already consumed by the scanner
		}
```

In `pkg/css/fontface.go`, add:
```go
// parseFontFace maps an @font-face block's declarations to a FontFace. ok is false
// when the rule lacks a font-family or has no usable src (the caller drops it).
func parseFontFace(decls []Declaration) (FontFace, bool) {
	var ff FontFace
	for _, d := range decls {
		switch strings.ToLower(d.Property) {
		case "font-family":
			ff.Family = unquote(strings.TrimSpace(d.Value))
		case "src":
			ff.Sources = parseSrcList(d.Value)
		case "font-weight":
			ff.Weight = strings.ToLower(strings.TrimSpace(d.Value))
		case "font-style":
			ff.Style = strings.ToLower(strings.TrimSpace(d.Value))
		}
	}
	if ff.Family == "" || len(ff.Sources) == 0 {
		return FontFace{}, false
	}
	return ff, true
}
```

- [ ] **Step 4: Run the new tests AND the existing parse tests**

Run (sandbox disabled): `go test ./pkg/css/ -v`
Expected: PASS — including the pre-existing `TestParseStylesheet` (which asserts `@media` is skipped and `len(Rules)==2`); the `FontFaces` field does not change `Rules`. If `TestParseStylesheet` fails, you broke rule parsing — stop and fix.

- [ ] **Step 5: gofmt + lint + commit**

Run (sandbox disabled): `gofmt -l pkg/css/` (nothing); `golangci-lint run ./pkg/css/...` (clean).
```bash
git add pkg/css/parse.go pkg/css/fontface.go pkg/css/fontface_test.go
git commit -m "css: capture @font-face into Stylesheet.FontFaces (other at-rules still skipped)"
```

---

## Task 3: `pkg/font` sfnt reassembly + `LoadSFNT` sniffing (sfnt pass-through only)

This task builds the shared sfnt reassembler and the `LoadSFNT` entry, wired so a **raw sfnt** (the committed `.ttf`) round-trips. WOFF1/WOFF2 decode arrive in Tasks 4–6; `LoadSFNT` returns a typed error for them until then.

**Files:**
- Create: `pkg/font/sfntbuild.go`
- Create: `pkg/font/sfnt.go`
- Create: `pkg/font/sfnt_test.go`
- Modify: `pkg/font/errors.go` (add `ErrInvalidWOFF`)

- [ ] **Step 1: Write the failing test**

Create `pkg/font/sfnt_test.go`:
```go
package font

import (
	"os"
	"path/filepath"
	"testing"
)

// fixturePath resolves a testdata/fonts/* fixture from the repo root (three levels
// up from pkg/font).
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "fonts", name)
}

func TestLoadSFNTRawTTF(t *testing.T) {
	data, err := os.ReadFile(fixturePath(t, "webfont.ttf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	face, err := LoadSFNT(data)
	if err != nil {
		t.Fatalf("LoadSFNT(ttf): %v", err)
	}
	// A letter present in the subset must resolve to a non-empty outline.
	out, adv, ok := face.Glyph('A')
	if !ok || adv <= 0 {
		t.Fatalf("Glyph('A') ok=%v adv=%v, want a real glyph", ok, adv)
	}
	_ = out // outline may legitimately be non-nil; advance + ok is the contract here
}

func TestLoadSFNTRejectsGarbage(t *testing.T) {
	_, err := LoadSFNT([]byte("not a font at all"))
	if err == nil {
		t.Fatal("LoadSFNT(garbage) = nil error, want a typed error")
	}
}
```
(If the chosen fixture font has no `A`, change the probe rune in this and later tests to one it does contain — note it in PROVENANCE.)

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/font/ -run TestLoadSFNT -v`
Expected: FAIL — `undefined: LoadSFNT`.

- [ ] **Step 3: Add the sentinel, the reassembler, and `LoadSFNT`**

In `pkg/font/errors.go`, add:
```go
// ErrInvalidWOFF is returned when a WOFF/WOFF2 container is malformed (bad
// signature, truncated, bad compression, or an unreconstructable table transform).
// Callers fall back to a bundled substitute.
var ErrInvalidWOFF = errors.New("font: invalid WOFF container")
```
(Confirm `errors` is already imported in errors.go; if not, add it.)

Create `pkg/font/sfntbuild.go`:
```go
package font

import (
	"encoding/binary"
	"sort"
)

// sfntTable is one decoded table: its 4-byte tag and raw (uncompressed,
// untransformed) bytes.
type sfntTable struct {
	tag  [4]byte
	data []byte
}

// buildSFNT reassembles a valid sfnt (TrueType/OpenType) byte stream from decoded
// tables: an offset table, a tag-sorted table directory with correct offsets and
// checksums, and the 4-byte-aligned table data. flavor is the sfnt version tag
// (0x00010000 for TrueType, "OTTO" for CFF). This is the common tail both the
// WOFF1 and WOFF2 decoders feed their decoded tables into.
func buildSFNT(flavor uint32, tables []sfntTable) []byte {
	sort.Slice(tables, func(i, j int) bool {
		return binary.BigEndian.Uint32(tables[i].tag[:]) < binary.BigEndian.Uint32(tables[j].tag[:])
	})
	n := len(tables)
	// searchRange = (largest power of 2 <= n) * 16; entrySelector = log2 of that
	// power; rangeShift = n*16 - searchRange. (OpenType offset-table fields.)
	pow2, exp := 1, 0
	for pow2*2 <= n {
		pow2 *= 2
		exp++
	}
	searchRange := uint16(pow2 * 16)
	entrySelector := uint16(exp)
	rangeShift := uint16(n*16) - searchRange

	headerLen := 12 + 16*n
	offset := headerLen
	// 4-byte-align each table's start; record padded offsets.
	offsets := make([]int, n)
	for i := range tables {
		offsets[i] = offset
		offset += len(tables[i].data)
		offset = (offset + 3) &^ 3
	}
	total := offset

	buf := make([]byte, total)
	binary.BigEndian.PutUint32(buf[0:], flavor)
	binary.BigEndian.PutUint16(buf[4:], uint16(n))
	binary.BigEndian.PutUint16(buf[6:], searchRange)
	binary.BigEndian.PutUint16(buf[8:], entrySelector)
	binary.BigEndian.PutUint16(buf[10:], rangeShift)
	for i, t := range tables {
		rec := 12 + 16*i
		copy(buf[rec:rec+4], t.tag[:])
		binary.BigEndian.PutUint32(buf[rec+4:], tableChecksum(t.data))
		binary.BigEndian.PutUint32(buf[rec+8:], uint32(offsets[i]))
		binary.BigEndian.PutUint32(buf[rec+12:], uint32(len(t.data)))
		copy(buf[offsets[i]:], t.data)
	}
	return buf
}

// tableChecksum is the sum of the table's 32-bit big-endian words, zero-padded to
// a 4-byte boundary (OpenType table checksum). The parser does not validate these,
// but a well-formed directory carries them.
func tableChecksum(b []byte) uint32 {
	var sum uint32
	for i := 0; i+4 <= len(b); i += 4 {
		sum += binary.BigEndian.Uint32(b[i:])
	}
	if rem := len(b) % 4; rem != 0 {
		var tail [4]byte
		copy(tail[:], b[len(b)-rem:])
		sum += binary.BigEndian.Uint32(tail[:])
	}
	return sum
}
```

Create `pkg/font/sfnt.go`:
```go
package font

import (
	"encoding/binary"
	"fmt"
)

// sfnt version tags / WOFF signatures (the first 4 bytes of the container).
const (
	sigTrueType = 0x00010000
	sigTrue     = 0x74727565 // "true"
	sigOTTO     = 0x4F54544F // "OTTO"
	sigTTCF     = 0x74746366 // "ttcf" (TrueType Collection)
	sigWOFF     = 0x774F4646 // "wOFF"
	sigWOFF2    = 0x774F4632 // "wOF2"
)

// LoadSFNT builds a reflow Face from a font file's bytes, transparently unwrapping
// a WOFF1 or WOFF2 container to its sfnt tables first. Raw sfnt (TrueType/OpenType)
// is parsed directly. It returns a typed error for an unrecognized or malformed
// container so the caller (the face cache) falls back to a bundled substitute.
func LoadSFNT(data []byte) (*Face, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("%w: too short", ErrInvalidWOFF)
	}
	sig := binary.BigEndian.Uint32(data[:4])
	var sfnt []byte
	switch sig {
	case sigTrueType, sigTrue, sigOTTO, sigTTCF:
		sfnt = data
	case sigWOFF:
		b, err := decodeWOFF1(data)
		if err != nil {
			return nil, err
		}
		sfnt = b
	case sigWOFF2:
		b, err := decodeWOFF2(data)
		if err != nil {
			return nil, err
		}
		sfnt = b
	default:
		return nil, fmt.Errorf("%w: unrecognized signature 0x%08x", ErrUnsupportedFontProgram, sig)
	}
	prog, err := parseProgram(sfnt, progTrueType)
	if err != nil {
		return nil, err
	}
	return &Face{prog: prog, names: prog.nameToGID()}, nil
}
```

To make this compile before Tasks 4–6, add temporary stubs at the bottom of `sfnt.go` (each replaced by its real file in the next tasks):
```go
// Replaced by woff1.go in Task 4.
func decodeWOFF1(data []byte) ([]byte, error) {
	return nil, fmt.Errorf("%w: WOFF1 decode not yet implemented", ErrInvalidWOFF)
}

// Replaced by woff2.go in Task 6.
func decodeWOFF2(data []byte) ([]byte, error) {
	return nil, fmt.Errorf("%w: WOFF2 decode not yet implemented", ErrInvalidWOFF)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/font/ -run TestLoadSFNT -v`
Expected: PASS (raw TTF round-trips; garbage rejected).

- [ ] **Step 5: gofmt + lint + commit**

Run (sandbox disabled): `gofmt -l pkg/font/` (nothing); `golangci-lint run ./pkg/font/...` (clean).
```bash
git add pkg/font/sfnt.go pkg/font/sfntbuild.go pkg/font/sfnt_test.go pkg/font/errors.go
git commit -m "font: LoadSFNT sniffing + shared sfnt reassembly (raw sfnt pass-through; WOFF stubs)"
```

---

## Task 4: WOFF1 decode

**Files:**
- Create: `pkg/font/woff1.go` (move the `decodeWOFF1` stub out of `sfnt.go` into here)
- Create: `pkg/font/woff1_test.go`
- Modify: `pkg/font/sfnt.go` (delete the `decodeWOFF1` stub)

- [ ] **Step 1: Write the failing test**

Create `pkg/font/woff1_test.go`:
```go
package font

import (
	"os"
	"testing"
)

func TestDecodeWOFF1RoundTrips(t *testing.T) {
	ttf, err := os.ReadFile(fixturePath(t, "webfont.ttf"))
	if err != nil {
		t.Fatalf("read ttf: %v", err)
	}
	woff, err := os.ReadFile(fixturePath(t, "webfont.woff"))
	if err != nil {
		t.Fatalf("read woff: %v", err)
	}
	// The WOFF must decode to a Face whose 'A' outline matches the bare TTF's 'A'.
	bare, err := LoadSFNT(ttf)
	if err != nil {
		t.Fatalf("LoadSFNT(ttf): %v", err)
	}
	got, err := LoadSFNT(woff)
	if err != nil {
		t.Fatalf("LoadSFNT(woff): %v", err)
	}
	assertSameGlyph(t, bare, got, 'A')
}

// assertSameGlyph fails unless rune r has the same advance and outline presence in
// both faces (the round-trip ground-truth check shared by the WOFF1/WOFF2 tests).
func assertSameGlyph(t *testing.T, want, got *Face, r rune) {
	t.Helper()
	wOut, wAdv, wOK := want.Glyph(r)
	gOut, gAdv, gOK := got.Glyph(r)
	if wOK != gOK || wAdv != gAdv {
		t.Fatalf("glyph %q mismatch: want ok=%v adv=%v, got ok=%v adv=%v", r, wOK, wAdv, gOK, gAdv)
	}
	if (wOut == nil) != (gOut == nil) {
		t.Fatalf("glyph %q outline presence differs: want nil=%v, got nil=%v", r, wOut == nil, gOut == nil)
	}
}

func TestDecodeWOFF1RejectsTruncated(t *testing.T) {
	_, err := decodeWOFF1([]byte("wOFF\x00\x01")) // signature only, header truncated
	if err == nil {
		t.Fatal("decodeWOFF1(truncated) = nil error, want a typed error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/font/ -run TestDecodeWOFF1 -v`
Expected: FAIL — the stub returns "not yet implemented" so the round-trip errors.

- [ ] **Step 3: Implement `decodeWOFF1` (verify the layout against the W3C WOFF1 spec first)**

`WebFetch` https://www.w3.org/TR/WOFF/ to confirm the header + table-directory byte layout before coding. Then delete the `decodeWOFF1` stub from `sfnt.go` and create `pkg/font/woff1.go`:
```go
package font

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
)

// decodeWOFF1 unwraps a WOFF (1.0) container to sfnt bytes. WOFF1 stores the sfnt
// table directory in a compact form and each table either zlib-compressed (when
// compLength < origLength) or stored raw; this rebuilds a standard sfnt via
// buildSFNT. Layout per the W3C WOFF File Format 1.0 spec.
func decodeWOFF1(data []byte) ([]byte, error) {
	// WOFFHeader is 44 bytes: signature(4) flavor(4) length(4) numTables(2)
	// reserved(2) totalSfntSize(4) majorVersion(2) minorVersion(2) metaOffset(4)
	// metaLength(4) metaOrigLength(4) privOffset(4) privLength(4).
	if len(data) < 44 {
		return nil, fmt.Errorf("%w: WOFF header truncated", ErrInvalidWOFF)
	}
	if binary.BigEndian.Uint32(data[0:]) != sigWOFF {
		return nil, fmt.Errorf("%w: bad WOFF signature", ErrInvalidWOFF)
	}
	flavor := binary.BigEndian.Uint32(data[4:])
	numTables := int(binary.BigEndian.Uint16(data[12:]))

	// Table directory: numTables entries of 20 bytes each, starting at offset 44.
	// Each entry: tag(4) offset(4) compLength(4) origLength(4) origChecksum(4).
	const dirStart = 44
	if len(data) < dirStart+20*numTables {
		return nil, fmt.Errorf("%w: WOFF table directory truncated", ErrInvalidWOFF)
	}
	tables := make([]sfntTable, numTables)
	for i := 0; i < numTables; i++ {
		e := dirStart + 20*i
		var t sfntTable
		copy(t.tag[:], data[e:e+4])
		off := binary.BigEndian.Uint32(data[e+4:])
		compLen := binary.BigEndian.Uint32(data[e+8:])
		origLen := binary.BigEndian.Uint32(data[e+12:])
		if int(off)+int(compLen) > len(data) {
			return nil, fmt.Errorf("%w: WOFF table %q out of range", ErrInvalidWOFF, t.tag)
		}
		raw := data[off : off+compLen]
		if compLen == origLen {
			t.data = append([]byte(nil), raw...) // stored uncompressed
		} else {
			zr, err := zlib.NewReader(bytes.NewReader(raw))
			if err != nil {
				return nil, fmt.Errorf("%w: WOFF table %q zlib: %v", ErrInvalidWOFF, t.tag, err)
			}
			out, err := io.ReadAll(io.LimitReader(zr, int64(origLen)+1))
			zr.Close()
			if err != nil {
				return nil, fmt.Errorf("%w: WOFF table %q inflate: %v", ErrInvalidWOFF, t.tag, err)
			}
			if uint32(len(out)) != origLen {
				return nil, fmt.Errorf("%w: WOFF table %q length %d != declared %d", ErrInvalidWOFF, t.tag, len(out), origLen)
			}
			t.data = out
		}
		tables[i] = t
	}
	return buildSFNT(flavor, tables), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/font/ -run TestDecodeWOFF1 -v`
Expected: PASS (WOFF1 → same 'A' as the TTF; truncated → error).

- [ ] **Step 5: gofmt + lint + commit**

Run (sandbox disabled): `gofmt -l pkg/font/` (nothing); `golangci-lint run ./pkg/font/...` (clean).
```bash
git add pkg/font/woff1.go pkg/font/woff1_test.go pkg/font/sfnt.go
git commit -m "font: WOFF1 decode (per-table zlib/raw -> sfnt)"
```

---

## Task 5: Add the Brotli dependency + WOFF2 container parse (non-transformed tables)

Splits WOFF2 into two tasks: this one decodes the container (header, compact directory, Brotli block) for **non-transformed** tables and reassembles; Task 6 adds the glyf/loca transform reconstruction. (A WOFF2 produced by `fonttools` *will* transform glyf — so the full round-trip test lands in Task 6. This task tests the directory parse + Brotli wiring with adversarial/unit assertions.)

**Files:**
- Modify: `go.mod`, `go.sum` (add `github.com/andybalholm/brotli`)
- Create: `pkg/font/woff2.go` (move the `decodeWOFF2` stub out of `sfnt.go`)
- Create: `pkg/font/woff2_test.go`
- Modify: `pkg/font/sfnt.go` (delete the `decodeWOFF2` stub)

- [ ] **Step 1: Add the dependency (sandbox disabled — needs network/proxy)**

Run (sandbox disabled):
```bash
go get github.com/andybalholm/brotli@latest
go mod tidy
```
Expected: `go.mod` gains `github.com/andybalholm/brotli vX.Y.Z`. Confirm it is the only new require.

- [ ] **Step 2: Write the failing test**

Create `pkg/font/woff2_test.go`:
```go
package font

import "testing"

func TestDecodeWOFF2RejectsBadSignature(t *testing.T) {
	_, err := decodeWOFF2([]byte("wOF2\x00\x00")) // signature only
	if err == nil {
		t.Fatal("decodeWOFF2(truncated) = nil error, want a typed error")
	}
}

func TestUIntBase128(t *testing.T) {
	// 0x3F88 = 16264 encodes as 0xFF 0x08 (two base-128 groups: 0x7F<<7 | 0x08).
	got, n, err := readUIntBase128([]byte{0xFF, 0x08})
	if err != nil {
		t.Fatalf("readUIntBase128: %v", err)
	}
	if got != (0x7F<<7)|0x08 || n != 2 {
		t.Fatalf("readUIntBase128 = %d (n=%d), want %d (n=2)", got, n, (0x7F<<7)|0x08)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/font/ -run 'TestDecodeWOFF2RejectsBadSignature|TestUIntBase128' -v`
Expected: FAIL — `undefined: readUIntBase128` (and the stub error for the signature test passes only after the real file exists).

- [ ] **Step 4: Implement the container parse (verify layout against the W3C WOFF2 spec first)**

`WebFetch` https://www.w3.org/TR/WOFF2/ to confirm: the 48-byte header, the `UIntBase128` encoding, the table-directory `flags` byte (6-bit known-tag index, `0x3f` = custom tag) + the known-tags table, and the transform flag bits. Delete the `decodeWOFF2` stub from `sfnt.go` and create `pkg/font/woff2.go`:
```go
package font

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/andybalholm/brotli"
)

// woff2KnownTags is the WOFF2 known-table tag list, indexed by the 6-bit flag
// value. Index 63 (0x3f) means a 4-byte custom tag follows instead. (Order is
// normative — copy exactly from the W3C WOFF2 spec, Table "Known Table Tags".)
var woff2KnownTags = [...][4]byte{
	{'c', 'm', 'a', 'p'}, {'h', 'e', 'a', 'd'}, {'h', 'h', 'e', 'a'}, {'h', 'm', 't', 'x'},
	{'m', 'a', 'x', 'p'}, {'n', 'a', 'm', 'e'}, {'O', 'S', '/', '2'}, {'p', 'o', 's', 't'},
	{'c', 'v', 't', ' '}, {'f', 'p', 'g', 'm'}, {'g', 'l', 'y', 'f'}, {'l', 'o', 'c', 'a'},
	{'p', 'r', 'e', 'p'}, {'C', 'F', 'F', ' '}, {'V', 'O', 'R', 'G'}, {'E', 'B', 'D', 'T'},
	{'E', 'B', 'L', 'C'}, {'g', 'a', 's', 'p'}, {'h', 'd', 'm', 'x'}, {'k', 'e', 'r', 'n'},
	{'L', 'T', 'S', 'H'}, {'P', 'C', 'L', 'T'}, {'V', 'D', 'M', 'X'}, {'v', 'h', 'e', 'a'},
	{'v', 'm', 't', 'x'}, {'B', 'A', 'S', 'E'}, {'G', 'D', 'E', 'F'}, {'G', 'P', 'O', 'S'},
	{'G', 'S', 'U', 'B'}, {'E', 'B', 'S', 'C'}, {'J', 'S', 'T', 'F'}, {'M', 'A', 'T', 'H'},
	{'C', 'B', 'D', 'T'}, {'C', 'B', 'L', 'C'}, {'C', 'O', 'L', 'R'}, {'C', 'P', 'A', 'L'},
	{'S', 'V', 'G', ' '}, {'s', 'b', 'i', 'x'}, {'a', 'c', 'n', 't'}, {'a', 'v', 'a', 'r'},
	{'b', 'd', 'a', 't'}, {'b', 'l', 'o', 'c'}, {'b', 's', 'l', 'n'}, {'c', 'v', 'a', 'r'},
	{'f', 'd', 's', 'c'}, {'f', 'e', 'a', 't'}, {'f', 'm', 't', 'x'}, {'f', 'v', 'a', 'r'},
	{'g', 'v', 'a', 'r'}, {'h', 's', 't', 'y'}, {'j', 'u', 's', 't'}, {'l', 'c', 'a', 'r'},
	{'m', 'o', 'r', 't'}, {'m', 'o', 'r', 'x'}, {'o', 'p', 'b', 'd'}, {'p', 'r', 'o', 'p'},
	{'t', 'r', 'a', 'k'}, {'Z', 'a', 'p', 'f'}, {'S', 'i', 'l', 'f'}, {'G', 'l', 'a', 't'},
	{'G', 'l', 'o', 'c'}, {'F', 'e', 'a', 't'}, {'S', 'i', 'l', 'l'},
}

// woff2Entry is one parsed WOFF2 directory entry before its data is sliced from
// the decompressed block.
type woff2Entry struct {
	tag         [4]byte
	transformed bool
	origLength  uint32
	transLength uint32 // valid only when transformed
}

// decodeWOFF2 unwraps a WOFF2 container to sfnt bytes: parse the header + compact
// directory, Brotli-decompress the single table block, then for each table either
// pass it through or (glyf/loca) reverse the transform, and reassemble via
// buildSFNT. Layout per the W3C WOFF2 spec.
func decodeWOFF2(data []byte) ([]byte, error) {
	// WOFF2Header (48 bytes): signature(4) flavor(4) length(4) numTables(2)
	// reserved(2) totalSfntSize(4) totalCompressedSize(4) major(2) minor(2)
	// metaOffset(4) metaLength(4) metaOrigLength(4) privOffset(4) privLength(4).
	if len(data) < 48 {
		return nil, fmt.Errorf("%w: WOFF2 header truncated", ErrInvalidWOFF)
	}
	if binary.BigEndian.Uint32(data[0:]) != sigWOFF2 {
		return nil, fmt.Errorf("%w: bad WOFF2 signature", ErrInvalidWOFF)
	}
	flavor := binary.BigEndian.Uint32(data[4:])
	numTables := int(binary.BigEndian.Uint16(data[12:]))
	totalCompressed := binary.BigEndian.Uint32(data[20:])

	pos := 48
	entries := make([]woff2Entry, numTables)
	for i := 0; i < numTables; i++ {
		if pos >= len(data) {
			return nil, fmt.Errorf("%w: WOFF2 directory truncated", ErrInvalidWOFF)
		}
		flags := data[pos]
		pos++
		tagIdx := flags & 0x3f
		var tag [4]byte
		if tagIdx == 0x3f {
			if pos+4 > len(data) {
				return nil, fmt.Errorf("%w: WOFF2 custom tag truncated", ErrInvalidWOFF)
			}
			copy(tag[:], data[pos:pos+4])
			pos += 4
		} else {
			if int(tagIdx) >= len(woff2KnownTags) {
				return nil, fmt.Errorf("%w: WOFF2 bad known-tag index %d", ErrInvalidWOFF, tagIdx)
			}
			tag = woff2KnownTags[tagIdx]
		}
		// transform version is bits 6-7 of flags; 0 means "transformed" for
		// glyf/loca and "no transform" for all other tables (WOFF2 spec quirk).
		transformVersion := (flags >> 6) & 0x3
		origLen, n, err := readUIntBase128(data[pos:])
		if err != nil {
			return nil, err
		}
		pos += n
		e := woff2Entry{tag: tag, origLength: origLen}
		isGlyfOrLoca := tag == woff2KnownTags[10] || tag == woff2KnownTags[11] // glyf, loca
		if isGlyfOrLoca {
			e.transformed = transformVersion == 0
		} else {
			e.transformed = transformVersion != 0
		}
		if e.transformed {
			transLen, n, err := readUIntBase128(data[pos:])
			if err != nil {
				return nil, err
			}
			pos += n
			e.transLength = transLen
		}
		entries[i] = e
	}

	// The compressed table block follows the directory and runs totalCompressed
	// bytes; Brotli-decompress it whole.
	if pos+int(totalCompressed) > len(data) {
		return nil, fmt.Errorf("%w: WOFF2 compressed block out of range", ErrInvalidWOFF)
	}
	br := brotli.NewReader(bytes.NewReader(data[pos : pos+int(totalCompressed)]))
	block, err := io.ReadAll(br)
	if err != nil {
		return nil, fmt.Errorf("%w: WOFF2 brotli: %v", ErrInvalidWOFF, err)
	}

	tables, err := reconstructWOFF2Tables(flavor, entries, block)
	if err != nil {
		return nil, err
	}
	return buildSFNT(flavor, tables), nil
}

// readUIntBase128 decodes a WOFF2 UIntBase128 value (1-5 big-endian base-128
// groups, high bit = continuation). Returns the value and bytes consumed.
func readUIntBase128(b []byte) (uint32, int, error) {
	var v uint32
	for i := 0; i < 5; i++ {
		if i >= len(b) {
			return 0, 0, fmt.Errorf("%w: UIntBase128 truncated", ErrInvalidWOFF)
		}
		c := b[i]
		// No leading 0x80 (overlong) and no overflow past 32 bits.
		if i == 0 && c == 0x80 {
			return 0, 0, fmt.Errorf("%w: UIntBase128 leading zero", ErrInvalidWOFF)
		}
		if v > (0xFFFFFFFF >> 7) {
			return 0, 0, fmt.Errorf("%w: UIntBase128 overflow", ErrInvalidWOFF)
		}
		v = (v << 7) | uint32(c&0x7f)
		if c&0x80 == 0 {
			return v, i + 1, nil
		}
	}
	return 0, 0, fmt.Errorf("%w: UIntBase128 too long", ErrInvalidWOFF)
}
```

Add a **temporary** `reconstructWOFF2Tables` to `woff2.go` that handles only non-transformed tables (Task 6 replaces it with the transform-aware version):
```go
// reconstructWOFF2Tables slices each table from the decompressed block in
// directory order. (Task 6 replaces this with glyf/loca transform reconstruction.)
func reconstructWOFF2Tables(flavor uint32, entries []woff2Entry, block []byte) ([]sfntTable, error) {
	tables := make([]sfntTable, 0, len(entries))
	off := 0
	for _, e := range entries {
		if e.transformed {
			return nil, fmt.Errorf("%w: WOFF2 transformed %q not yet supported", ErrInvalidWOFF, e.tag)
		}
		end := off + int(e.origLength)
		if end > len(block) {
			return nil, fmt.Errorf("%w: WOFF2 table %q out of range", ErrInvalidWOFF, e.tag)
		}
		tables = append(tables, sfntTable{tag: e.tag, data: append([]byte(nil), block[off:end]...)})
		off = end
	}
	return tables, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/font/ -run 'TestDecodeWOFF2RejectsBadSignature|TestUIntBase128' -v`
Expected: PASS.

- [ ] **Step 6: gofmt + lint + commit**

Run (sandbox disabled): `gofmt -l pkg/font/` (nothing); `golangci-lint run ./pkg/font/...` (clean).
```bash
git add go.mod go.sum pkg/font/woff2.go pkg/font/woff2_test.go pkg/font/sfnt.go
git commit -m "font: WOFF2 container parse + Brotli dep (non-transformed tables; glyf transform next)"
```

---

## Task 6: WOFF2 glyf/loca transform reconstruction (the hard part)

**Files:**
- Create: `pkg/font/woff2glyf.go`
- Modify: `pkg/font/woff2.go` (replace `reconstructWOFF2Tables` with the transform-aware version)
- Modify: `pkg/font/woff2_test.go` (add the full round-trip from the committed WOFF2)

- [ ] **Step 1: Write the failing test (the real round-trip)**

Append to `pkg/font/woff2_test.go`. First change its import line `import "testing"` to a block that adds `os`:
```go
import (
	"os"
	"testing"
)
```
Then append the test:
```go
func TestDecodeWOFF2RoundTrips(t *testing.T) {
	ttf, err := os.ReadFile(fixturePath(t, "webfont.ttf"))
	if err != nil {
		t.Fatalf("read ttf: %v", err)
	}
	w2, err := os.ReadFile(fixturePath(t, "webfont.woff2"))
	if err != nil {
		t.Fatalf("read woff2: %v", err)
	}
	bare, err := LoadSFNT(ttf)
	if err != nil {
		t.Fatalf("LoadSFNT(ttf): %v", err)
	}
	got, err := LoadSFNT(w2)
	if err != nil {
		t.Fatalf("LoadSFNT(woff2): %v", err)
	}
	// Same outline + advance for several runes present in the subset.
	for _, r := range []rune{'A', 'a', 'g'} {
		assertSameGlyph(t, bare, got, r)
	}
}
```
(Adjust the rune set to glyphs actually in the fixture subset.)

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/font/ -run TestDecodeWOFF2RoundTrips -v`
Expected: FAIL — `reconstructWOFF2Tables` returns "transformed glyf not yet supported".

- [ ] **Step 3: Confirm the transform byte layout against the W3C WOFF2 spec**

`WebFetch` https://www.w3.org/TR/WOFF2/ §"Transformed glyf table" and §"Transformed loca table". Confirm exactly: the transformed-glyf sub-stream header (version, numGlyphs, indexFormat, and the 7 sub-stream lengths: nContourStream, nPointsStream, flagStream, glyphStream, compositeStream, bboxStream + bboxBitmap, instructionStream); the **255UInt16** encoding (for nPoints); the **triplet (flag+coordinate)** encoding for simple-glyph points; the composite-glyph flag bits; and that the transformed loca is empty (rebuilt from glyf). Note any detail that differs from your assumptions before coding.

- [ ] **Step 4: Implement the transform reconstruction**

Create `pkg/font/woff2glyf.go` implementing:
```go
package font

import (
	"encoding/binary"
	"fmt"
)

// reconstructGlyf rebuilds standard glyf + loca tables from a WOFF2 transformed
// glyf sub-stream (W3C WOFF2 §"Transformed glyf table"). It returns the glyf bytes
// and the loca bytes (indexFormat-dependent), or an error for a malformed stream.
func reconstructGlyf(transformed []byte) (glyf []byte, loca []byte, err error) {
	// ... per the spec:
	//  1. Read the transformed-glyf header (numGlyphs, indexFormat, 7 stream sizes).
	//  2. Slice the 7 sub-streams.
	//  3. For each glyph: read nContours (int16 from nContourStream).
	//       <0  -> composite: copy from compositeStream (parse component flags to
	//              find its length), set a flag to emit instructions.
	//       ==0 -> empty glyph (no outline).
	//       >0  -> simple: read nContours endPts via 255UInt16 from nPointsStream;
	//              read flags from flagStream; decode x/y deltas from glyphStream
	//              via the triplet encoding; assemble a standard simple-glyph record
	//              (flags, xCoordinates, yCoordinates, with repeat-flag packing).
	//  4. Append each glyph to glyf (2-byte aligned per loca), recording offsets.
	//  5. Emit loca (uint16 halves if indexFormat==0, else uint32).
	return nil, nil, fmt.Errorf("not implemented")
}

// read255UInt16 decodes a WOFF2 255UInt16 value (one of three forms keyed by a
// lead byte: 253->next 2 bytes; 255->next byte + 253; 254->next byte + 506;
// else the byte itself). Returns the value and bytes consumed.
func read255UInt16(b []byte) (uint16, int, error) {
	// ... per the spec.
	return 0, 0, fmt.Errorf("not implemented")
}

// decodeTriplet decodes one simple-glyph point's (dx, dy) from a flag byte and the
// glyphStream coordinate bytes (W3C WOFF2 triplet encoding table). onCurve is the
// flag's high bit. Returns dx, dy, and bytes consumed from coords.
func decodeTriplet(flag byte, coords []byte) (dx, dy int, n int, err error) {
	// ... per the spec's 128-row triplet table (flag&0x7f selects the row).
	return 0, 0, 0, fmt.Errorf("not implemented")
}

var _ = binary.BigEndian // keep the import while stubs are fleshed out
```
Then fill in each function per the spec. **Implement incrementally and re-run the round-trip test after each glyph case** (empty, then simple, then composite) — the fixture exercises whichever its glyphs use. The triplet table and 255UInt16 are the easiest to get subtly wrong; cross-check a couple of decoded points by hand against the bare TTF's `glyf` if a mismatch appears.

In `pkg/font/woff2.go`, replace `reconstructWOFF2Tables` so a transformed glyf/loca pair is reconstructed:
```go
func reconstructWOFF2Tables(flavor uint32, entries []woff2Entry, block []byte) ([]sfntTable, error) {
	// First slice every table's transformed/raw bytes from the block in order.
	type slot struct {
		entry woff2Entry
		raw   []byte
	}
	slots := make([]slot, len(entries))
	off := 0
	for i, e := range entries {
		size := int(e.origLength)
		if e.transformed {
			size = int(e.transLength)
		}
		end := off + size
		if end > len(block) {
			return nil, fmt.Errorf("%w: WOFF2 table %q out of range", ErrInvalidWOFF, e.tag)
		}
		slots[i] = slot{entry: e, raw: block[off:end]}
		off = end
	}

	// Reconstruct glyf+loca together if glyf is transformed.
	var rebuiltGlyf, rebuiltLoca []byte
	for _, s := range slots {
		if s.entry.tag == woff2KnownTags[10] && s.entry.transformed { // glyf
			g, l, err := reconstructGlyf(s.raw)
			if err != nil {
				return nil, err
			}
			rebuiltGlyf, rebuiltLoca = g, l
		}
	}

	tables := make([]sfntTable, 0, len(slots))
	for _, s := range slots {
		var data []byte
		switch {
		case s.entry.tag == woff2KnownTags[10] && s.entry.transformed: // glyf
			data = rebuiltGlyf
		case s.entry.tag == woff2KnownTags[11] && s.entry.transformed: // loca
			data = rebuiltLoca
		case s.entry.transformed:
			return nil, fmt.Errorf("%w: WOFF2 unexpected transform on %q", ErrInvalidWOFF, s.entry.tag)
		default:
			data = append([]byte(nil), s.raw...)
		}
		tables = append(tables, sfntTable{tag: s.entry.tag, data: data})
	}
	return tables, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/font/ -run TestDecodeWOFF2 -v`
Expected: PASS — the WOFF2 fixture round-trips to the same glyphs as the bare TTF.

- [ ] **Step 6: Add adversarial degradation tests**

Append to `pkg/font/woff2_test.go`:
```go
func TestDecodeWOFF2CorruptTransformDegrades(t *testing.T) {
	w2, err := os.ReadFile(fixturePath(t, "webfont.woff2"))
	if err != nil {
		t.Fatalf("read woff2: %v", err)
	}
	// Truncate inside the compressed block: brotli or reconstruction must error,
	// never panic.
	corrupt := w2[:len(w2)-10]
	if _, err := decodeWOFF2(corrupt); err == nil {
		t.Fatal("decodeWOFF2(truncated block) = nil error, want a typed error")
	}
}
```
Run (sandbox disabled): `go test ./pkg/font/ -run TestDecodeWOFF2 -v` — PASS, no panic.

- [ ] **Step 7: gofmt + lint + commit**

Run (sandbox disabled): `gofmt -l pkg/font/` (nothing); `golangci-lint run ./pkg/font/...` (clean); `go test ./pkg/font/... -v` (all green).
```bash
git add pkg/font/woff2glyf.go pkg/font/woff2.go pkg/font/woff2_test.go
git commit -m "font: WOFF2 glyf/loca transform reconstruction (full WOFF2 round-trip)"
```

---

## Task 7: `SystemFontProvider` + `DiskFontProvider`

**Files:**
- Create: `pkg/layout/font/system.go`
- Create: `pkg/layout/font/system_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/layout/font/system_test.go`:
```go
package font

import (
	"path/filepath"
	"testing"
)

func TestDiskFontProviderLoadsByName(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "testdata", "fonts")
	p := DiskFontProvider{Dir: dir}
	// Case-insensitive, extension-agnostic match on the base name "webfont".
	data, ok := p.LoadLocal("webfont")
	if !ok || len(data) == 0 {
		t.Fatalf("LoadLocal(webfont) ok=%v len=%d, want a hit", ok, len(data))
	}
}

func TestDiskFontProviderMissReturnsFalse(t *testing.T) {
	p := DiskFontProvider{Dir: filepath.Join("..", "..", "..", "testdata", "fonts")}
	if _, ok := p.LoadLocal("no-such-font"); ok {
		t.Fatal("LoadLocal(no-such-font) ok=true, want false")
	}
}

func TestDiskFontProviderEmptyDir(t *testing.T) {
	var p DiskFontProvider // zero value: empty Dir -> never matches, no panic
	if _, ok := p.LoadLocal("webfont"); ok {
		t.Fatal("zero DiskFontProvider matched, want miss")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/font/ -run TestDiskFontProvider -v`
Expected: FAIL — `undefined: DiskFontProvider`.

- [ ] **Step 3: Implement the provider**

Create `pkg/layout/font/system.go`:
```go
package font

import (
	"os"
	"path/filepath"
	"strings"
)

// SystemFontProvider resolves an @font-face local() name to font bytes (raw sfnt
// or a WOFF container — the caller unwraps via font.LoadSFNT). A nil provider, or
// one with no match, means local() does not resolve and the caller tries the next
// src entry.
type SystemFontProvider interface {
	// LoadLocal returns the raw font bytes for a named local face. ok is false when
	// the provider has no such font.
	LoadLocal(name string) (data []byte, ok bool)
}

// fontExts are the file extensions DiskFontProvider recognizes, in preference order.
var fontExts = []string{".ttf", ".otf", ".woff2", ".woff"}

// DiskFontProvider serves local() fonts from a directory, matching name against
// file base names case-insensitively (extension-agnostic). It is the hermetic
// default for tests (point Dir at testdata/) and a simple local resolver. A zero
// value (empty Dir) never matches.
type DiskFontProvider struct {
	Dir string
}

// LoadLocal implements SystemFontProvider.
func (d DiskFontProvider) LoadLocal(name string) ([]byte, bool) {
	if d.Dir == "" || name == "" {
		return nil, false
	}
	want := strings.ToLower(strings.TrimSpace(name))
	for _, ext := range fontExts {
		path := filepath.Join(d.Dir, want+ext)
		if b, err := os.ReadFile(path); err == nil {
			return b, true
		}
	}
	// Fallback: scan the directory for a base-name match (handles a name whose file
	// uses different casing or a space the exact-path probe missed).
	ents, err := os.ReadDir(d.Dir)
	if err != nil {
		return nil, false
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		base := strings.ToLower(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		if base == want {
			if b, err := os.ReadFile(filepath.Join(d.Dir, e.Name())); err == nil {
				return b, true
			}
		}
	}
	return nil, false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/layout/font/ -run TestDiskFontProvider -v`
Expected: PASS.

- [ ] **Step 5: gofmt + lint + commit**

Run (sandbox disabled): `gofmt -l pkg/layout/font/` (nothing); `golangci-lint run ./pkg/layout/font/...` (clean).
```bash
git add pkg/layout/font/system.go pkg/layout/font/system_test.go
git commit -m "layout/font: SystemFontProvider + DiskFontProvider for @font-face local()"
```

---

## Task 8: `FaceCache` resolves `@font-face` sources (the resolution seam)

**Files:**
- Modify: `pkg/layout/font/cache.go`
- Create: `pkg/layout/font/cache_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/layout/font/cache_test.go`:
```go
package font

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	pkgfont "github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// countingLoader wraps a MapLoader and counts Load calls (to prove no re-fetch).
type countingLoader struct {
	inner resource.MapLoader
	calls int32
}

func (c *countingLoader) Load(ctx context.Context, ref string) ([]byte, string, error) {
	atomic.AddInt32(&c.calls, 1)
	return c.inner.Load(ctx, ref)
}

func fontsDir() string { return filepath.Join("..", "..", "..", "testdata", "fonts") }

func TestResolveDownloadedFace(t *testing.T) {
	ttf, err := os.ReadFile(filepath.Join(fontsDir(), "webfont.ttf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	loader := &countingLoader{inner: resource.MapLoader{"my.ttf": {Data: ttf}}}
	faces := []gcss.FontFace{{Family: "My Face", Sources: []gcss.FontSource{{URL: "my.ttf"}}}}
	c := NewFaceCacheWithFonts(faces, loader, nil, nil)

	face, ok := c.Resolve("My Face", pkgfont.Style{})
	if !ok || face == nil {
		t.Fatalf("Resolve(My Face) ok=%v face=%v, want the downloaded face", ok, face)
	}
	// Second resolve must hit the cache, not re-fetch.
	if _, ok := c.Resolve("My Face", pkgfont.Style{}); !ok {
		t.Fatal("second Resolve(My Face) missed")
	}
	if got := atomic.LoadInt32(&loader.calls); got != 1 {
		t.Fatalf("loader called %d times, want 1 (cached)", got)
	}
}

func TestResolveUnknownFamilyFallsBackToBundled(t *testing.T) {
	c := NewFaceCacheWithFonts(nil, nil, nil, nil)
	// "Arial" is a base-14 alias -> LoadStandard returns a bundled face.
	if _, ok := c.Resolve("Arial", pkgfont.Style{}); !ok {
		t.Fatal("Resolve(Arial) miss, want the bundled substitute")
	}
}

func TestResolveFetchFailureCachesFallback(t *testing.T) {
	// @font-face points at a missing url; family is also a base-14 alias so the
	// fallback succeeds and must be cached (no re-fetch on the 2nd call).
	loader := &countingLoader{inner: resource.MapLoader{}} // empty -> 404
	faces := []gcss.FontFace{{Family: "Arial", Sources: []gcss.FontSource{{URL: "missing.ttf"}}}}
	c := NewFaceCacheWithFonts(faces, loader, nil, nil)

	if _, ok := c.Resolve("Arial", pkgfont.Style{}); !ok {
		t.Fatal("Resolve(Arial w/ bad @font-face) miss, want bundled fallback")
	}
	c.Resolve("Arial", pkgfont.Style{})
	if got := atomic.LoadInt32(&loader.calls); got != 1 {
		t.Fatalf("loader called %d times, want 1 (negative result cached)", got)
	}
}

func TestResolveLocalViaSystemProvider(t *testing.T) {
	// local("webfont") resolves via a DiskFontProvider; no url() needed.
	faces := []gcss.FontFace{{Family: "Local Face", Sources: []gcss.FontSource{{Local: "webfont"}}}}
	c := NewFaceCacheWithFonts(faces, nil, DiskFontProvider{Dir: fontsDir()}, nil)
	if _, ok := c.Resolve("Local Face", pkgfont.Style{}); !ok {
		t.Fatal("Resolve(Local Face) miss, want the local disk font")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/font/ -run TestResolve -v`
Expected: FAIL — `undefined: NewFaceCacheWithFonts`.

- [ ] **Step 3: Extend the cache**

Modify `pkg/layout/font/cache.go`. Keep `NewFaceCache` exactly as-is (DOCX + existing callers). Add the web-font state + constructor + resolution, and add the new imports (`context`, `strings`, `gcss`, `resource`):
```go
import (
	"context"
	"strings"
	"sync"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	pkgfont "github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)
```
Add fields to `FaceCache`:
```go
type FaceCache struct {
	mu    sync.Mutex
	faces map[faceKey]cacheEntry

	// Web-font resolution state (nil/empty for bundled-only caches, e.g. DOCX).
	fontFaces map[string][]gcss.FontFace // normalized family -> @font-face entries
	loader    resource.ResourceLoader
	sys       SystemFontProvider
	logf      func(string, ...any)
}
```
Add the constructor:
```go
// NewFaceCacheWithFonts returns a cache that resolves @font-face families to
// downloaded faces before falling back to bundled substitutes. faces are the
// captured @font-face rules (grouped by family); loader fetches url() sources; sys
// resolves local() sources (nil → local() never matches); logf logs degradation
// (nil → no-op). It is safe for concurrent use.
func NewFaceCacheWithFonts(faces []gcss.FontFace, loader resource.ResourceLoader, sys SystemFontProvider, logf func(string, ...any)) *FaceCache {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	byFamily := make(map[string][]gcss.FontFace)
	for _, ff := range faces {
		key := normalizeFamily(ff.Family)
		byFamily[key] = append(byFamily[key], ff)
	}
	return &FaceCache{
		faces:     make(map[faceKey]cacheEntry),
		fontFaces: byFamily,
		loader:    loader,
		sys:       sys,
		logf:      logf,
	}
}

// normalizeFamily lowercases and trims a family name for case-insensitive lookup.
func normalizeFamily(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
```
Change `Resolve` to consult `@font-face` first on a miss:
```go
func (c *FaceCache) Resolve(family string, style pkgfont.Style) (*pkgfont.Face, bool) {
	key := faceKey{family: family, style: style}

	c.mu.Lock()
	defer c.mu.Unlock()
	if e, found := c.faces[key]; found {
		return e.face, e.ok
	}
	// Try @font-face sources for this family first; on any failure (or none), fall
	// back to the bundled substitute. The result — including a miss — is cached, so
	// a failed fetch is not retried per glyph.
	if face, ok := c.resolveFontFace(family, style); ok {
		c.faces[key] = cacheEntry{face: face, ok: true}
		return face, true
	}
	face, ok := pkgfont.LoadStandard(family, style)
	c.faces[key] = cacheEntry{face: face, ok: ok}
	return face, ok
}

// resolveFontFace walks the @font-face entries for family (best style match first),
// trying each source in order: local() via the system provider, url() via the
// loader. The first that decodes wins. Caller holds c.mu.
func (c *FaceCache) resolveFontFace(family string, style pkgfont.Style) (*pkgfont.Face, bool) {
	entries := c.fontFaces[normalizeFamily(family)]
	if len(entries) == 0 {
		return nil, false
	}
	for _, ff := range bestFirst(entries, style) {
		for _, src := range ff.Sources {
			var raw []byte
			switch {
			case src.Local != "":
				if c.sys == nil {
					continue
				}
				b, ok := c.sys.LoadLocal(src.Local)
				if !ok {
					continue
				}
				raw = b
			case src.URL != "":
				if c.loader == nil {
					continue
				}
				b, _, err := c.loader.Load(context.Background(), src.URL)
				if err != nil {
					c.logf("@font-face %q: fetch %q failed: %v", family, src.URL, err)
					continue
				}
				raw = b
			default:
				continue
			}
			face, err := pkgfont.LoadSFNT(raw)
			if err != nil {
				c.logf("@font-face %q: decode failed: %v", family, err)
				continue
			}
			return face, true
		}
	}
	return nil, false
}

// bestFirst orders @font-face entries so the one best matching style comes first:
// exact weight+style, then a regular/unspecified entry, then the rest in source
// order. (A coarse match — full font-weight numeric matching is a deferral.)
func bestFirst(entries []gcss.FontFace, style pkgfont.Style) []gcss.FontFace {
	wantBold := style.Bold
	wantItalic := style.Italic
	score := func(ff gcss.FontFace) int {
		ffBold := ff.Weight == "bold" || ff.Weight == "700"
		ffItalic := ff.Style == "italic" || ff.Style == "oblique"
		s := 0
		if ffBold == wantBold {
			s += 2
		}
		if ffItalic == wantItalic {
			s++
		}
		return s
	}
	out := make([]gcss.FontFace, len(entries))
	copy(out, entries)
	// Stable sort by descending score (keep source order within equal scores).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && score(out[j]) > score(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
```
(Note: the insertion sort keeps source order for equal scores — equivalent to a stable sort — and avoids the `sort.Slice`/modernize friction; it is fine for the handful of entries a family has.)

- [ ] **Step 4: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/layout/font/ -v`
Expected: PASS — including the existing `Resolve`/bundled tests (NewFaceCache unchanged).

- [ ] **Step 5: gofmt + lint + commit**

Run (sandbox disabled): `gofmt -l pkg/layout/font/` (nothing); `golangci-lint run ./pkg/layout/font/...` (clean). Then confirm nothing else broke: `go build ./...`.
```bash
git add pkg/layout/font/cache.go pkg/layout/font/cache_test.go
git commit -m "layout/font: FaceCache resolves @font-face (url()/local()) before bundled fallback"
```

---

## Task 9: Thread the `@font-face` table from box-gen to the engine

**Files:**
- Modify: `pkg/layout/css/build.go`
- Create/extend: `pkg/layout/css/build_test.go` (a font-face aggregation test)

- [ ] **Step 1: Write the failing test**

Append to the existing `pkg/layout/css/build_test.go` (it is already `package css` and already imports `context` + `github.com/nathanstitt/doctaculous/pkg/html` — the existing tests call `html.Parse([]byte(src))`; reuse that exactly):
```go
func TestBuildWithFontsCollectsFontFaces(t *testing.T) {
	src := `<!DOCTYPE html><html><head><style>
		@font-face { font-family: "Doc Face"; src: url(doc.ttf) }
		p { font-family: "Doc Face" }
	</style></head><body><p>hi</p></body></html>`
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	_, faces, err := BuildWithFonts(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("BuildWithFonts: %v", err)
	}
	if len(faces) != 1 || faces[0].Family != "Doc Face" || len(faces[0].Sources) != 1 {
		t.Fatalf("collected faces = %+v, want one Doc Face with one source", faces)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run TestBuildWithFonts -v`
Expected: FAIL — `undefined: BuildWithFonts`.

- [ ] **Step 3: Add `BuildWithFonts` and font-face aggregation; keep `Build` stable**

In `pkg/layout/css/build.go`: change `assembleSheets` to also return the aggregated font faces, and add `BuildWithFonts` that `Build` delegates to. First, have `assembleSheets` collect faces (it already iterates every sheet):
```go
// assembleSheets returns the origin-ordered sheets AND the aggregated @font-face
// rules across all of them (UA + <style> + resolvable <link>). Font faces are
// collected here because this is where every sheet is parsed.
func assembleSheets(ctx context.Context, doc *html.Document, loader resource.ResourceLoader, logf func(string, ...any)) ([]gcss.OriginSheet, []gcss.FontFace) {
	sheets := []gcss.OriginSheet{{Sheet: html.UAStylesheet, Origin: gcss.OriginUA}}
	var faces []gcss.FontFace
	faces = append(faces, html.UAStylesheet.FontFaces...)
	for _, s := range doc.StyleSheets {
		sheets = append(sheets, gcss.OriginSheet{Sheet: s, Origin: gcss.OriginAuthor})
		faces = append(faces, s.FontFaces...)
	}
	if loader != nil {
		for _, ref := range doc.LinkRefs {
			data, _, err := loader.Load(ctx, ref)
			if err != nil {
				logf("link stylesheet %q: %v (skipped)", ref, err)
				continue
			}
			parsed := gcss.Parse(string(data))
			sheets = append(sheets, gcss.OriginSheet{Sheet: parsed, Origin: gcss.OriginAuthor})
			faces = append(faces, parsed.FontFaces...)
		}
	}
	return sheets, faces
}
```
Refactor `Build` into `BuildWithFonts` + a thin `Build` wrapper:
```go
// Build generates a cssbox tree from a parsed HTML document (see BuildWithFonts;
// this form discards the collected @font-face table for callers that do not need
// it). Signature unchanged for existing callers.
func Build(ctx context.Context, doc *html.Document, loader resource.ResourceLoader, logf func(string, ...any)) (*cssbox.Box, error) {
	root, _, err := BuildWithFonts(ctx, doc, loader, logf)
	return root, err
}

// BuildWithFonts is Build plus the aggregated @font-face rules collected from every
// origin sheet (UA + <style> + <link>), so the caller can hand them to the face
// cache. It never panics on malformed input: a recover at the entry boundary
// returns whatever tree was built so far (and the faces collected so far).
func BuildWithFonts(ctx context.Context, doc *html.Document, loader resource.ResourceLoader, logf func(string, ...any)) (root *cssbox.Box, faces []gcss.FontFace, err error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	defer func() {
		if r := recover(); r != nil {
			logf("box generation recovered from panic: %v", r)
			if root == nil {
				root = &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
			}
			err = nil
		}
	}()

	sheets, faces := assembleSheets(ctx, doc, loader, logf)
	resolver := gcss.NewResolver(sheets, logf)

	root = generate(doc.Root, resolver, resolver.ComputeRoot(doc.Root))
	if root == nil {
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC}, faces, nil
	}
	normalize(root)
	fixupTables(root)
	return root, faces, nil
}
```
(`html.UAStylesheet` is `css.Parse(uaSource)` — a `gcss.Stylesheet` value (`pkg/html/ua.go:41`), so `.FontFaces` is type-correct; it is empty for the UA sheet, so that append is harmless future-proofing. Keep the line.)

- [ ] **Step 4: Run test to verify it passes (and existing build tests)**

Run (sandbox disabled): `go test ./pkg/layout/css/ -v`
Expected: PASS — including all existing `Build(...)` call sites (signature unchanged).

- [ ] **Step 5: gofmt + lint + commit**

Run (sandbox disabled): `gofmt -l pkg/layout/css/` (nothing); `golangci-lint run ./pkg/layout/css/...` (clean).
```bash
git add pkg/layout/css/build.go pkg/layout/css/build_test.go
git commit -m "layout/css: BuildWithFonts collects @font-face across origin sheets (Build unchanged)"
```

---

## Task 10: Wire web fonts into the HTML backend + `WithSystemFontProvider`

**Files:**
- Modify: `pkg/doctaculous/html_backend.go`
- Create: `pkg/doctaculous/html_webfont_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/doctaculous/html_webfont_test.go`:
```go
package doctaculous

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// A page whose only font is an @font-face family served by a MapLoader must render
// without error and produce a non-empty page (the substitution path is exercised;
// the golden test proves the glyphs visually).
func TestOpenHTMLWithWebFont(t *testing.T) {
	ttf, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fonts", "webfont.ttf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	html := []byte(`<!DOCTYPE html><html><head><style>
		@font-face { font-family: "Web Face"; src: url(web.ttf) }
		body { margin: 0; font-family: "Web Face"; font-size: 40px }
	</style></head><body>Web font AaGg</body></html>`)
	loader := resource.MapLoader{"web.ttf": {Data: ttf}}
	doc, err := OpenHTMLBytes(html, WithResourceLoader(loader))
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	if doc == nil {
		t.Fatal("OpenHTMLBytes returned nil document")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/doctaculous/ -run TestOpenHTMLWithWebFont -v`
Expected: it may COMPILE-FAIL only if `WithSystemFontProvider` is referenced; this test does not reference it yet, so it likely PASSES already via the existing path **but without using the downloaded face** (the engine still builds a plain `NewFaceCache`). To make it a true failing test first, temporarily assert the rendered page height is larger than a base-14 render — simpler: proceed to Step 3 (wire it), then rely on the golden in Task 11 for the visual proof. Mark this test as the integration smoke test.

- [ ] **Step 3: Wire `BuildWithFonts` + `NewFaceCacheWithFonts` + the option**

In `pkg/doctaculous/html_backend.go`: add the system-provider field + option, and switch the pipeline to the font-aware calls:
```go
type htmlConfig struct {
	viewportPt float64
	loader     resource.ResourceLoader
	sys        layoutfont.SystemFontProvider
	logf       func(string, ...any)
}
```
```go
// WithSystemFontProvider sets the provider used to resolve @font-face local()
// sources. Defaults to nil (local() never matches; the next src is tried). OpenHTML
// supplies a DiskFontProvider rooted at the document's directory.
func WithSystemFontProvider(p layoutfont.SystemFontProvider) HTMLOption {
	return func(c *htmlConfig) { c.sys = p }
}
```
In `OpenHTML` (path form), default the provider to the document's directory (mirroring the loader default):
```go
func OpenHTML(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open html %q: %w", path, err)
	}
	dir := filepath.Dir(path)
	return OpenHTMLBytes(data,
		WithResourceLoader(resource.DirLoader{Base: dir}),
		WithSystemFontProvider(layoutfont.DiskFontProvider{Dir: dir}),
	)
}
```
In `htmlDocument`, use the font-aware build + cache:
```go
func htmlDocument(data []byte, cfg htmlConfig) (*Document, error) {
	doc, err := html.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: parse html: %w", err)
	}
	ctx := context.Background()
	root, fontFaces, err := layoutcss.BuildWithFonts(ctx, doc, cfg.loader, cfg.logf)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: build html boxes: %w", err)
	}
	faces := layoutfont.NewFaceCacheWithFonts(fontFaces, cfg.loader, cfg.sys, cfg.logf)
	engine := layoutcss.New(faces, cfg.loader, cfg.logf)
	pages, err := engine.Layout(ctx, root, cfg.viewportPt)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: layout html: %w", err)
	}
	return &Document{r: &reflowRenderer{pages: pages}}, nil
}
```

- [ ] **Step 4: Run the smoke test + the whole package**

Run (sandbox disabled): `go test ./pkg/doctaculous/ -run TestOpenHTMLWithWebFont -v` (PASS) and `go test ./pkg/doctaculous/...` (all green, no golden changes yet).

- [ ] **Step 5: Byte-identical guard + gofmt + lint + commit**

Run (sandbox disabled): `go test ./pkg/doctaculous/... ./pkg/render/raster/...` (no `-update`), then `git status --short pkg/doctaculous/testdata pkg/render/raster/testdata` — must be **empty** (no existing golden changed). `gofmt -l pkg/doctaculous/` (nothing); `golangci-lint run ./pkg/doctaculous/...` (clean).
```bash
git add pkg/doctaculous/html_backend.go pkg/doctaculous/html_webfont_test.go
git commit -m "doctaculous: wire @font-face resolution into OpenHTML (WithSystemFontProvider)"
```

---

## Task 11: Golden image — a page rendered with the downloaded face

**Files:**
- Modify: `pkg/doctaculous/html_golden_test.go` (add a `webfont` entry; `os`/`filepath`/`resource` are already imported there)
- Create: `pkg/doctaculous/testdata/golden/html-webfont.png` (via `-update`, after eyeball) — note the harness names goldens `html-<name>.png` directly under `testdata/golden/` (see `html_golden_test.go:463`)

- [ ] **Step 1: Add the golden fixture entry**

In `pkg/doctaculous/html_golden_test.go`, add to the `htmlGoldens` slice (use the WOFF2 fixture so the golden exercises the full decode path; serve it via a `MapLoader`):
```go
{
	// Web font: text rendered with an @font-face family served from memory. The
	// glyph shapes are visibly distinct from the base-14 substitutes, proving the
	// downloaded face is used (not LoadStandard).
	name:       "webfont",
	viewportPx: 360,
	html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  @font-face { font-family: "Web Face"; src: url(web.woff2) format("woff2"); }
  p { font-family: "Web Face", sans-serif; font-size: 48px; color: #202020; }
</style></head><body>
  <p>Web Font AaGg</p>
</body></html>`,
	loader: webfontGoldenLoader(),
},
```
Add a small helper near the top of the test file (reads the committed WOFF2 once). The test runs with the working directory at `pkg/doctaculous`, so the repo-root fixture is two levels up:
```go
// webfontGoldenLoader serves the committed WOFF2 fixture as web.woff2 for the
// web-font golden. It panics on a missing fixture (a test-setup error).
func webfontGoldenLoader() resource.ResourceLoader {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fonts", "webfont.woff2"))
	if err != nil {
		panic("webfont golden fixture: " + err.Error())
	}
	return resource.MapLoader{"web.woff2": {Data: data}}
}
```
(`os`, `path/filepath`, and `resource` are already imported in `html_golden_test.go` — no import changes needed.)

- [ ] **Step 2: Generate the golden (sandbox disabled), then STOP for eyeball**

Run (sandbox disabled): `go test ./pkg/doctaculous -run TestHTMLGolden -update`
Then **STOP**. Report the new file path `pkg/doctaculous/testdata/golden/html-webfont.png` to the controller and **hand back for visual review** — the implementer has no image vision. Do not commit until the controller confirms the PNG shows the custom glyphs (distinct from a base-14 render).

- [ ] **Step 3 (controller): eyeball the PNG**

The controller Reads `pkg/doctaculous/testdata/golden/html-webfont.png` and confirms: text "Web Font AaGg" renders with the fixture font's distinctive letterforms (not the TeX Gyre/Inconsolata base-14 shapes). If it looks like a base-14 render, resolution did not take effect — debug before committing.

- [ ] **Step 4: Verify the golden is stable + byte-identical guard**

Run (sandbox disabled): `go test ./pkg/doctaculous -run TestHTMLGolden -v` (PASS against the just-committed PNG). Then `git status --short pkg/doctaculous/testdata pkg/render/raster/testdata` — the **only** change is the NEW `webfont.png` (plus its entry already committed in code). No existing golden changed.

- [ ] **Step 5: Commit**

```bash
git add pkg/doctaculous/html_golden_test.go pkg/doctaculous/testdata/golden/html-webfont.png
git commit -m "doctaculous: web-font golden (text rendered with the downloaded @font-face)"
```

---

## Task 12: Degradation tests (the full deferral/fallback matrix)

**Files:**
- Modify: `pkg/doctaculous/html_webfont_test.go`
- Modify: `pkg/layout/font/cache_test.go`

- [ ] **Step 1: Write the degradation tests**

Append to `pkg/doctaculous/html_webfont_test.go`:
```go
// A 404 font url + a non-base-14 family: the run degrades (no panic); the document
// still renders. (The text may render blank since no bundled substitute exists for
// the made-up family — that is the documented graceful skip.)
func TestWebFont404Degrades(t *testing.T) {
	html := []byte(`<!DOCTYPE html><html><head><style>
		@font-face { font-family: "Ghost"; src: url(missing.woff2) }
		p { font-family: "Ghost" }
	</style></head><body><p>nothing to see</p></body></html>`)
	loader := resource.MapLoader{} // 404 for everything
	if _, err := OpenHTMLBytes(html, WithResourceLoader(loader)); err != nil {
		t.Fatalf("OpenHTMLBytes degraded to an error, want graceful render: %v", err)
	}
}

// A corrupt font payload degrades to the bundled fallback (family is a base-14
// alias), no panic.
func TestWebFontCorruptDegrades(t *testing.T) {
	html := []byte(`<!DOCTYPE html><html><head><style>
		@font-face { font-family: "Arial"; src: url(bad.woff2) }
		p { font-family: "Arial" }
	</style></head><body><p>fallback please</p></body></html>`)
	loader := resource.MapLoader{"bad.woff2": {Data: []byte("wOF2 not really")}}
	if _, err := OpenHTMLBytes(html, WithResourceLoader(loader)); err != nil {
		t.Fatalf("corrupt web font caused an error, want graceful fallback: %v", err)
	}
}

// Deferred descriptors present but ignored: the font still resolves.
func TestWebFontIgnoredDescriptors(t *testing.T) {
	ttf, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fonts", "webfont.ttf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	html := []byte(`<!DOCTYPE html><html><head><style>
		@font-face {
			font-family: "Web Face"; src: url(web.ttf);
			unicode-range: U+0000-00FF; font-display: swap;
			font-variation-settings: "wght" 700;
		}
		p { font-family: "Web Face"; font-size: 30px }
	</style></head><body><p>AaGg</p></body></html>`)
	loader := resource.MapLoader{"web.ttf": {Data: ttf}}
	if _, err := OpenHTMLBytes(html, WithResourceLoader(loader)); err != nil {
		t.Fatalf("ignored descriptors caused an error: %v", err)
	}
}
```
Append to `pkg/layout/font/cache_test.go`:
```go
// Missing variant: @font-face supplies only regular; a bold request falls back to
// the bundled substitute (family is a base-14 alias).
func TestResolveMissingVariantFallsBack(t *testing.T) {
	ttf, err := os.ReadFile(filepath.Join(fontsDir(), "webfont.ttf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// Regular-only @font-face for "Arial"; request Bold.
	loader := resource.MapLoader{"r.ttf": {Data: ttf}}
	faces := []gcss.FontFace{{Family: "Arial", Sources: []gcss.FontSource{{URL: "r.ttf"}}}}
	c := NewFaceCacheWithFonts(faces, loader, nil, nil)
	// Regular resolves to the downloaded face; bold also resolves (downloaded face
	// reused as the coarse best match) — both non-nil, no panic.
	if _, ok := c.Resolve("Arial", pkgfont.Style{}); !ok {
		t.Fatal("regular miss")
	}
	if _, ok := c.Resolve("Arial", pkgfont.Style{Bold: true}); !ok {
		t.Fatal("bold miss, want a resolved face (downloaded or bundled)")
	}
}

// local() with no provider skips to the next src (a url()).
func TestResolveLocalNoProviderFallsToURL(t *testing.T) {
	ttf, err := os.ReadFile(filepath.Join(fontsDir(), "webfont.ttf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	loader := resource.MapLoader{"u.ttf": {Data: ttf}}
	faces := []gcss.FontFace{{Family: "X", Sources: []gcss.FontSource{{Local: "nope"}, {URL: "u.ttf"}}}}
	c := NewFaceCacheWithFonts(faces, loader, nil, nil) // nil provider
	if _, ok := c.Resolve("X", pkgfont.Style{}); !ok {
		t.Fatal("Resolve(X) miss, want the url() fallback after local() skip")
	}
}
```

- [ ] **Step 2: Run the tests**

Run (sandbox disabled): `go test ./pkg/doctaculous/ -run 'TestWebFont' -v` and `go test ./pkg/layout/font/ -run 'TestResolve' -v`
Expected: PASS — no panics; fallbacks resolve.

- [ ] **Step 3: Full suite + race + byte-identical guard**

Run (sandbox disabled):
```bash
go test ./...
go test -race ./pkg/font/... ./pkg/layout/font/... ./pkg/doctaculous/...
git status --short pkg/doctaculous/testdata pkg/render/raster/testdata
```
Expected: all green; race clean; the status shows no modified existing goldens.

- [ ] **Step 4: gofmt + lint + commit**

Run (sandbox disabled): `gofmt -l pkg/doctaculous/ pkg/layout/font/` (nothing); `golangci-lint run ./pkg/doctaculous/... ./pkg/layout/font/...` (clean).
```bash
git add pkg/doctaculous/html_webfont_test.go pkg/layout/font/cache_test.go
git commit -m "test: web-font degradation matrix (404, corrupt, ignored descriptors, missing variant, local skip)"
```

---

## Task 13: WPT-style reftest (best-effort) + WOFF2 transformed-glyf adversarial coverage

**Files:**
- Modify (if a reftest fits): `pkg/doctaculous/wpt_reftest_test.go` + `NAME.html` / `NAME-ref.html`

- [ ] **Step 1: Decide whether a reftest fits**

A reftest needs a reference page that produces the *same pixels* by a different route. For web fonts, the workable shape: the test page sets the text via the `@font-face` family on a `<p>`; the reference sets the **same family as the document default** (e.g. on `body`) so both hit the same downloaded face — identical pixels. If that holds in this engine, author it; otherwise skip (goldens + the unit round-trip already prove correctness) and note the skip.

- [ ] **Step 2 (if authoring): add the reftest**

Look at an existing pair in `pkg/doctaculous/testdata/` referenced by `wptReftests` for the exact directory + naming, then add `webfont.html` / `webfont-ref.html` (both serving the WOFF2 via the harness's loader) and a `wptReftests` entry. Run (sandbox disabled): `go test ./pkg/doctaculous -run TestWPTReftests -v` — PASS.

- [ ] **Step 3: Commit (only if a reftest was added)**

```bash
git add pkg/doctaculous/testdata pkg/doctaculous/wpt_reftest_test.go
git commit -m "doctaculous: web-font reftest (@font-face family == document default)"
```

(If skipped: no commit; record in the PR description that a reftest does not fit web fonts and why.)

---

## Task 14: Holistic review, docs, finish

**Files:**
- Modify: `CLAUDE.md`
- Modify: `docs/superpowers/HANDOVER-subproject-8-webfonts.md` (mark Done, or add a follow-up note for deferred WOFF2 edge cases if any)

- [ ] **Step 1: Full verification sweep (sandbox disabled)**

```bash
go build ./...
go test ./...
go test -race ./...
gofmt -l pkg/css/ pkg/font/ pkg/layout/ pkg/doctaculous/
golangci-lint run ./pkg/css/... ./pkg/font/... ./pkg/layout/... ./pkg/doctaculous/...
find . -name 'zz_*' -o -name '*probe*'   # must be empty
git status --short                        # clean except intended files
```
Expected: all green, lint/gofmt clean, no scratch files, no unexpected modifications.

- [ ] **Step 2: Render a real page for a final eyeball (controller)**

The controller renders the web-font golden fixture (or a fresh small page) end-to-end and Reads the PNG to confirm the downloaded glyphs render correctly at a glance — visible bugs are caught by rendering, not unit tests.

- [ ] **Step 3: Update CLAUDE.md**

In CLAUDE.md "Approved deps" (Non-negotiable constraints), add `github.com/andybalholm/brotli` (MIT) with the reason (WOFF2 Brotli decompression). Add a new **Done** bullet describing: `@font-face` capture (`pkg/css`), `LoadSFNT` + WOFF1 (zlib) + WOFF2 (Brotli + glyf/loca transform) decode (`pkg/font`), `SystemFontProvider`/`DiskFontProvider` for `local()`, `FaceCache` `@font-face` resolution (lazy, cached, negative results) → bundled fallback, the threading through `BuildWithFonts` + `NewFaceCacheWithFonts`, what goldens/tests cover, and the deferrals (synthetic bold/oblique, `unicode-range`, `font-display`, variable axes, `local()` beyond the disk adapter). Remove "web fonts" from the §6 remaining-slices list (update the parenthetical that lists done slices).

- [ ] **Step 4: Commit docs**

```bash
git add CLAUDE.md docs/superpowers/HANDOVER-subproject-8-webfonts.md
git commit -m "docs: record web fonts in CLAUDE.md (Done + deferrals; brotli dep) "
```

- [ ] **Step 5: Finish the branch**

Use the superpowers:finishing-a-development-branch skill to open the stacked PR (off `feat/html-tables`, or rebased onto `main` if the stack merged). PR description: short, no Claude credit; **include the dependency rationale** (why `andybalholm/brotli`, why not the shuralyov package), the fixture font's provenance + license, and the deferral list. Eyeball-confirm the golden PNG is in the diff.

---

## Self-review notes (coverage map — spec → task)

- `@font-face` capture (spec §`pkg/css`) → Tasks 1–2.
- Raw sfnt / WOFF1 / WOFF2 decode incl. glyf transform (spec §`pkg/font`) → Tasks 3–6.
- New Brotli dep w/ rationale (spec "Dependency decision") → Task 5 (+ CLAUDE.md in Task 14).
- `local()` via `SystemFontProvider`/`DiskFontProvider` (spec §`pkg/layout/font`) → Task 7 (+ resolution in Task 8).
- `FaceCache` `@font-face` resolution, lazy + cached + negative (spec §`pkg/layout/font`) → Task 8.
- Threading box-gen → engine (spec §`pkg/layout/css` + `pkg/doctaculous`) → Tasks 9–10.
- `WithSystemFontProvider` option + `OpenHTML` disk-provider default (spec §threading) → Task 10.
- Fixtures w/ provenance + CC→README (spec §Fixtures) → Task 0.
- Golden, eyeballed (spec §Golden) → Task 11.
- Degradation matrix (spec §Degradation) → Task 12.
- Reftest best-effort (spec §WPT) → Task 13.
- Byte-identical guard (spec §"byte-identical") → every rendering task + Tasks 10/11/12.
- CLAUDE.md update (spec "Process reminders") → Task 14.
