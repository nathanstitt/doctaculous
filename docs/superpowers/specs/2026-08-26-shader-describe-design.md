# Native PDF Shadings via a Describable Shader — Design

**Date:** 2026-08-26
**Status:** Approved design (user-directed), pending implementation
**Base branch:** `feat/shader-describe`, stacked on `feat/svg-paint-servers` (PR #104)

## Goal

SVG gradients converted to PDF emit a native `/Shading` dictionary — real vector
output that scales losslessly and stays small — instead of being rasterized into
an image XObject.

Today `pkg/render/pdfwrite`'s `FillShading` samples the shader into an RGBA image
(shipped in PR #104, which was itself a strict improvement over the previous
no-op stub that rendered gradients **blank**). Rasterizing is correct but lossy:
it fixes resolution at write time, inflates file size, and discards the vector
nature of the gradient.

## Why it cannot be done today

`render.Shader` is an opaque one-method interface:

```go
type Shader interface {
	ColorAt(userX, userY float64) (c color.RGBA, ok bool)
}
```

A backend can *sample* it but cannot *recover* what it is — the axis endpoints,
the circles, the stops, the spread method. A PDF `/Shading` dictionary needs all
of those. So the writer's only options were "sample it" or "emit nothing".

## Design

**Keep `Shader` exactly as it is.** Add an OPTIONAL companion interface that a
backend type-asserts for. Every existing implementation keeps working untouched;
a backend that does not care never notices.

```go
// ShadingKind distinguishes the gradient geometries a Describer can report.
type ShadingKind int

const (
	ShadingAxial ShadingKind = iota  // linear: two endpoints
	ShadingRadial                    // radial: focal circle -> outer circle
)

// SpreadMode is what happens outside the [0,1] parameter range.
type SpreadMode int

const (
	SpreadPad SpreadMode = iota
	SpreadReflect
	SpreadRepeat
)

// ShadingStop is one color stop on the gradient ramp.
type ShadingStop struct {
	Offset float64    // [0,1], non-decreasing
	Color  color.RGBA // straight (non-premultiplied) RGBA
}

// ShadingDesc is a backend-recoverable description of a gradient.
type ShadingDesc struct {
	Kind   ShadingKind
	Coords [6]float64 // axial: x0,y0,x1,y1 (last two unused); radial: fx,fy,fr,cx,cy,cr
	Stops  []ShadingStop
	Spread SpreadMode
}

// ShadingDescriber is an optional companion to Shader. A Shader that can
// describe its own geometry implements it so a vector backend (the PDF writer)
// can emit a native shading instead of sampling. Backends MUST fall back to
// ColorAt sampling when a Shader does not implement it — the interface is an
// optimization, never a requirement.
type ShadingDescriber interface {
	Shader
	DescribeShading() (ShadingDesc, bool)
}
```

`ok=false` from `DescribeShading` means "I could describe shadings in principle
but not this one" (e.g. a mesh shading), so a describer can decline per-instance
without the backend needing to know its concrete type.

### The wrapper problem — the one non-obvious constraint

`pkg/svg/draw`'s `alphaShader` wraps a gradient to fold element/group opacity
into it, because `Device.FillShading` takes no alpha parameter. That wrapper is
on the **common** SVG path — any gradient under a `<g opacity>` or with its own
`opacity` goes through it. A naive type-assert on the outer shader would miss the
describer underneath and silently fall back to rasterizing exactly the documents
most likely to have gradients.

`alphaShader` therefore implements `ShadingDescriber` by delegating: it calls the
inner describer and scales each stop's alpha by its factor. Any future wrapper
must do the same, and that obligation is stated on the interface's doc comment.

### Who implements what

| Type | Implements | Notes |
|---|---|---|
| `raster.shading` (axial/radial) | `ShadingDescriber` | Reports its own coords/stops/spread. Needs the stop list retained at construction — see below. |
| `raster.meshShading` (PDF types 4-7) | no | Genuinely not describable in this form; PDF input already has its own dict. |
| `svg/draw.alphaShader` | `ShadingDescriber` | Delegates, scaling stop alpha. |
| PDF-constructed shadings | `ok=false` or no impl | A PDF *input* shading round-tripping to PDF *output* is out of scope here; it already has a source dictionary. |

**Stop retention.** `NewAxialShader`/`NewRadialShader` currently take a
`function.Func` ramp, which is opaque. They gain the stop list alongside it (the
SVG side already has it — `pkg/svg/stops.go` builds the ramp from exactly these
stops), stored for description only; `ColorAt` keeps using the function
unchanged, so sampling behavior is byte-identical.

### The PDF side

`pkg/render/pdfwrite` gains shading emission:

- A `/ShadingType 2` (axial) or `3` (radial) dictionary with `/Coords`,
  `/ColorSpace /DeviceRGB`, `/Extend`, and a `/Function`.
- Stops become a **stitching function** (`/FunctionType 3`) over per-segment
  exponential functions (`/FunctionType 2`, `N 1` = linear), which is the
  standard way to express a multi-stop CSS/SVG ramp in PDF. Two stops collapse
  to a single Type 2.
- Painting follows the existing precedent: the shape is already clipped (`W n`),
  then `sh` paints the shading through it.

**Alpha is the sharp edge.** PDF `/Shading` has no alpha channel — transparency
requires a soft mask (an `/SMask` in an ExtGState whose group luminosity encodes
the alpha ramp). Scope decision: **if every stop is opaque, emit a native vector
shading; if any stop carries alpha, fall back to today's rasterization** (which
already handles alpha correctly via the image `/SMask` path) and log why. That
keeps this PR focused, keeps the common case vector, and never regresses the
transparent case. The soft-mask path belongs with the groups/clip/mask slice,
which builds exactly that machinery.

**`spreadMethod`.** PDF `/Extend` models only `pad`. `reflect`/`repeat` have no
native equivalent, so they also fall back to rasterization with a log. This is
the same "vector when we can, honest raster when we cannot" rule as alpha.

## Non-goals

- Soft masks / transparency groups (the groups/clip/mask slice).
- PDF-input shadings round-tripping to PDF output as dictionaries.
- Mesh shadings (types 4-7).
- Changing `Shader` itself, or any existing `ColorAt` behavior.

## Testing

- The description round-trips: build an axial and a radial shader, describe it,
  and assert the coords/stops/spread match what went in.
- `alphaShader` delegates and scales stop alpha (the wrapper trap).
- A PDF containing an opaque SVG gradient has **no image XObject** and DOES have
  a `/ShadingType 2` (or 3) dictionary — the inverse of the assertion PR #104
  added.
- A PDF containing a gradient with `stop-opacity` still rasterizes (image
  XObject present, no shading dict) and logs the reason.
- `reflect`/`repeat` likewise fall back.
- **Visual equivalence**: SVG → PDF → raster must match SVG → raster within the
  standard golden tolerance for the vector path. This is the real proof that the
  emitted dictionary is correct rather than merely well-formed.
- No pre-existing golden may move. The raster backend is untouched, so the SVG
  corpus must be byte-identical.
