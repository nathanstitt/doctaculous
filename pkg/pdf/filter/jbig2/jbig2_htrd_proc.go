// Copyright 2026 肖其顿 (XIAO QI DUN)
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package jbig2

import (
	"errors"
)

// HTRDProc 半色调区域解码过程
type HTRDProc struct {
	HBW, HBH    uint32
	HMMR        bool
	HTEMPLATE   uint8
	HNUMPATS    uint32
	HPATS       []*Image
	HDEFPIXEL   bool
	HCOMBOP     ComposeOp
	HENABLESKIP bool
	HGW, HGH    uint32
	HGX, HGY    int32
	HRX, HRY    uint16
	HPW, HPH    uint8
}

// NewHTRDProc 创建半色调区域解码过程对象
// 返回: *HTRDProc 对象
func NewHTRDProc() *HTRDProc {
	return &HTRDProc{}
}

// DecodeArith 算术解码
// 入参: arithDecoder 算术解码器, gbContexts 上下文
// 返回: *Image 图像, error 错误信息
func (h *HTRDProc) DecodeArith(arithDecoder *ArithDecoder, gbContexts []ArithCtx) (*Image, error) {
	var hSkip *Image
	if h.HENABLESKIP {
		hSkip = NewImage(int32(h.HGW), int32(h.HGH))
		if hSkip == nil {
			return nil, errors.New("failed to create skip image")
		}
		for mg := uint32(0); mg < h.HGH; mg++ {
			for ng := uint32(0); ng < h.HGW; ng++ {
				mgInt := int64(mg)
				ngInt := int64(ng)
				x := (int64(h.HGX) + mgInt*int64(h.HRY) + ngInt*int64(h.HRX)) >> 8
				y := (int64(h.HGY) + mgInt*int64(h.HRX) - ngInt*int64(h.HRY)) >> 8
				if (x+int64(h.HPW) <= 0) || (x >= int64(h.HBW)) || (y+int64(h.HPH) <= 0) || (y >= int64(h.HBH)) {
					hSkip.SetPixel(int32(ng), int32(mg), 1)
				} else {
					hSkip.SetPixel(int32(ng), int32(mg), 0)
				}
			}
		}
	}
	hbpp := uint32(1)
	for (uint32(1) << hbpp) < h.HNUMPATS {
		hbpp++
	}
	grd := NewGRDProc()
	grd.MMR = h.HMMR
	grd.GBW = h.HGW
	grd.GBH = h.HGH
	grd.GBTEMPLATE = h.HTEMPLATE
	grd.TPGDON = false
	grd.USESKIP = h.HENABLESKIP
	grd.SKIP = hSkip
	if h.HTEMPLATE <= 1 {
		grd.GBAT[0] = 3
	} else {
		grd.GBAT[0] = 2
	}
	grd.GBAT[1] = -1
	if grd.GBTEMPLATE == 0 {
		grd.GBAT[2] = -3
		grd.GBAT[3] = -1
		grd.GBAT[4] = 2
		grd.GBAT[5] = -2
		grd.GBAT[6] = -2
		grd.GBAT[7] = -2
	}
	gsbpp := int(hbpp)
	gsplanes := make([]*Image, gsbpp)
	for i := gsbpp - 1; i >= 0; i-- {
		var pImage *Image
		state := &ProgressiveArithDecodeState{
			Image:        &pImage,
			ArithDecoder: arithDecoder,
			GbContexts:   gbContexts,
		}
		status := grd.StartDecodeArith(state)
		if status == JBig2SegmentError {
			return nil, errors.New("arith decoding failure")
		}
		if pImage == nil {
			return nil, errors.New("failed to decode plane")
		}
		gsplanes[i] = pImage
		if i < gsbpp-1 {
			gsplanes[i].ComposeFrom(0, 0, gsplanes[i+1], ComposeXor)
		}
	}
	return h.decodeImage(gsplanes)
}

// DecodeMMR MMR解码
// 入参: stream 位流
// 返回: *Image 半色调区域图像, error 错误信息
func (h *HTRDProc) DecodeMMR(stream *BitStream) (*Image, error) {
	hbpp := uint32(1)
	for (uint32(1) << hbpp) < h.HNUMPATS {
		hbpp++
	}
	gsbpp := int(hbpp)
	gsplanes := make([]*Image, gsbpp)
	j := gsbpp - 1
	decoder := NewMMRDecompressor(int(h.HGW), int(h.HGH), stream)
	pImage, err := decoder.Uncompress()
	if err != nil {
		return nil, err
	}
	gsplanes[j] = pImage
	for j > 0 {
		j--
		decoder = NewMMRDecompressor(int(h.HGW), int(h.HGH), stream)
		pImg, err := decoder.Uncompress()
		if err != nil {
			return nil, err
		}
		gsplanes[j] = pImg
		gsplanes[j].ComposeFrom(0, 0, gsplanes[j+1], ComposeXor)
	}
	return h.decodeImage(gsplanes)
}

// decodeImage 解码图像
// 入参: gsplanes 图像平面集合
// 返回: *Image 图像, error 错误信息
func (h *HTRDProc) decodeImage(gsplanes []*Image) (*Image, error) {
	htReg := NewImage(int32(h.HBW), int32(h.HBH))
	if htReg == nil {
		return nil, errors.New("failed to create target image")
	}
	htReg.Fill(h.HDEFPIXEL)
	for mg := uint32(0); mg < h.HGH; mg++ {
		for ng := uint32(0); ng < h.HGW; ng++ {
			gsval := uint32(0)
			for i := 0; i < len(gsplanes); i++ {
				bit := gsplanes[i].GetPixel(int32(ng), int32(mg))
				gsval |= uint32(bit) << i
			}
			patIndex := gsval
			if patIndex >= h.HNUMPATS {
				patIndex = h.HNUMPATS - 1
			}
			mgInt := int64(mg)
			ngInt := int64(ng)
			x := (int64(h.HGX) + mgInt*int64(h.HRY) + ngInt*int64(h.HRX)) >> 8
			y := (int64(h.HGY) + mgInt*int64(h.HRX) - ngInt*int64(h.HRY)) >> 8
			pat := h.HPATS[patIndex]
			if pat != nil {
				pat.ComposeTo(htReg, int32(x), int32(y), h.HCOMBOP)
			}
		}
	}
	return htReg, nil
}
