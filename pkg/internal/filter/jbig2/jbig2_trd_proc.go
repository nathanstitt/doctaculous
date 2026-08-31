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

// ComposeData 混合数据
type ComposeData struct {
	x, y      int32
	increment int32
}

// JBig2Corner 角落位置枚举
type JBig2Corner int

const (
	JBig2CornerBottomLeft  JBig2Corner = 0
	JBig2CornerTopLeft     JBig2Corner = 1
	JBig2CornerBottomRight JBig2Corner = 2
	JBig2CornerTopRight    JBig2Corner = 3
)

// TRDProc 文本区域解码过程
type TRDProc struct {
	SBHUFF         bool
	SBREFINE       bool
	SBRTEMPLATE    bool
	TRANSPOSED     bool
	SBDEFPIXEL     bool
	SBDSOFFSET     int8
	SBSYMCODELEN   uint8
	SBW            uint32
	SBH            uint32
	SBNUMINSTANCES uint32
	SBSTRIPS       uint32
	SBNUMSYMS      uint32
	SBSYMCODES     []HuffmanCode
	SBSYMS         []*Image
	SBCOMBOP       ComposeOp
	REFCORNER      JBig2Corner
	SBHUFFFS       *HuffmanTable
	SBHUFFDS       *HuffmanTable
	SBHUFFDT       *HuffmanTable
	SBHUFFRDW      *HuffmanTable
	SBHUFFRDH      *HuffmanTable
	SBHUFFRDX      *HuffmanTable
	SBHUFFRDY      *HuffmanTable
	SBHUFFRSIZE    *HuffmanTable
	SBRAT          [4]int8
}

// IntDecoderState 整数解码器状态
type IntDecoderState struct {
	IADT, IAFS, IADS, IAIT, IARI *ArithIntDecoder
	IARDW, IARDH, IARDX, IARDY   *ArithIntDecoder
	IAID                         *ArithIaidDecoder
}

// NewTRDProc 创建文本区域解码过程对象
// 返回: *TRDProc 对象
func NewTRDProc() *TRDProc {
	return &TRDProc{
		SBSTRIPS: 1,
	}
}

// GetComposeData 获取混合位置数据
// 入参: SI, TI 相对坐标, WI, HI 宽高
// 返回: ComposeData 混合位置信息
func (t *TRDProc) GetComposeData(SI, TI int32, WI, HI uint32) ComposeData {
	var results ComposeData
	s := SI
	t_val := TI
	if !t.TRANSPOSED {
		results.x = s
		results.y = t_val
		switch t.REFCORNER {
		case JBig2CornerBottomLeft:
			results.y = t_val - int32(HI) + 1
		case JBig2CornerBottomRight:
			results.x = s - int32(WI) + 1
			results.y = t_val - int32(HI) + 1
		case JBig2CornerTopLeft:
			results.x = s
			results.y = t_val
		case JBig2CornerTopRight:
			results.x = s - int32(WI) + 1
		}
		results.increment = int32(WI) - 1
	} else {
		results.x = t_val
		results.y = s
		switch t.REFCORNER {
		case JBig2CornerBottomLeft:
			results.x = t_val - int32(HI) + 1
		case JBig2CornerBottomRight:
			results.x = t_val - int32(HI) + 1
			results.y = s - int32(WI) + 1
		case JBig2CornerTopLeft:
			results.x = t_val
			results.y = s
		case JBig2CornerTopRight:
			results.y = s - int32(WI) + 1
		}
		results.increment = int32(HI) - 1
	}
	return results
}

// checkTRDDimension 检查文本区域维度
// 入参: dimension 原始维度, delta 增量
// 返回: uint32 新维度, bool 是否有效
func checkTRDDimension(dimension uint32, delta int32) (uint32, bool) {
	res := int64(dimension) + int64(delta)
	if res < 0 || res > 0xFFFFFFFF {
		return 0, false
	}
	return uint32(res), true
}

// checkTRDReferenceDimension 检查参考维度
// 入参: dimension 维度, shift 位移, offset 偏移
// 返回: int32 新坐标, bool 是否有效
func checkTRDReferenceDimension(dimension int32, shift uint32, offset int32) (int32, bool) {
	res := int64(offset) + (int64(dimension) >> shift)
	if res < -2147483648 || res > 2147483647 {
		return 0, false
	}
	return int32(res), true
}

// DecodeHuffman 霍夫曼解码
// 入参: stream 位流, grContexts 细化上下文集
// 返回: *Image 图像对象, error 错误信息
func (t *TRDProc) DecodeHuffman(stream *BitStream, grContexts []ArithCtx) (*Image, error) {
	sbReg := NewImage(int32(t.SBW), int32(t.SBH))
	if sbReg == nil {
		return nil, errors.New("failed to create text region image")
	}
	sbReg.Fill(t.SBDEFPIXEL)
	decoder := NewHuffmanDecoder(stream)
	var initialStript int32
	if res := decoder.DecodeAValue(t.SBHUFFDT, &initialStript); res != 0 {
		return nil, errors.New("huffman decode failed for sbhuffdt")
	}
	STRIPT := -int64(initialStript) * int64(t.SBSTRIPS)
	FIRSTS := int64(0)
	NINSTANCES := uint32(0)
	for NINSTANCES < t.SBNUMINSTANCES {
		var initialDt int32
		if res := decoder.DecodeAValue(t.SBHUFFDT, &initialDt); res != 0 {
			return nil, errors.New("huffman decode failed for sbhuffdt in loop")
		}
		STRIPT += int64(initialDt) * int64(t.SBSTRIPS)
		bFirst := true
		CURS := int64(0)
		for {
			if bFirst {
				var dfs int32
				if res := decoder.DecodeAValue(t.SBHUFFFS, &dfs); res != 0 {
					return nil, errors.New("huffman decode failed for sbhufffs")
				}
				FIRSTS += int64(dfs)
				CURS = FIRSTS
				bFirst = false
			} else {
				var ids int32
				res := decoder.DecodeAValue(t.SBHUFFDS, &ids)
				if res == JBig2OOB {
					break
				}
				if res != 0 {
					return nil, errors.New("huffman decode failed for sbhuffds")
				}
				currDso := int32(t.SBDSOFFSET)
				if currDso >= 16 {
					currDso -= 32
				}
				CURS += int64(ids) + int64(currDso)
			}
			CURT := int32(0)
			if t.SBSTRIPS != 1 {
				nTmp := uint32(1)
				for uint32(1<<nTmp) < t.SBSTRIPS {
					nTmp++
				}
				var val uint32
				val, err := stream.ReadNBits(nTmp)
				if err != nil {
					return nil, errors.New("read nbits failed")
				}
				CURT = int32(val)
			}
			TI := int32(STRIPT + int64(CURT))
			nSafeVal := int32(0)
			nBits := 0
			IDI := uint32(0)
			for {
				var nTmp uint32
				val, err := stream.Read1Bit()
				if err != nil {
					return nil, errors.New("read 1 bit failed")
				}
				nTmp = val
				nSafeVal = (nSafeVal << 1) | int32(nTmp)
				nBits++
				for IDI = 0; IDI < t.SBNUMSYMS; IDI++ {
					if int32(nBits) == t.SBSYMCODES[IDI].Codelen && nSafeVal == int32(t.SBSYMCODES[IDI].Code) {
						break
					}
				}
				if IDI < t.SBNUMSYMS {
					break
				}
			}
			var RI uint32 = 0
			if t.SBREFINE {
				val, err := stream.Read1Bit()
				if err != nil {
					return nil, errors.New("read refine bit failed")
				}
				RI = val
			}
			var IBI *Image
			if RI == 0 {
				if IDI >= uint32(len(t.SBSYMS)) {
					return nil, errors.New("idi out of bounds")
				}
				IBI = t.SBSYMS[IDI]
			} else {
				var rdwi, rdhi, rdxi, rdyi, uffrsize int32
				if decoder.DecodeAValue(t.SBHUFFRDW, &rdwi) != 0 ||
					decoder.DecodeAValue(t.SBHUFFRDH, &rdhi) != 0 ||
					decoder.DecodeAValue(t.SBHUFFRDX, &rdxi) != 0 ||
					decoder.DecodeAValue(t.SBHUFFRDY, &rdyi) != 0 ||
					decoder.DecodeAValue(t.SBHUFFRSIZE, &uffrsize) != 0 {
					return nil, errors.New("huffman decode refine values failed")
				}
				stream.AlignByte()
				IBOI := t.SBSYMS[IDI]
				if IBOI == nil {
					return nil, errors.New("failed to get iboi")
				}
				WOI, okW := checkTRDDimension(uint32(IBOI.width), rdwi)
				HOI, okH := checkTRDDimension(uint32(IBOI.height), rdhi)
				if !okW || !okH {
					return nil, errors.New("dimension check failed")
				}
				refDX, okDX := checkTRDReferenceDimension(rdwi, 1, rdxi)
				refDY, okDY := checkTRDReferenceDimension(rdhi, 1, rdyi)
				if !okDX || !okDY {
					return nil, errors.New("ref check failed")
				}
				pGRRD := NewGRRDProc()
				pGRRD.GRW = WOI
				pGRRD.GRH = HOI
				pGRRD.GRTEMPLATE = t.SBRTEMPLATE
				pGRRD.GRREFERENCE = IBOI
				pGRRD.GRREFERENCEDX = refDX
				pGRRD.GRREFERENCEDY = refDY
				pGRRD.TPGRON = false
				pGRRD.GRAT = t.SBRAT
				pArithDecoder := NewArithDecoder(stream)
				var err error
				IBI, err = pGRRD.Decode(pArithDecoder, grContexts)
				if err != nil {
					return nil, err
				}
				stream.AlignByte()
				stream.AddOffset(2)
			}
			if IBI != nil {
				WI := uint32(IBI.width)
				HI := uint32(IBI.height)
				if !t.TRANSPOSED && (t.REFCORNER == JBig2CornerTopRight || t.REFCORNER == JBig2CornerBottomRight) {
					CURS += int64(WI) - 1
				} else if t.TRANSPOSED && (t.REFCORNER == JBig2CornerBottomLeft || t.REFCORNER == JBig2CornerBottomRight) {
					CURS += int64(HI) - 1
				}
				SI := int32(CURS)
				compose := t.GetComposeData(SI, TI, WI, HI)
				IBI.ComposeTo(sbReg, int32(compose.x), int32(compose.y), t.SBCOMBOP)
				CURS += int64(compose.increment)
				NINSTANCES++
			}
		}
	}
	return sbReg, nil
}

// DecodeArith 算术解码
// 入参: arithDecoder 算术解码器, grContexts 细化上下文集, ids 整数解码器状态
// 返回: *Image 图像对象, error 错误信息
func (t *TRDProc) DecodeArith(arithDecoder *ArithDecoder, grContexts []ArithCtx, ids *IntDecoderState) (*Image, error) {
	var pIADT, pIAFS, pIADS, pIAIT, pIARI, pIARDW, pIARDH, pIARDX, pIARDY *ArithIntDecoder
	var pIAID *ArithIaidDecoder
	if ids != nil {
		pIADT = ids.IADT
		pIAFS = ids.IAFS
		pIADS = ids.IADS
		pIAIT = ids.IAIT
		pIARI = ids.IARI
		pIARDW = ids.IARDW
		pIARDH = ids.IARDH
		pIARDX = ids.IARDX
		pIARDY = ids.IARDY
		pIAID = ids.IAID
	}
	if pIADT == nil {
		pIADT = NewArithIntDecoder()
	}
	if pIAFS == nil {
		pIAFS = NewArithIntDecoder()
	}
	if pIADS == nil {
		pIADS = NewArithIntDecoder()
	}
	if pIAIT == nil {
		pIAIT = NewArithIntDecoder()
	}
	if pIARI == nil {
		pIARI = NewArithIntDecoder()
	}
	if pIARDW == nil {
		pIARDW = NewArithIntDecoder()
	}
	if pIARDH == nil {
		pIARDH = NewArithIntDecoder()
	}
	if pIARDX == nil {
		pIARDX = NewArithIntDecoder()
	}
	if pIARDY == nil {
		pIARDY = NewArithIntDecoder()
	}
	if pIAID == nil {
		pIAID = NewArithIaidDecoder(t.SBSYMCODELEN)
	}
	sbReg := NewImage(int32(t.SBW), int32(t.SBH))
	if sbReg == nil {
		return nil, errors.New("failed to create text region image")
	}
	sbReg.Fill(t.SBDEFPIXEL)
	var initialStript int32
	if res, ok := pIADT.Decode(arithDecoder); !ok {
		return nil, errors.New("failed to decode initial stript")
	} else {
		initialStript = res
	}
	STRIPT := int64(initialStript) * int64(t.SBSTRIPS)
	STRIPT = -STRIPT
	FIRSTS := int64(0)
	NINSTANCES := uint32(0)
	for NINSTANCES < t.SBNUMINSTANCES {
		var initialDt int32
		if res, ok := pIADT.Decode(arithDecoder); !ok {
			return nil, errors.New("iadt decode failed")
		} else {
			initialDt = res
		}
		STRIPT += int64(initialDt) * int64(t.SBSTRIPS)
		bFirst := true
		CURS := int64(0)
		for {
			if bFirst {
				dfs, ok := pIAFS.Decode(arithDecoder)
				if !ok {
					return nil, errors.New("iafs decode failed")
				}
				FIRSTS += int64(dfs)
				CURS = FIRSTS
				bFirst = false
			} else {
				idsVal, ok := pIADS.Decode(arithDecoder)
				if !ok {
					break
				}
				dso := int32(t.SBDSOFFSET)
				if dso >= 16 {
					dso -= 32
				}
				CURS += int64(idsVal) + int64(dso)
			}
			if NINSTANCES >= t.SBNUMINSTANCES {
				break
			}
			CURT := int32(0)
			if t.SBSTRIPS != 1 {
				res, ok := pIAIT.Decode(arithDecoder)
				if !ok {
					return nil, errors.New("iait decode failed")
				}
				CURT = res
			}
			TI := int32(STRIPT + int64(CURT))
			IDI, err := pIAID.Decode(arithDecoder)
			if err != nil {
				return nil, err
			}
			if uint32(IDI) >= t.SBNUMSYMS {
				return nil, errors.New("idi out of bounds")
			}
			RI := int32(0)
			if t.SBREFINE {
				res, ok := pIARI.Decode(arithDecoder)
				if !ok {
					return nil, errors.New("failed to decode refinement flag")
				}
				RI = res
			}
			var IBI *Image
			if RI == 0 {
				if uint32(IDI) < uint32(len(t.SBSYMS)) {
					IBI = t.SBSYMS[IDI]
				}
			} else {
				rdwi, ok := pIARDW.Decode(arithDecoder)
				if !ok {
					return nil, errors.New("failed to decode refinement width")
				}
				rdhi, ok := pIARDH.Decode(arithDecoder)
				if !ok {
					return nil, errors.New("failed to decode refinement height")
				}
				rdxi, ok := pIARDX.Decode(arithDecoder)
				if !ok {
					return nil, errors.New("failed to decode refinement x")
				}
				rdyi, ok := pIARDY.Decode(arithDecoder)
				if !ok {
					return nil, errors.New("failed to decode refinement y")
				}
				IBOI := t.SBSYMS[IDI]
				if IBOI != nil {
					WOI, okW := checkTRDDimension(uint32(IBOI.width), rdwi)
					HOI, okH := checkTRDDimension(uint32(IBOI.height), rdhi)
					refDX, okDX := checkTRDReferenceDimension(rdwi, 1, rdxi)
					refDY, okDY := checkTRDReferenceDimension(rdhi, 1, rdyi)
					if okW && okH && okDX && okDY {
						pGRRD := NewGRRDProc()
						pGRRD.GRW = WOI
						pGRRD.GRH = HOI
						pGRRD.GRTEMPLATE = t.SBRTEMPLATE
						pGRRD.GRREFERENCE = IBOI
						pGRRD.GRREFERENCEDX = refDX
						pGRRD.GRREFERENCEDY = refDY
						pGRRD.TPGRON = false
						pGRRD.GRAT = t.SBRAT
						IBI, err = pGRRD.Decode(arithDecoder, grContexts)
						if err != nil {
							return nil, err
						}
					}
				}
			}
			if IBI != nil {
				WI := uint32(IBI.width)
				HI := uint32(IBI.height)
				if !t.TRANSPOSED && (t.REFCORNER == JBig2CornerTopRight || t.REFCORNER == JBig2CornerBottomRight) {
					CURS += int64(WI) - 1
				} else if t.TRANSPOSED && (t.REFCORNER == JBig2CornerBottomLeft || t.REFCORNER == JBig2CornerBottomRight) {
					CURS += int64(HI) - 1
				}
				SI := int32(CURS)
				compose := t.GetComposeData(SI, TI, WI, HI)
				IBI.ComposeTo(sbReg, int32(compose.x), int32(compose.y), t.SBCOMBOP)
				if compose.increment > 0 {
					CURS += int64(compose.increment)
				}
				NINSTANCES++
			}
		}
	}
	return sbReg, nil
}
