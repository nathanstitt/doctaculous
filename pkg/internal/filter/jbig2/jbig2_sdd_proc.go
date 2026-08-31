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

// SDDProc 符号字典解码过程
type SDDProc struct {
	SDHUFF        bool
	SDREFAGG      bool
	SDMMR         bool
	SDRTEMPLATE   bool
	SDTEMPLATE    uint8
	SDNUMINSYMS   uint32
	SDNUMNEWSYMS  uint32
	SDNUMEXSYMS   uint32
	SDINSYMS      []*Image
	SDHUFFDH      *HuffmanTable
	SDHUFFDW      *HuffmanTable
	SDHUFFBMSIZE  *HuffmanTable
	SDHUFFAGGINST *HuffmanTable
	SDAT          [8]int8
	SDRAT         [4]int8
}

// NewSDDProc 创建符号字典解码过程对象
// 返回: *SDDProc 对象
func NewSDDProc() *SDDProc {
	return &SDDProc{}
}

// DecodeArith 算术解码
// 入参: arithDecoder 算术解码器, gbContexts 通用上下文, grContexts 细化上下文
// 返回: *SymbolDict 符号字典, error 错误信息
func (s *SDDProc) DecodeArith(arithDecoder *ArithDecoder, gbContexts, grContexts []ArithCtx) (*SymbolDict, error) {
	IADH := NewArithIntDecoder()
	IADW := NewArithIntDecoder()
	IAAI := NewArithIntDecoder()
	IARDX := NewArithIntDecoder()
	IARDY := NewArithIntDecoder()
	IAEX := NewArithIntDecoder()
	IADT := NewArithIntDecoder()
	IAFS := NewArithIntDecoder()
	IADS := NewArithIntDecoder()
	IAIT := NewArithIntDecoder()
	IARI := NewArithIntDecoder()
	IARDW := NewArithIntDecoder()
	IARDH := NewArithIntDecoder()
	SBSYMCODELENA := uint8(0)
	for (uint32(1) << SBSYMCODELENA) < (s.SDNUMINSYMS + s.SDNUMNEWSYMS) {
		SBSYMCODELENA++
	}
	IAID := NewArithIaidDecoder(SBSYMCODELENA)
	SDNEWSYMS := make([]*Image, s.SDNUMNEWSYMS)
	HCHEIGHT := uint32(0)
	NSYMSDECODED := uint32(0)
	for NSYMSDECODED < s.SDNUMNEWSYMS {
		var BS *Image
		HCDH, ok := IADH.Decode(arithDecoder)
		if !ok {
			return nil, errors.New("failed to decode hcdh")
		}
		HCHEIGHT = uint32(int32(HCHEIGHT) + HCDH)
		if HCHEIGHT > JBig2MaxImageSize {
			return nil, errors.New("image height too large")
		}
		SYMWIDTH := uint32(0)
		for {
			DW, ok := IADW.Decode(arithDecoder)
			if !ok {
				break
			}
			if NSYMSDECODED >= s.SDNUMNEWSYMS {
				return nil, errors.New("too many symbols decoded")
			}
			SYMWIDTH = uint32(int32(SYMWIDTH) + DW)
			if SYMWIDTH > JBig2MaxImageSize {
				return nil, errors.New("image width too large")
			}
			if HCHEIGHT == 0 || SYMWIDTH == 0 {
				NSYMSDECODED++
				continue
			}
			if !s.SDREFAGG {
				pGRD := NewGRDProc()
				pGRD.MMR = false
				pGRD.GBW = SYMWIDTH
				pGRD.GBH = HCHEIGHT
				pGRD.GBTEMPLATE = s.SDTEMPLATE
				pGRD.TPGDON = false
				pGRD.USESKIP = false
				copy(pGRD.GBAT[:], s.SDAT[:])
				var err error
				BS, err = pGRD.DecodeArith(arithDecoder, gbContexts)
				if err != nil {
					return nil, err
				}
			} else {
				REFAGGNINST, ok := IAAI.Decode(arithDecoder)
				if !ok {
					return nil, errors.New("failed to decode refaggninst")
				}
				if REFAGGNINST > 1 {
					pDecoder := NewTRDProc()
					pDecoder.SBHUFF = s.SDHUFF
					pDecoder.SBREFINE = true
					pDecoder.SBW = SYMWIDTH
					pDecoder.SBH = HCHEIGHT
					pDecoder.SBNUMINSTANCES = uint32(REFAGGNINST)
					pDecoder.SBSTRIPS = 1
					pDecoder.SBNUMSYMS = s.SDNUMINSYMS + NSYMSDECODED
					nTmp := uint32(0)
					for (uint32(1) << nTmp) < pDecoder.SBNUMSYMS {
						nTmp++
					}
					pDecoder.SBSYMCODELEN = uint8(nTmp)
					pDecoder.SBSYMS = make([]*Image, pDecoder.SBNUMSYMS)
					copy(pDecoder.SBSYMS, s.SDINSYMS)
					for i := 0; i < int(NSYMSDECODED); i++ {
						pDecoder.SBSYMS[int(s.SDNUMINSYMS)+i] = SDNEWSYMS[i]
					}
					pDecoder.SBDEFPIXEL = false
					pDecoder.SBCOMBOP = ComposeOr
					pDecoder.TRANSPOSED = false
					pDecoder.REFCORNER = JBig2CornerTopLeft
					pDecoder.SBDSOFFSET = 0
					pDecoder.SBRTEMPLATE = s.SDRTEMPLATE
					pDecoder.SBRAT = s.SDRAT
					ids := &IntDecoderState{
						IADT: IADT, IAFS: IAFS, IADS: IADS, IAIT: IAIT,
						IARI: IARI, IARDW: IARDW, IARDH: IARDH, IARDX: IARDX, IARDY: IARDY,
						IAID: IAID,
					}
					var err error
					BS, err = pDecoder.DecodeArith(arithDecoder, grContexts, ids)
					if err != nil {
						return nil, err
					}
				} else if REFAGGNINST == 1 {
					SBNUMSYMS := s.SDNUMINSYMS + NSYMSDECODED
					IDI, err := IAID.Decode(arithDecoder)
					if err != nil {
						return nil, err
					}
					if uint32(IDI) >= SBNUMSYMS {
						return nil, errors.New("idi out of bounds")
					}
					var sbsyms_idi *Image
					if uint32(IDI) < s.SDNUMINSYMS {
						sbsyms_idi = s.SDINSYMS[IDI]
					} else {
						sbsyms_idi = SDNEWSYMS[uint32(IDI)-s.SDNUMINSYMS]
					}
					if sbsyms_idi == nil {
						return nil, errors.New("referenced symbol is nil")
					}
					RDXI, ok := IARDX.Decode(arithDecoder)
					if !ok {
						return nil, errors.New("failed to decode refinement x")
					}
					RDYI, ok := IARDY.Decode(arithDecoder)
					if !ok {
						return nil, errors.New("failed to decode refinement y")
					}
					pGRRD := NewGRRDProc()
					pGRRD.GRW = SYMWIDTH
					pGRRD.GRH = HCHEIGHT
					pGRRD.GRTEMPLATE = s.SDRTEMPLATE
					pGRRD.GRREFERENCE = sbsyms_idi
					pGRRD.GRREFERENCEDX = RDXI
					pGRRD.GRREFERENCEDY = RDYI
					pGRRD.TPGRON = false
					pGRRD.GRAT = s.SDRAT
					BS, err = pGRRD.Decode(arithDecoder, grContexts)
					if err != nil {
						return nil, err
					}
				}
			}
			SDNEWSYMS[NSYMSDECODED] = BS
			NSYMSDECODED++
		}
	}
	EXFLAGS := make([]bool, s.SDNUMINSYMS+s.SDNUMNEWSYMS)
	CUREXFLAG := false
	EXINDEX := uint32(0)
	num_ex_syms := uint32(0)
	for EXINDEX < s.SDNUMINSYMS+s.SDNUMNEWSYMS {
		EXRUNLENGTH, ok := IAEX.Decode(arithDecoder)
		if !ok {
			return nil, errors.New("failed to decode exrunlength")
		}
		if EXINDEX+uint32(EXRUNLENGTH) > s.SDNUMINSYMS+s.SDNUMNEWSYMS {
			return nil, errors.New("exrunlength out of bounds")
		}
		if CUREXFLAG {
			num_ex_syms += uint32(EXRUNLENGTH)
		}
		for i := uint32(0); i < uint32(EXRUNLENGTH); i++ {
			EXFLAGS[EXINDEX+i] = CUREXFLAG
		}
		EXINDEX += uint32(EXRUNLENGTH)
		CUREXFLAG = !CUREXFLAG
	}
	if num_ex_syms > s.SDNUMEXSYMS {
		return nil, errors.New("too many exported symbols")
	}
	dict := NewSymbolDict()
	for i := uint32(0); i < s.SDNUMINSYMS+s.SDNUMNEWSYMS; i++ {
		if !EXFLAGS[i] {
			continue
		}
		if i < s.SDNUMINSYMS {
			img := s.SDINSYMS[i]
			if img != nil {
				newImg := img.Duplicate()
				dict.AddImage(newImg)
			} else {
				dict.AddImage(nil)
			}
		} else {
			dict.AddImage(SDNEWSYMS[i-s.SDNUMINSYMS])
		}
	}
	return dict, nil
}

// DecodeHuffman 霍夫曼解码
// 入参: stream 位流, gbContexts 通用上下文, grContexts 细化上下文
// 返回: *SymbolDict 符号字典, error 错误信息
func (s *SDDProc) DecodeHuffman(stream *BitStream, gbContexts, grContexts []ArithCtx) (*SymbolDict, error) {
	huffmanDecoder := NewHuffmanDecoder(stream)
	SDNEWSYMS := make([]*Image, s.SDNUMNEWSYMS)
	var SDNEWSYMWIDTHS []uint32
	if !s.SDREFAGG {
		SDNEWSYMWIDTHS = make([]uint32, s.SDNUMNEWSYMS)
	}
	HCHEIGHT := uint32(0)
	NSYMSDECODED := uint32(0)
	for NSYMSDECODED < s.SDNUMNEWSYMS {
		var HCDH int32
		if res := huffmanDecoder.DecodeAValue(s.SDHUFFDH, &HCDH); res != 0 {
			return nil, errors.New("failed to decode hcdh")
		}
		HCHEIGHT = uint32(int32(HCHEIGHT) + HCDH)
		if HCHEIGHT > JBig2MaxImageSize {
			return nil, errors.New("image height too large")
		}
		SYMWIDTH := uint32(0)
		TOTWIDTH := uint32(0)
		HCFIRSTSYM := NSYMSDECODED
		for {
			var DW int32
			res := huffmanDecoder.DecodeAValue(s.SDHUFFDW, &DW)
			if res == JBig2OOB {
				break
			}
			if res != 0 {
				return nil, errors.New("failed to decode dw")
			}
			if NSYMSDECODED >= s.SDNUMNEWSYMS {
				return nil, errors.New("too many symbols decoded")
			}
			SYMWIDTH = uint32(int32(SYMWIDTH) + DW)
			if SYMWIDTH > JBig2MaxImageSize {
				return nil, errors.New("image width too large")
			}
			TOTWIDTH += SYMWIDTH
			if HCHEIGHT == 0 || SYMWIDTH == 0 {
				NSYMSDECODED++
				continue
			}
			var BS *Image
			if s.SDREFAGG {
				var REFAGGNINST int32
				if huffmanDecoder.DecodeAValue(s.SDHUFFAGGINST, &REFAGGNINST) != 0 {
					return nil, errors.New("failed to decode refaggninst")
				}
				if REFAGGNINST > 1 {
					pDecoder := NewTRDProc()
					pDecoder.SBHUFF = s.SDHUFF
					pDecoder.SBREFINE = true
					pDecoder.SBW = SYMWIDTH
					pDecoder.SBH = HCHEIGHT
					pDecoder.SBNUMINSTANCES = uint32(REFAGGNINST)
					pDecoder.SBSTRIPS = 1
					pDecoder.SBNUMSYMS = s.SDNUMINSYMS + NSYMSDECODED
					pDecoder.SBSYMCODES = make([]HuffmanCode, pDecoder.SBNUMSYMS)
					nTmp := uint32(1)
					for (uint32(1) << nTmp) < pDecoder.SBNUMSYMS {
						nTmp++
					}
					for i := uint32(0); i < pDecoder.SBNUMSYMS; i++ {
						pDecoder.SBSYMCODES[i].Codelen = int32(nTmp)
						pDecoder.SBSYMCODES[i].Code = int32(i)
					}
					pDecoder.SBSYMS = make([]*Image, pDecoder.SBNUMSYMS)
					copy(pDecoder.SBSYMS, s.SDINSYMS)
					for i := 0; i < int(NSYMSDECODED); i++ {
						pDecoder.SBSYMS[int(s.SDNUMINSYMS)+i] = SDNEWSYMS[i]
					}
					pDecoder.SBDEFPIXEL = false
					pDecoder.SBCOMBOP = ComposeOr
					pDecoder.TRANSPOSED = false
					pDecoder.REFCORNER = JBig2CornerTopLeft
					pDecoder.SBDSOFFSET = 0
					pDecoder.SBHUFFFS = NewStandardTable(6)
					pDecoder.SBHUFFDS = NewStandardTable(8)
					pDecoder.SBHUFFDT = NewStandardTable(11)
					pDecoder.SBHUFFRDW = NewStandardTable(15)
					pDecoder.SBHUFFRDH = NewStandardTable(15)
					pDecoder.SBHUFFRDX = NewStandardTable(15)
					pDecoder.SBHUFFRDY = NewStandardTable(15)
					pDecoder.SBHUFFRSIZE = NewStandardTable(1)
					pDecoder.SBRTEMPLATE = s.SDRTEMPLATE
					pDecoder.SBRAT = s.SDRAT
					var err error
					BS, err = pDecoder.DecodeHuffman(stream, grContexts)
					if err != nil {
						return nil, err
					}
				} else if REFAGGNINST == 1 {
					SBNUMSYMS := s.SDNUMINSYMS + NSYMSDECODED
					nTmp := uint32(1)
					for (uint32(1) << nTmp) < SBNUMSYMS {
						nTmp++
					}
					SBSYMCODELEN := nTmp
					IDI := uint32(0)
					for n := uint32(0); n < SBSYMCODELEN; n++ {
						val, err := stream.Read1Bit()
						if err != nil {
							return nil, err
						}
						IDI = (IDI << 1) | val
					}
					if IDI >= SBNUMSYMS {
						return nil, errors.New("idi out of bounds")
					}
					var sbsyms_idi *Image
					if IDI < s.SDNUMINSYMS {
						sbsyms_idi = s.SDINSYMS[IDI]
					} else {
						sbsyms_idi = SDNEWSYMS[IDI-s.SDNUMINSYMS]
					}
					if sbsyms_idi == nil {
						return nil, errors.New("referenced symbol is nil")
					}
					SBHUFFRDX := NewStandardTable(15)
					SBHUFFRSIZE := NewStandardTable(1)
					var RDXI, RDYI, nVal int32
					if huffmanDecoder.DecodeAValue(SBHUFFRDX, &RDXI) != 0 ||
						huffmanDecoder.DecodeAValue(SBHUFFRDX, &RDYI) != 0 ||
						huffmanDecoder.DecodeAValue(SBHUFFRSIZE, &nVal) != 0 {
						return nil, errors.New("failed to decode refinement values")
					}
					stream.AlignByte()
					pGRRD := NewGRRDProc()
					pGRRD.GRW = SYMWIDTH
					pGRRD.GRH = HCHEIGHT
					pGRRD.GRTEMPLATE = s.SDRTEMPLATE
					pGRRD.GRREFERENCE = sbsyms_idi
					pGRRD.GRREFERENCEDX = RDXI
					pGRRD.GRREFERENCEDY = RDYI
					pGRRD.TPGRON = false
					pGRRD.GRAT = s.SDRAT
					arithDecoder := NewArithDecoder(stream)
					var err error
					BS, err = pGRRD.Decode(arithDecoder, grContexts)
					if err != nil {
						return nil, err
					}
					stream.AlignByte()
					stream.AddOffset(2)
				}
				SDNEWSYMS[NSYMSDECODED] = BS
			}
			if !s.SDREFAGG {
				SDNEWSYMWIDTHS[NSYMSDECODED] = SYMWIDTH
			}
			NSYMSDECODED++
		}
		if !s.SDREFAGG {
			var BMSIZE int32
			if huffmanDecoder.DecodeAValue(s.SDHUFFBMSIZE, &BMSIZE) != 0 {
				return nil, errors.New("failed to decode bmsize")
			}
			stream.AlignByte()
			var BHC *Image
			if BMSIZE == 0 {
				stride := (TOTWIDTH + 7) / 8
				if stream.GetByteLeft() < stride*HCHEIGHT {
					return nil, errors.New("insufficient data for grid")
				}
				BHC = NewImage(int32(TOTWIDTH), int32(HCHEIGHT))
				if BHC == nil {
					return nil, errors.New("failed to create grid image")
				}
				data := stream.GetPointer()
				bhcData := BHC.Data()
				for i := uint32(0); i < HCHEIGHT; i++ {
					copy(bhcData[int32(i)*BHC.Stride():], data[i*stride:i*stride+stride])
				}
				stream.AddOffset(stride * HCHEIGHT)
			} else {
				pGRD := NewGRDProc()
				if s.SDHUFF {
					pGRD.MMR = true
				} else {
					pGRD.MMR = s.SDMMR
				}
				pGRD.GBW = TOTWIDTH
				pGRD.GBH = HCHEIGHT
				if !pGRD.MMR {
					pGRD.GBAT = [8]int8{0, 0, 0, 0, 0, 0, 0, 0}
					gbContexts := make([]ArithCtx, 65536)
					arithDecoder := NewArithDecoder(stream)
					var err error
					BHC, err = pGRD.DecodeArith(arithDecoder, gbContexts)
					if err != nil {
						return nil, err
					}
				} else {
					if stream.GetByteLeft() < uint32(BMSIZE) {
						return nil, errors.New("insufficient data for mmr")
					}
					mmrData := make([]byte, BMSIZE)
					for i := int32(0); i < BMSIZE; i++ {
						val, err := stream.Read1Byte()
						if err != nil {
							return nil, err
						}
						mmrData[i] = val
					}
					mmrStream := NewBitStream(mmrData, 0)
					if pGRD.StartDecodeMMR(&BHC, mmrStream) == JBig2SegmentError || BHC == nil {
						return nil, errors.New("mmr decoding failure")
					}
				}
			}
			if BHC != nil {
				nTmp := uint32(0)
				currentSym := HCFIRSTSYM
				for i := uint32(0); i < NSYMSDECODED-HCFIRSTSYM; i++ {
					idx := currentSym + i
					SDNEWSYMS[idx] = BHC.SubImage(int32(nTmp), 0, int32(SDNEWSYMWIDTHS[idx]), int32(HCHEIGHT))
					nTmp += SDNEWSYMWIDTHS[idx]
				}
			}
		}
	}
	EXFLAGS := make([]bool, s.SDNUMINSYMS+s.SDNUMNEWSYMS)
	CUREXFLAG := false
	EXINDEX := uint32(0)
	num_ex_syms := uint32(0)
	pTable := NewStandardTable(1)
	for EXINDEX < s.SDNUMINSYMS+s.SDNUMNEWSYMS {
		var EXRUNLENGTH int32
		if res := huffmanDecoder.DecodeAValue(pTable, &EXRUNLENGTH); res != 0 {
			return nil, errors.New("failed to decode exrunlength")
		}
		if EXINDEX+uint32(EXRUNLENGTH) > s.SDNUMINSYMS+s.SDNUMNEWSYMS {
			return nil, errors.New("exrunlength out of bounds")
		}
		if CUREXFLAG {
			num_ex_syms += uint32(EXRUNLENGTH)
		}
		for i := uint32(0); i < uint32(EXRUNLENGTH); i++ {
			EXFLAGS[EXINDEX+i] = CUREXFLAG
		}
		EXINDEX += uint32(EXRUNLENGTH)
		CUREXFLAG = !CUREXFLAG
	}
	if num_ex_syms > s.SDNUMEXSYMS {
		return nil, errors.New("too many exported symbols")
	}
	dict := NewSymbolDict()
	for i := uint32(0); i < s.SDNUMINSYMS+s.SDNUMNEWSYMS; i++ {
		if !EXFLAGS[i] {
			continue
		}
		if i < s.SDNUMINSYMS {
			img := s.SDINSYMS[i]
			if img != nil {
				newImg := img.Duplicate()
				dict.AddImage(newImg)
			} else {
				dict.AddImage(nil)
			}
		} else {
			dict.AddImage(SDNEWSYMS[i-s.SDNUMINSYMS])
		}
	}
	return dict, nil
}
