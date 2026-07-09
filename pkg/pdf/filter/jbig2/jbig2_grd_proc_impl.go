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

var (
	// kOptConstant1 优化常量1
	kOptConstant1 = []uint16{0x9b25, 0x0795, 0x00e5}
	// kOptConstant9 优化常量9
	kOptConstant9 = []uint{0x000c, 0x0009, 0x0007}
	// kOptConstant10 优化常量10
	kOptConstant10 = []uint32{0x0007, 0x000f, 0x0007}
	// kOptConstant11 优化常量11
	kOptConstant11 = []uint32{0x001f, 0x001f, 0x000f}
	// kOptConstant12 优化常量12
	kOptConstant12 = []uint32{0x000f, 0x0007, 0x0003}
)

// decodeTemplate0Opt3 模板0优化3解码
// 入参: state 解码状态
// 返回: JBig2SegmentState 状态
func (g *GRDProc) decodeTemplate0Opt3(state *ProgressiveArithDecodeState) JBig2SegmentState {
	return g.decodeTemplateUnopt(state, 0)
}

// decodeTemplate0Unopt 模板0非优化解码
// 入参: state 解码状态
// 返回: JBig2SegmentState 状态
func (g *GRDProc) decodeTemplate0Unopt(state *ProgressiveArithDecodeState) JBig2SegmentState {
	return g.decodeTemplateUnopt(state, 0)
}

// decodeTemplate1Opt3 模板1优化3解码
// 入参: state 解码状态
// 返回: JBig2SegmentState 状态
func (g *GRDProc) decodeTemplate1Opt3(state *ProgressiveArithDecodeState) JBig2SegmentState {
	return g.decodeTemplateUnopt(state, 1)
}

// decodeTemplate1Unopt 模板1非优化解码
// 入参: state 解码状态
// 返回: JBig2SegmentState 状态
func (g *GRDProc) decodeTemplate1Unopt(state *ProgressiveArithDecodeState) JBig2SegmentState {
	return g.decodeTemplateUnopt(state, 1)
}

// decodeTemplate23Opt3 模板2/3优化3解码
// 入参: state 解码状态, opt 选项
// 返回: JBig2SegmentState 状态
func (g *GRDProc) decodeTemplate23Opt3(state *ProgressiveArithDecodeState, opt int) JBig2SegmentState {
	return g.decodeTemplateUnopt(state, opt)
}

// decodeTemplateUnopt 通用算术解码
// 入参: state 解码状态, opt 选项
// 返回: JBig2SegmentState 状态
func (g *GRDProc) decodeTemplateUnopt(state *ProgressiveArithDecodeState, opt int) JBig2SegmentState {
	if state.Image == nil || *state.Image == nil {
		return JBig2SegmentError
	}
	img := *state.Image
	gbContexts := state.GbContexts
	decoder := state.ArithDecoder
	mod2 := int32(opt % 2)
	div2 := int32(opt / 2)
	shift := uint(4 - opt)
	shiftC9 := kOptConstant9[opt]
	for ; g.loopIndex < g.GBH; g.loopIndex++ {
		h := int32(g.loopIndex)
		if g.TPGDON {
			if decoder.IsComplete() {
				return JBig2SegmentError
			}
			bit := decoder.Decode(&gbContexts[kOptConstant1[opt]])
			if bit != 0 {
				g.ltp ^= 1
			}
		}
		if g.ltp == 1 {
			img.CopyLine(h, h-1)
			continue
		}
		line1 := uint32(img.GetPixel(1+mod2, h-2))
		line1 |= uint32(img.GetPixel(mod2, h-2)) << 1
		if opt == 1 {
			line1 |= uint32(img.GetPixel(0, h-2)) << 2
		}
		line2 := uint32(img.GetPixel(2-div2, h-1))
		line2 |= uint32(img.GetPixel(1-div2, h-1)) << 1
		if opt < 2 {
			line2 |= uint32(img.GetPixel(0, h-1)) << 2
		}
		line3 := uint32(0)
		for w := int32(0); w < int32(g.GBW); w++ {
			bVal := 0
			skip := false
			if g.USESKIP && g.SKIP != nil && g.SKIP.GetPixel(w, h) != 0 {
				skip = true
				bVal = 0
			}
			if !skip {
				if decoder.IsComplete() {
					return JBig2SegmentError
				}
				CONTEXT := line3
				CONTEXT |= uint32(img.GetPixel(w+int32(g.GBAT[0]), h+int32(g.GBAT[1]))) << shift
				CONTEXT |= line2 << (shift + 1)
				CONTEXT |= line1 << shiftC9
				if opt == 0 {
					CONTEXT |= uint32(img.GetPixel(w+int32(g.GBAT[2]), h+int32(g.GBAT[3]))) << 10
					CONTEXT |= uint32(img.GetPixel(w+int32(g.GBAT[4]), h+int32(g.GBAT[5]))) << 11
					CONTEXT |= uint32(img.GetPixel(w+int32(g.GBAT[6]), h+int32(g.GBAT[7]))) << 15
				}
				bVal = decoder.Decode(&gbContexts[CONTEXT])
			}
			if bVal != 0 {
				img.SetPixel(w, h, bVal)
			}
			line1 = ((line1 << 1) | uint32(img.GetPixel(w+2+mod2, h-2))) & kOptConstant10[opt]
			line2 = ((line2 << 1) | uint32(img.GetPixel(w+3-div2, h-1))) & kOptConstant11[opt]
			line3 = ((line3 << 1) | uint32(bVal)) & kOptConstant12[opt]
		}
	}
	return JBig2SegmentParseComplete
}

// decodeTemplate3Unopt 模板3非优化解码
// 入参: state 解码状态
// 返回: JBig2SegmentState 状态
func (g *GRDProc) decodeTemplate3Unopt(state *ProgressiveArithDecodeState) JBig2SegmentState {
	if state.Image == nil || *state.Image == nil {
		return JBig2SegmentError
	}
	img := *state.Image
	gbContexts := state.GbContexts
	decoder := state.ArithDecoder
	for ; g.loopIndex < g.GBH; g.loopIndex++ {
		h := int32(g.loopIndex)
		if g.TPGDON {
			if decoder.IsComplete() {
				return JBig2SegmentError
			}
			bit := decoder.Decode(&gbContexts[0x0195])
			if bit != 0 {
				g.ltp ^= 1
			}
		}
		if g.ltp == 1 {
			img.CopyLine(h, h-1)
			continue
		}
		line1 := uint32(img.GetPixel(1, h-1))
		line1 |= uint32(img.GetPixel(0, h-1)) << 1
		line2 := uint32(0)
		for w := int32(0); w < int32(g.GBW); w++ {
			bVal := 0
			skip := false
			if g.USESKIP && g.SKIP != nil && g.SKIP.GetPixel(w, h) != 0 {
				skip = true
				bVal = 0
			}
			if !skip {
				if decoder.IsComplete() {
					return JBig2SegmentError
				}
				CONTEXT := line2
				CONTEXT |= uint32(img.GetPixel(w+int32(g.GBAT[0]), h+int32(g.GBAT[1]))) << 4
				CONTEXT |= line1 << 5
				bVal = decoder.Decode(&gbContexts[CONTEXT])
			}
			if bVal != 0 {
				img.SetPixel(w, h, bVal)
			}
			line1 = ((line1 << 1) | uint32(img.GetPixel(w+2, h-1))) & 0x1f
			line2 = ((line2 << 1) | uint32(bVal)) & 0x0f
		}
	}
	return JBig2SegmentParseComplete
}
