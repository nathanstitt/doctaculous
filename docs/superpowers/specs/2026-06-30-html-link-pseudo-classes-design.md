# Link pseudo-classes `:link` / `:visited` (+ pseudo-class parsing) — design (HTML engine feature 4/4)

**Goal:** Support the link pseudo-classes in CSS selectors — `a:link { color: ... }` styles
unvisited links, the near-universal way real pages color their links — plus parse (and
correctly no-op) the other common pseudo-classes so a real stylesheet's `:hover`/`:focus`/
`:visited` rules don't break selector matching. Add the UA default `<a href>` style (blue,
underlined) so links look like links even without author CSS.

**Scope:** selector **parsing** of `:pseudo-class` suffixes on a simple selector, and
**matching** for the static-render subset:
- `:link` → matches a *hyperlink* element with an `href` (`<a>`, `<area>`, `<link>`); in a
  no-history renderer every hyperlink is unvisited, so `:link` = "is a link".
- `:visited` → matches **nothing** (no browsing history; also the standard privacy stance).
- `:hover`, `:focus`, `:active`, `:focus-visible`, `:focus-within`, `:target`, `:enabled`,
  `:disabled`, `:checked` and any other recognized dynamic/state pseudo-class → matches
  nothing in a static render (parsed so the rule is still valid, the selector just doesn't
  match) — except `:disabled`/`:checked`/`:enabled`, which we could honor from attributes but
  defer (form controls already read those attributes directly in box-gen; a selector form is a
  follow-up).
- Unknown / unsupported pseudo-classes and **all pseudo-elements** (`::before`, `:after`, …) →
  the simple selector is **dropped** (parse returns ok=false for that selector), so the rule's
  other selectors still apply and we never falsely match. (`content`-driven `::before`/`::after`
  generated content is a separate feature.)

**Functional pseudo-classes** (`:not()`, `:nth-child()`, …) are out of scope — they need
argument parsing; for now a `:pseudo(` form drops the selector (graceful).

This is feature 4/4 of the autonomous fidelity program. It is **additive and byte-identical**
for any stylesheet with no pseudo-class selectors (the parser path is unchanged for them), and
the only new always-on effect is the UA `a:link` default — which changes link color/underline
on pages that have links and don't override it (intended; that is the fix).

## Background (what exists)

- `simpleSelector{tag, id, classes}` (`pkg/css/selector.go`); `parseSimple` scans a compound
  selector splitting on `.`/`#`. `Selector.Specificity()` counts ids/classes/types.
  `simpleSelector.matches(Node)` checks tag/id/classes. `Node` already exposes
  `Attr(key) (string, bool)` (so `href` is reachable) and `Tag()`.
- The cascade (`ComputeRoot`/cascade) consumes `Selector.Matches`; a pseudo-class that matches
  flows through the existing specificity + origin ordering unchanged.
- The UA stylesheet is `uaSource` in `pkg/html/ua.go` (parsed at `OriginUA`).

## Architecture

Two small changes, both in `pkg/css/selector.go`, plus one UA rule:

1. **Parse** pseudo-class suffixes. Extend `simpleSelector` with `pseudos []string` (the
   lowercased pseudo-class names, e.g. `["link"]`). Extend `parseSimple` to recognize `:` as a
   third marker (alongside `.`/`#`): read the pseudo name, and
   - if it is a **pseudo-element** (`::name`, or the legacy `:before`/`:after`/`:first-line`/
     `:first-letter`) → return ok=false (drop this selector);
   - if it is a **functional** pseudo (`name(` …) → return ok=false (drop; out of scope);
   - else record it in `pseudos`.
   A `:` with an empty name, or a trailing `:`, is a parse error (drop).
2. **Match** them. In `simpleSelector.matches`, after the tag/id/class checks, every recorded
   pseudo must match:
   - `link` → `isHyperlink(n)` (tag is a/area/link AND has a non-empty `href`).
   - `visited` → always false.
   - everything else recognized as a **static-false** dynamic/state pseudo (hover/focus/active/
     target/checked/disabled/enabled/…): false.
   - an unrecognized pseudo that slipped past parse: false (defensive).
   Because an unmatched pseudo makes `matches` return false, a `:hover` rule simply never
   applies — correct for a static render.
3. **Specificity.** A pseudo-class counts as a class (CSS): add `len(p.pseudos)` to
   `sp.Classes` in `Selector.Specificity()`. (Pseudo-elements would count as a type, but we
   drop those, so no type contribution.)
4. **UA default.** Add to `uaSource`: `a:link { color: #0000ee; text-decoration: underline; }`
   (the classic browser link style). Use `a:link` (not bare `a`) so it only colors actual
   hyperlinks, and so an author `a { color: … }` (type selector, lower specificity than the UA
   `a:link`'s class-level)… — note: the UA rule is `OriginUA`, below all author rules, so any
   author `a`/`a:link` rule still wins regardless of specificity. Pin this with a test.

`text-decoration: underline` is **not yet supported** (no `ComputedStyle` field, no paint —
`pkg/layout` references "underline" only in a `RuleKind` comment; even DOCX's `box.Underline`
is not actually emitted). This design therefore lands **`text-decoration: underline|none` for
the CSS/HTML engine**:
- `ComputedStyle.TextDecorationLine string` ("none" initial; "underline" the supported value;
  inherited like color — CSS text-decoration is not inherited per se but propagates to inline
  descendants via the decorating box; modeling it as inherited is the pragmatic approximation
  the engine uses for color too). Parse `text-decoration` (shorthand) and
  `text-decoration-line`, taking the `underline` keyword; `none` clears it; other keywords
  (overline/line-through/wavy/colors) are ignored (kept as none/underline only).
- Paint: the CSS inline formatting context emits, per run of underlined glyphs on a line, a
  thin `RuleKind` rectangle at `baselineY + underlineOffset` spanning the run's x-range, in the
  run's color. Offset/thickness derive from the font size (≈ 0.1em thick, ≈ 0.08–0.12em below
  the baseline) — a simple, browser-plausible underline. This reuses the existing `RuleKind`
  primitive (no new item kind, no painter change).

This is the maximal choice (browser links are blue **and** underlined). The flat/DOCX
underline stays out of scope (a separate concern; `box.Underline` is unaffected).

## Error handling / degradation

- A selector containing a pseudo-element or functional/unknown-syntax pseudo is dropped (its
  rule's other comma-separated selectors are unaffected — `parseSelectorList` already isolates
  each). No panic.
- `:visited` and dynamic pseudos never match → their rules are inert (the safe, correct static
  behavior), not errors.

## Tests

- **Selector parse** (`pkg/css/selector_test.go`): `a:link`, `a:visited`, `a:hover`,
  `.x:focus`, `div::before` (dropped), `:nth-child(2)` (dropped), `a:link:hover` (multi),
  `a:` (error). Assert `pseudos` captured and specificity (`a:link` = (0,1,1)).
- **Selector match** (`pkg/css/selector_test.go` with the test DOM): `a:link` matches `<a
  href>` but not `<a>` (no href) nor `<span>`; `a:visited` matches neither; `a:hover` matches
  neither.
- **Cascade** (`pkg/css/cascade_test.go`): `a:link { color: red }` colors a linked `<a>`; an
  author rule beats the UA `a:link` default.
- **UA default** (`pkg/html/ua_test.go` or cascade): a bare `<a href>` with no author CSS gets
  the blue underlined link style; a `<a>` without href does not.
- **text-decoration** (if landed): parse `underline`/`none`; an underline rule is emitted under
  a linked line (paint/fragment assertion).
- **HTML golden + showcase**: a `html-link` golden (a paragraph with a styled `<a href>` link)
  and a "13 / LINKS" showcase section (default link, a custom `a:link` color, a `:visited`
  rule shown inert). Regenerate the paginated goldens; eyeball.

## Out of scope (follow-ups)

Functional pseudo-classes (`:not`, `:nth-*`, `:is`, `:where`); pseudo-elements and
`::before`/`::after` generated content; honoring `:checked`/`:disabled`/`:enabled` from
attributes in selectors (box-gen already reads the attributes); `:hover`/`:focus`/`:active`
interactivity (no live state in a static render); `text-decoration` line styles beyond
underline/none (overline, line-through, wavy, color) if underline is the only one landed.
