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

// PDDProc 模式字典解码过程
type PDDProc struct {
	HDMMR      bool
	HDPW, HDPH uint8
	GRAYMAX    uint32
	HDTEMPLATE uint8
}

// NewPDDProc 创建模式字典解码过程对象
// 返回: *PDDProc 对象
func NewPDDProc() *PDDProc {
	return &PDDProc{}
}

// createGRDProc 创建通用区域解码过程对象
// 返回: *GRDProc 对象
func (p *PDDProc) createGRDProc() *GRDProc {
	width := (p.GRAYMAX + 1) * uint32(p.HDPW)
	height := uint32(p.HDPH)
	if width > JBig2MaxImageSize || height > JBig2MaxImageSize {
		return nil
	}
	grd := NewGRDProc()
	grd.MMR = p.HDMMR
	grd.GBW = width
	grd.GBH = height
	return grd
}

// DecodeArith 算术解码
// 入参: arithDecoder 算术解码器, gbContexts 上下文集
// 返回: *PatternDict 模式字典对象, error 错误信息
func (p *PDDProc) DecodeArith(arithDecoder *ArithDecoder, gbContexts []ArithCtx) (*PatternDict, error) {
	grd := p.createGRDProc()
	if grd == nil {
		return nil, errors.New("failed to create grdproc")
	}
	grd.GBTEMPLATE = p.HDTEMPLATE
	grd.TPGDON = false
	grd.USESKIP = false
	grd.GBAT[0] = -int8(p.HDPW)
	grd.GBAT[1] = 0
	if grd.GBTEMPLATE == 0 {
		grd.GBAT[2] = -3
		grd.GBAT[3] = -1
		grd.GBAT[4] = 2
		grd.GBAT[5] = -2
		grd.GBAT[6] = -2
		grd.GBAT[7] = -2
	}
	var bhdc *Image
	state := &ProgressiveArithDecodeState{
		Image:        &bhdc,
		ArithDecoder: arithDecoder,
		GbContexts:   gbContexts,
	}
	status := grd.StartDecodeArith(state)
	if status == JBig2SegmentError || bhdc == nil {
		return nil, errors.New("arith decoding failure")
	}
	dict := NewPatternDict(p.GRAYMAX + 1)
	hdpw := int32(p.HDPW)
	hdph := int32(p.HDPH)
	for gray := uint32(0); gray <= p.GRAYMAX; gray++ {
		subImg := bhdc.SubImage(int32(gray)*hdpw, 0, hdpw, hdph)
		dict.HDPATS[gray] = subImg
	}
	return dict, nil
}

// DecodeMMR MMR解码
// 入参: stream 位流
// 返回: *PatternDict 模式字典对象, error 错误信息
func (p *PDDProc) DecodeMMR(stream *BitStream) (*PatternDict, error) {
	grd := p.createGRDProc()
	if grd == nil {
		return nil, errors.New("failed to create grdproc")
	}
	var bhdc *Image
	status := grd.StartDecodeMMR(&bhdc, stream)
	if status == JBig2SegmentError || bhdc == nil {
		return nil, errors.New("mmr decoding failure")
	}
	dict := NewPatternDict(p.GRAYMAX + 1)
	hdpw := int32(p.HDPW)
	hdph := int32(p.HDPH)
	for gray := uint32(0); gray <= p.GRAYMAX; gray++ {
		subImg := bhdc.SubImage(int32(gray)*hdpw, 0, hdpw, hdph)
		dict.HDPATS[gray] = subImg
	}
	return dict, nil
}
