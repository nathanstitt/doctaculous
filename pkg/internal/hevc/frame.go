package hevc

// Frame is a decoded picture: planar YCbCr 4:2:0, one uint16 per sample for
// both 8- and 10-bit content (one code path; the container layer narrows to
// 8-bit output when appropriate). Dimensions are the cropped (conformance
// window) output size; the planes are allocated at coded size and cropping
// is a view adjustment done in Crop.
type Frame struct {
	// Width and Height are the output dimensions in luma samples.
	Width, Height int
	// BitDepth is the luma/chroma sample bit depth (8 or 10).
	BitDepth int
	// Y, Cb, Cr are the sample planes; chroma is half resolution in both
	// axes. Rows are YStride (luma) or CStride (chroma) apart, and the
	// first sample of each plane is the top-left of the cropped image.
	Y, Cb, Cr []uint16
	// YStride and CStride are the row strides of the planes.
	YStride, CStride int

	// Colour signaling from the SPS VUI (CICP code points; 2 =
	// unspecified) for the container layer's colour conversion.
	FullRange               bool
	ColourPrimaries         uint8
	TransferCharacteristics uint8
	MatrixCoefficients      uint8
	HasColourDescription    bool
}

// newFrame allocates a frame at the coded picture size.
func newFrame(s *sps) *Frame {
	w, h := s.width, s.height
	f := &Frame{
		Width:    w,
		Height:   h,
		BitDepth: s.bitDepthLuma,
		YStride:  w,
		CStride:  w / 2,
		Y:        make([]uint16, w*h),
		Cb:       make([]uint16, w/2*(h/2)),
		Cr:       make([]uint16, w/2*(h/2)),

		FullRange:               s.fullRange,
		ColourPrimaries:         s.colourPrimaries,
		TransferCharacteristics: s.transferChar,
		MatrixCoefficients:      s.matrixCoeffs,
		HasColourDescription:    s.vuiColourDesc,
	}
	return f
}

// applyConformanceWindow narrows the frame view to the cropped output
// region (offsets are in chroma units; luma offsets are double).
func (f *Frame) applyConformanceWindow(s *sps) {
	left, top := int(s.confWin[0]), int(s.confWin[2])
	f.Y = f.Y[top*2*f.YStride+left*2:]
	f.Cb = f.Cb[top*f.CStride+left:]
	f.Cr = f.Cr[top*f.CStride+left:]
	f.Width = s.croppedWidth()
	f.Height = s.croppedHeight()
}
