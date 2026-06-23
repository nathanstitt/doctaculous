package raster

import (
	"fmt"
	"image/color"
	"math"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/pdf/function"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// Mesh shadings (ISO 32000-1 §8.7.4.5.5–.8, ShadingType 4–7) describe a surface
// as a set of colored triangles or patches whose data is packed into a stream of
// bit fields rather than a continuous function. We decode the stream, tessellate
// to triangles (patches subdivide to a fixed grid), and evaluate color by finding
// the triangle containing a query point and barycentrically interpolating its
// vertex colors. This reuses the render.Shader seam (ColorAt) so mesh shadings
// paint through the same FillShading path as axial/radial shadings — honoring the
// clip and blend mode — at the cost of an O(triangles) point query, which is fine
// for the small meshes seen in practice.

// meshVertex is a tessellated vertex: a position in shading space and its RGBA.
type meshVertex struct {
	x, y float64
	c    color.RGBA
}

// meshTriangle is a Gouraud triangle: three colored vertices.
type meshTriangle struct{ v [3]meshVertex }

// meshShading is a render.Shader backed by a triangle list. ColorAt locates the
// containing triangle and interpolates; points outside every triangle are not
// painted (unless /Background applies, handled by the caller via the shading
// dict — meshes here simply report !ok outside).
type meshShading struct {
	tris []meshTriangle
}

// newMeshShader decodes a mesh shading stream (Types 4–7) into a triangle list.
// Types 4 (free-form Gouraud) and 5 (lattice-form) are decoded directly; Types 6
// (Coons) and 7 (tensor) tessellate each patch into a fixed grid of triangles by
// evaluating the patch's boundary as bilinear-cornered Coons surfaces (an
// approximation noted in the roadmap). Returns an error on malformed data so the
// caller degrades gracefully.
func newMeshShader(doc *pdf.Document, dict pdf.Dict, stream *pdf.Stream) (render.Shader, error) {
	st, _ := doc.GetInt(dict["ShadingType"])

	data, _, err := doc.DecodedStream(stream)
	if err != nil {
		return nil, fmt.Errorf("shading: mesh stream decode: %w", err)
	}

	cs, err := resolveImageCS(doc, dict["ColorSpace"], 8, nil)
	if err != nil {
		return nil, fmt.Errorf("shading: mesh color space: %w", err)
	}

	// An optional /Function reduces per-vertex color to a single parametric value
	// (one component) that the function maps to the real color components.
	var fn function.Func
	if dict["Function"] != nil {
		if f, ferr := function.Parse(doc, dict["Function"]); ferr == nil {
			fn = f
		} else {
			return nil, fmt.Errorf("shading: mesh /Function: %w", ferr)
		}
	}

	m := &meshDecoder{
		doc:    doc,
		dict:   dict,
		data:   data,
		csKind: cs.kind,
		fn:     fn,
	}
	if err := m.readCommon(); err != nil {
		return nil, err
	}

	var tris []meshTriangle
	switch st {
	case 4:
		tris, err = m.decodeType4()
	case 5:
		tris, err = m.decodeType5()
	case 6, 7:
		tris, err = m.decodePatches(st)
	default:
		return nil, fmt.Errorf("shading: unsupported mesh /ShadingType %d", st)
	}
	if err != nil {
		return nil, err
	}
	if len(tris) == 0 {
		return nil, fmt.Errorf("shading: mesh decoded no triangles")
	}
	return &meshShading{tris: tris}, nil
}

// ColorAt finds the triangle containing (x,y) and interpolates its vertex colors.
// Outside every triangle it reports !ok so the backdrop shows through.
func (m *meshShading) ColorAt(x, y float64) (color.RGBA, bool) {
	for i := range m.tris {
		if c, ok := barycentricColor(&m.tris[i], x, y); ok {
			return c, true
		}
	}
	return color.RGBA{}, false
}

// barycentricColor returns the interpolated color at (x,y) if the point lies
// within triangle t (with a small epsilon so shared edges paint), else ok=false.
func barycentricColor(t *meshTriangle, x, y float64) (color.RGBA, bool) {
	ax, ay := t.v[0].x, t.v[0].y
	bx, by := t.v[1].x, t.v[1].y
	cx, cy := t.v[2].x, t.v[2].y
	d := (by-cy)*(ax-cx) + (cx-bx)*(ay-cy)
	if math.Abs(d) < 1e-12 {
		return color.RGBA{}, false // degenerate triangle
	}
	l0 := ((by-cy)*(x-cx) + (cx-bx)*(y-cy)) / d
	l1 := ((cy-ay)*(x-cx) + (ax-cx)*(y-cy)) / d
	l2 := 1 - l0 - l1
	const eps = -1e-6
	if l0 < eps || l1 < eps || l2 < eps {
		return color.RGBA{}, false
	}
	return color.RGBA{
		R: lerp8(l0, l1, l2, t.v[0].c.R, t.v[1].c.R, t.v[2].c.R),
		G: lerp8(l0, l1, l2, t.v[0].c.G, t.v[1].c.G, t.v[2].c.G),
		B: lerp8(l0, l1, l2, t.v[0].c.B, t.v[1].c.B, t.v[2].c.B),
		A: 255,
	}, true
}

// lerp8 barycentrically blends three 8-bit channel values.
func lerp8(l0, l1, l2 float64, a, b, c uint8) uint8 {
	v := l0*float64(a) + l1*float64(b) + l2*float64(c)
	switch {
	case v <= 0:
		return 0
	case v >= 255:
		return 255
	default:
		return uint8(v + 0.5)
	}
}

// --- decoder ---------------------------------------------------------------

// meshDecoder holds the parameters and a bit cursor for unpacking a mesh stream.
type meshDecoder struct {
	doc    *pdf.Document
	dict   pdf.Dict
	data   []byte
	csKind csKind
	fn     function.Func

	bitsCoord int
	bitsComp  int
	bitsFlag  int
	nComps    int       // color components per vertex (1 if /Function, else cs comps)
	decode    []float64 // /Decode: [xmin xmax ymin ymax c0min c0max ...]

	// bit cursor
	bytePos int
	bitPos  int // 0..7, MSB-first
}

// readCommon reads the bit-width and /Decode parameters shared by all mesh types.
func (m *meshDecoder) readCommon() error {
	m.bitsCoord, _ = m.doc.GetInt(m.dict["BitsPerCoordinate"])
	m.bitsComp, _ = m.doc.GetInt(m.dict["BitsPerComponent"])
	m.bitsFlag, _ = m.doc.GetInt(m.dict["BitsPerFlag"])
	if m.bitsCoord <= 0 || m.bitsCoord > 32 || m.bitsComp <= 0 || m.bitsComp > 16 {
		return fmt.Errorf("shading: mesh bad BitsPerCoordinate/Component %d/%d", m.bitsCoord, m.bitsComp)
	}
	// Color components per vertex: 1 when a /Function maps a parametric value,
	// otherwise one per color-space channel.
	m.nComps = csComps(m.csKind)
	if m.fn != nil {
		m.nComps = 1
	}
	dec := m.doc.GetArray(m.dict["Decode"])
	if len(dec) < 4+2*m.nComps {
		return fmt.Errorf("shading: mesh /Decode too short (%d, need %d)", len(dec), 4+2*m.nComps)
	}
	m.decode = make([]float64, len(dec))
	for i, e := range dec {
		m.decode[i], _ = pdf.Number(m.doc.Resolve(e))
	}
	return nil
}

// csComps reports the channel count for a (non-indexed) color-space kind.
func csComps(k csKind) int {
	switch k {
	case csGray:
		return 1
	case csCMYK:
		return 4
	default:
		return 3
	}
}

// readBits reads n bits MSB-first, returning them right-aligned. Reading past the
// end yields zero bits (and sets exhausted via atEnd).
func (m *meshDecoder) readBits(n int) uint32 {
	var v uint32
	for i := 0; i < n; i++ {
		v <<= 1
		if m.bytePos < len(m.data) {
			bit := (m.data[m.bytePos] >> uint(7-m.bitPos)) & 1
			v |= uint32(bit)
		}
		m.bitPos++
		if m.bitPos == 8 {
			m.bitPos = 0
			m.bytePos++
		}
	}
	return v
}

// align advances the cursor to the next byte boundary (used between per-vertex
// records and per-row records where the spec pads to a byte).
func (m *meshDecoder) align() {
	if m.bitPos != 0 {
		m.bitPos = 0
		m.bytePos++
	}
}

// atEnd reports whether the cursor has consumed all packed data.
func (m *meshDecoder) atEnd() bool { return m.bytePos >= len(m.data) }

// readCoord reads one packed coordinate and maps it through /Decode index pair
// (di, di+1) into shading space.
func (m *meshDecoder) readCoord(di int) float64 {
	raw := m.readBits(m.bitsCoord)
	maxv := float64((uint64(1) << uint(m.bitsCoord)) - 1)
	t := float64(raw) / maxv
	lo, hi := m.decode[di], m.decode[di+1]
	return lo + t*(hi-lo)
}

// readColor reads the packed per-vertex color components, applies /Decode, runs
// the optional /Function, and converts to RGBA.
func (m *meshDecoder) readColor() color.RGBA {
	comps := make([]float64, m.nComps)
	maxv := float64((uint64(1) << uint(m.bitsComp)) - 1)
	for i := 0; i < m.nComps; i++ {
		raw := m.readBits(m.bitsComp)
		t := float64(raw) / maxv
		lo, hi := m.decode[4+2*i], m.decode[4+2*i+1]
		comps[i] = lo + t*(hi-lo)
	}
	if m.fn != nil {
		comps = m.fn.Eval(comps)
	}
	c := componentsToRGBA(m.csKind, comps)
	c.A = 0xFF
	return c
}

// readVertex reads (x, y, color) for a flagged/unflagged vertex.
func (m *meshDecoder) readVertex() meshVertex {
	x := m.readCoord(0)
	y := m.readCoord(2)
	return meshVertex{x: x, y: y, c: m.readColor()}
}

// decodeType4 reads free-form Gouraud-shaded triangles. Each vertex carries an
// edge flag. A flag-0 vertex starts a new triangle: three consecutive flag-0
// vertices (va, vb, vc) form it. Once a triangle exists, a flag-1 vertex forms
// (vb, vc, vnew) and a flag-2 vertex forms (va, vc, vnew), sharing an edge with
// the previous triangle (a strip / fan). A flag-0 vertex after a complete
// triangle begins a brand-new one (three more flag-0 vertices).
func (m *meshDecoder) decodeType4() ([]meshTriangle, error) {
	var tris []meshTriangle
	var va, vb, vc meshVertex
	pending := 0 // flag-0 vertices accumulated toward the next fresh triangle

	for !m.atEnd() {
		flag := m.readBits(m.bitsFlag)
		v := m.readVertex()
		m.align() // each vertex record is padded to a byte boundary

		if flag == 0 {
			switch pending {
			case 0:
				va, pending = v, 1
			case 1:
				vb, pending = v, 2
			default:
				vc = v
				tris = append(tris, meshTriangle{v: [3]meshVertex{va, vb, vc}})
				pending = 0 // triangle complete; next flag-0 starts a fresh one
			}
			continue
		}
		// flag 1 or 2 extends the most recent triangle by one shared edge.
		if len(tris) == 0 {
			continue // no triangle to share with yet; skip defensively
		}
		if flag == 1 {
			va, vb, vc = vb, vc, v
		} else { // flag == 2
			vb, vc = vc, v
		}
		tris = append(tris, meshTriangle{v: [3]meshVertex{va, vb, vc}})
		pending = 0
	}
	return tris, nil
}

// decodeType5 reads lattice-form Gouraud triangles: vertices in row-major order,
// /VerticesPerRow per row, no flags. Adjacent rows form quads split into two
// triangles each.
func (m *meshDecoder) decodeType5() ([]meshTriangle, error) {
	vpr, _ := m.doc.GetInt(m.dict["VerticesPerRow"])
	if vpr < 2 {
		return nil, fmt.Errorf("shading: type 5 mesh bad /VerticesPerRow %d", vpr)
	}
	var rows [][]meshVertex
	for !m.atEnd() {
		row := make([]meshVertex, 0, vpr)
		for i := 0; i < vpr && !m.atEnd(); i++ {
			row = append(row, m.readVertex())
		}
		m.align()
		if len(row) == vpr {
			rows = append(rows, row)
		}
	}
	var tris []meshTriangle
	for r := 0; r+1 < len(rows); r++ {
		for c := 0; c+1 < vpr; c++ {
			a := rows[r][c]
			b := rows[r][c+1]
			cc := rows[r+1][c]
			d := rows[r+1][c+1]
			tris = append(tris,
				meshTriangle{v: [3]meshVertex{a, b, cc}},
				meshTriangle{v: [3]meshVertex{b, d, cc}})
		}
	}
	return tris, nil
}

// decodePatches reads Coons (Type 6, 12 control points) or tensor (Type 7, 16)
// patches and tessellates each into a grid of triangles. We approximate the patch
// surface by bilinearly interpolating its four corner points and corner colors
// over a fixed grid — exact for flat patches and a reasonable approximation for
// curved ones (documented in the roadmap). Flag 0 patches carry 4 corner colors;
// flags 1–3 share an edge with the previous patch (we still read a full patch's
// new control points/colors per the spec's reduced records).
func (m *meshDecoder) decodePatches(shadingType int) ([]meshTriangle, error) {
	nCtrl := 12
	if shadingType == 7 {
		nCtrl = 16
	}
	var tris []meshTriangle
	// Previous patch corners/colors, for shared-edge (flag != 0) patches.
	var prevCorners [4]meshVertex
	havePrev := false

	for !m.atEnd() {
		flag := m.readBits(m.bitsFlag)

		// Number of new control points and new colors depends on the edge flag.
		newPts := nCtrl
		newColors := 4
		if flag != 0 {
			newPts = nCtrl - 4 // one edge (4 points) shared
			newColors = 2      // two corner colors shared
		}

		pts := make([][2]float64, 0, nCtrl)
		for i := 0; i < newPts; i++ {
			px := m.readCoord(0)
			py := m.readCoord(2)
			pts = append(pts, [2]float64{px, py})
		}
		colors := make([]color.RGBA, 0, 4)
		for i := 0; i < newColors; i++ {
			colors = append(colors, m.readColor())
		}
		m.align()

		corners, ok := m.patchCorners(flag, pts, colors, prevCorners, havePrev)
		if !ok {
			// Malformed shared-edge patch with no predecessor: stop gracefully.
			break
		}
		tris = append(tris, tessellatePatch(corners)...)
		prevCorners = corners
		havePrev = true
	}
	return tris, nil
}

// patchCorners reduces a (possibly shared-edge) patch record to its four corner
// vertices with colors. For flag 0 the four corners are control points 0, 3, 6, 9
// (Coons point ordering) with the four read colors. For shared-edge flags we reuse
// two corners from the previous patch; the approximation takes the previous
// patch's shared edge plus the two newly read colors. Returns ok=false if a
// shared-edge patch appears first (no predecessor).
func (m *meshDecoder) patchCorners(flag uint32, pts [][2]float64, colors []color.RGBA, prev [4]meshVertex, havePrev bool) ([4]meshVertex, bool) {
	var corners [4]meshVertex
	if flag == 0 {
		if len(pts) < 12 || len(colors) < 4 {
			return corners, false
		}
		// Coons control-point ordering: corners are p0, p3, p6, p9 (the four patch
		// corners around the boundary). We label them in boundary order.
		idx := [4]int{0, 3, 6, 9}
		for i := 0; i < 4; i++ {
			p := pts[idx[i]]
			corners[i] = meshVertex{x: p[0], y: p[1], c: colors[i]}
		}
		return corners, true
	}
	if !havePrev {
		return corners, false
	}
	// Shared-edge approximation: reuse the previous patch's two corners on the
	// shared edge, then place the two new corners from the newly read control
	// points (the new boundary's far corners) with the two new colors. We take the
	// last two read points as the new far corners.
	if len(pts) < 2 || len(colors) < 2 {
		return corners, false
	}
	// Shared edge: previous corners 1 and 2 (a heuristic ordering).
	corners[0] = prev[1]
	corners[1] = prev[2]
	far1 := pts[len(pts)-2]
	far2 := pts[len(pts)-1]
	corners[2] = meshVertex{x: far1[0], y: far1[1], c: colors[0]}
	corners[3] = meshVertex{x: far2[0], y: far2[1], c: colors[1]}
	return corners, true
}

// tessellatePatch splits a quad (four corners in boundary order) into a grid of
// Gouraud triangles, bilinearly interpolating position and color across the grid.
// patchGrid controls the subdivision density.
const patchGrid = 10

func tessellatePatch(c [4]meshVertex) []meshTriangle {
	// Corners in boundary order c0→c1→c2→c3. Bilinear parameterization: map (u,v)
	// in [0,1]^2 with c0 at (0,0), c1 at (1,0), c2 at (1,1), c3 at (0,1).
	at := func(u, v float64) meshVertex {
		// Bilinear position.
		top := [2]float64{
			c[0].x*(1-u) + c[1].x*u,
			c[0].y*(1-u) + c[1].y*u,
		}
		bot := [2]float64{
			c[3].x*(1-u) + c[2].x*u,
			c[3].y*(1-u) + c[2].y*u,
		}
		x := top[0]*(1-v) + bot[0]*v
		y := top[1]*(1-v) + bot[1]*v
		// Bilinear color.
		col := bilinColor(c, u, v)
		return meshVertex{x: x, y: y, c: col}
	}
	var tris []meshTriangle
	step := 1.0 / patchGrid
	for i := 0; i < patchGrid; i++ {
		for j := 0; j < patchGrid; j++ {
			u0, v0 := float64(i)*step, float64(j)*step
			u1, v1 := u0+step, v0+step
			a := at(u0, v0)
			b := at(u1, v0)
			cc := at(u1, v1)
			d := at(u0, v1)
			tris = append(tris,
				meshTriangle{v: [3]meshVertex{a, b, cc}},
				meshTriangle{v: [3]meshVertex{a, cc, d}})
		}
	}
	return tris
}

// bilinColor bilinearly blends the four corner colors at (u,v).
func bilinColor(c [4]meshVertex, u, v float64) color.RGBA {
	ch := func(get func(meshVertex) uint8) uint8 {
		top := float64(get(c[0]))*(1-u) + float64(get(c[1]))*u
		bot := float64(get(c[3]))*(1-u) + float64(get(c[2]))*u
		val := top*(1-v) + bot*v
		switch {
		case val <= 0:
			return 0
		case val >= 255:
			return 255
		default:
			return uint8(val + 0.5)
		}
	}
	return color.RGBA{
		R: ch(func(m meshVertex) uint8 { return m.c.R }),
		G: ch(func(m meshVertex) uint8 { return m.c.G }),
		B: ch(func(m meshVertex) uint8 { return m.c.B }),
		A: 255,
	}
}
