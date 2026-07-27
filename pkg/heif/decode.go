package heif

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"

	"github.com/nathanstitt/doctaculous/pkg/heif/hevc"
)

// Pixel decoding: coded items (single hvc1 or grid-of-tiles) to planar YCbCr
// at the item's full resolution, with grid tiles decoded concurrently.

// decodedPlanes is a composed YCbCr 4:2:0 picture (possibly stitched from
// grid tiles), before colour conversion and orientation.
type decodedPlanes struct {
	width, height    int
	bitDepth         int
	y, cb, cr        []uint16
	yStride, cStride int
	frame            *hevc.Frame // set when a single frame backs the planes
}

// decodeCodedItem decodes one hvc1 item via the HEVC decoder.
func (c *container) decodeCodedItem(it *item, workers int) (*hevc.Frame, error) {
	if it.hvcC == nil {
		return nil, fmt.Errorf("%w: item %d has no hvcC configuration", ErrInvalid, it.id)
	}
	cfg, err := parseHVCC(it.hvcC)
	if err != nil {
		return nil, err
	}
	payload, err := c.payload(it)
	if err != nil {
		return nil, err
	}
	nals, err := hevc.SplitNALs(payload, cfg.nalLengthSize)
	if err != nil {
		return nil, err
	}
	return hevc.DecodeFrameOpts(cfg.ps, nals, &hevc.DecodeOptions{Workers: workers})
}

// decodePlanes decodes the given item (single image or grid) into composed
// planes.
func (c *container) decodePlanes(ctx context.Context, it *item, workers int) (*decodedPlanes, error) {
	if it.itemType != "grid" {
		f, err := c.decodeCodedItem(it, workers)
		if err != nil {
			return nil, err
		}
		return &decodedPlanes{
			width: f.Width, height: f.Height, bitDepth: f.BitDepth,
			y: f.Y, cb: f.Cb, cr: f.Cr,
			yStride: f.YStride, cStride: f.CStride,
			frame: f,
		}, nil
	}

	g, tiles, err := c.validateGrid(it)
	if err != nil {
		return nil, err
	}
	w, h := int(g.width), int(g.height)
	if w > maxDim || h > maxDim || uint64(w)*uint64(h) > maxPixels {
		return nil, fmt.Errorf("%w: grid output %dx%d exceeds size limits", ErrUnsupported, w, h)
	}
	out := &decodedPlanes{
		width: w, height: h,
		yStride: w, cStride: (w + 1) / 2,
	}
	out.y = make([]uint16, w*h)
	ch := (h + 1) / 2
	out.cb = make([]uint16, out.cStride*ch)
	out.cr = make([]uint16, out.cStride*ch)

	// Tile geometry: uniform tile size from the first tile's decode; the
	// spec requires all tiles to share dimensions.
	var tileW, tileH int
	var mu sync.Mutex

	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	// Tiles are numerous and individually small: give each tile one worker
	// and parallelize across tiles instead.
	sem := make(chan struct{}, workers)
	errs := make([]error, len(tiles))
	var wg sync.WaitGroup
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for i, tile := range tiles {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, tile *item) {
			defer wg.Done()
			defer func() { <-sem }()
			if cctx.Err() != nil {
				errs[i] = cctx.Err()
				return
			}
			f, err := c.decodeCodedItem(tile, 1)
			if err != nil {
				errs[i] = err
				cancel()
				return
			}
			mu.Lock()
			if tileW == 0 {
				tileW, tileH = f.Width, f.Height
				out.bitDepth = f.BitDepth
			}
			tw, th, bd := tileW, tileH, out.bitDepth
			mu.Unlock()
			if f.Width != tw || f.Height != th || f.BitDepth != bd {
				errs[i] = fmt.Errorf("%w: grid tiles differ in size or depth", ErrInvalid)
				cancel()
				return
			}
			if i == 0 {
				// Colour signaling rides on the first tile.
				out.frame = f
			}
			copyTile(out, f, i%g.cols*tw, i/g.cols*th)
		}(i, tile)
	}
	wg.Wait()
	// Prefer a real failure over the cancellations it caused in siblings.
	var firstErr error
	for _, e := range errs {
		if e != nil && !errors.Is(e, context.Canceled) {
			firstErr = e
			break
		}
		if e != nil && firstErr == nil {
			firstErr = e
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	if tileW == 0 || tileW*g.cols < w || tileH*g.rows < h {
		return nil, fmt.Errorf("%w: grid tiles do not cover the output", ErrInvalid)
	}
	return out, nil
}

// copyTile writes one decoded tile into the composed planes at (x0, y0),
// cropping tile regions that extend past the grid output.
func copyTile(dst *decodedPlanes, f *hevc.Frame, x0, y0 int) {
	w := min(f.Width, dst.width-x0)
	h := min(f.Height, dst.height-y0)
	if w <= 0 || h <= 0 {
		return
	}
	for y := range h {
		copy(dst.y[(y0+y)*dst.yStride+x0:(y0+y)*dst.yStride+x0+w], f.Y[y*f.YStride:y*f.YStride+w])
	}
	cw := (w + 1) / 2
	chh := (h + 1) / 2
	cx, cy := x0/2, y0/2
	for y := range chh {
		copy(dst.cb[(cy+y)*dst.cStride+cx:(cy+y)*dst.cStride+cx+cw], f.Cb[y*f.CStride:y*f.CStride+cw])
		copy(dst.cr[(cy+y)*dst.cStride+cx:(cy+y)*dst.cStride+cx+cw], f.Cr[y*f.CStride:y*f.CStride+cw])
	}
}

// decodeAlpha decodes the alpha auxiliary item (if any) for the primary and
// returns its luma plane at full resolution, or nil.
func (c *container) decodeAlpha(ctx context.Context, primaryID uint32, w, h, workers int) ([]uint16, int, error) {
	aux := c.alphaAuxFor(primaryID)
	if aux == nil {
		return nil, 0, nil
	}
	if err := checkDecodable(aux); err != nil {
		return nil, 0, err
	}
	planes, err := c.decodePlanes(ctx, aux, workers)
	if err != nil {
		return nil, 0, err
	}
	if planes.width < w || planes.height < h {
		return nil, 0, fmt.Errorf("%w: alpha plane %dx%d smaller than image %dx%d",
			ErrInvalid, planes.width, planes.height, w, h)
	}
	// Repack to a tight w-stride plane.
	out := make([]uint16, w*h)
	for y := range h {
		copy(out[y*w:(y+1)*w], planes.y[y*planes.yStride:y*planes.yStride+w])
	}
	return out, planes.bitDepth, nil
}
