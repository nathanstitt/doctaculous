package hevc

// Tile geometry (spec 6.5.1): column/row boundaries in CTBs, the
// raster-to-tile-scan CTB address mappings, and the per-CTB tile id.
type tileInfo struct {
	colBd, rowBd   []int // boundaries in CTBs, len numCols+1 / numRows+1
	rsToTs, tsToRs []int
	tileOf         []int // tile index per raster CTB address
	numTiles       int
}

// buildTileInfo derives the tile layout for the SPS/PPS pair. A PPS without
// tiles yields a single tile covering the picture, so callers need no
// special case.
func buildTileInfo(s *sps, p *pps) *tileInfo {
	cols, rows := 1, 1
	if p.tilesEnabled {
		cols, rows = p.numTileColumns, p.numTileRows
	}
	t := &tileInfo{
		colBd:  make([]int, cols+1),
		rowBd:  make([]int, rows+1),
		rsToTs: make([]int, s.picSizeCtbs),
		tsToRs: make([]int, s.picSizeCtbs),
		tileOf: make([]int, s.picSizeCtbs),
	}
	t.numTiles = cols * rows
	if p.tilesEnabled && !p.uniformSpacing {
		for i := range cols - 1 {
			t.colBd[i+1] = t.colBd[i] + int(p.tileColumnWidths[i])
		}
		for j := range rows - 1 {
			t.rowBd[j+1] = t.rowBd[j] + int(p.tileRowHeights[j])
		}
	} else {
		for i := 1; i < cols; i++ {
			t.colBd[i] = i * s.picWidthCtbs / cols
		}
		for j := 1; j < rows; j++ {
			t.rowBd[j] = j * s.picHeightCtbs / rows
		}
	}
	t.colBd[cols] = s.picWidthCtbs
	t.rowBd[rows] = s.picHeightCtbs

	ts := 0
	for tj := range rows {
		for ti := range cols {
			tile := tj*cols + ti
			for y := t.rowBd[tj]; y < t.rowBd[tj+1]; y++ {
				for x := t.colBd[ti]; x < t.colBd[ti+1]; x++ {
					rs := y*s.picWidthCtbs + x
					t.rsToTs[rs] = ts
					t.tsToRs[ts] = rs
					t.tileOf[rs] = tile
					ts++
				}
			}
		}
	}
	return t
}

// tileID returns the tile index of a raster CTB address.
func (d *sliceDecoder) tileID(ctbRs int) int { return d.tiles.tileOf[ctbRs] }
