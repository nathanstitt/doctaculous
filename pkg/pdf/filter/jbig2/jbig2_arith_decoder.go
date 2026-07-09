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

import "errors"

// defaultAValue 默认A值
const defaultAValue = 0x8000

// kQeTable Qe表
var kQeTable = []ArithQe{
	{0x5601, 1, 1, true}, {0x3401, 2, 6, false}, {0x1801, 3, 9, false},
	{0x0AC1, 4, 12, false}, {0x0521, 5, 29, false}, {0x0221, 38, 33, false},
	{0x5601, 7, 6, true}, {0x5401, 8, 14, false}, {0x4801, 9, 14, false},
	{0x3801, 10, 14, false}, {0x3001, 11, 17, false}, {0x2401, 12, 18, false},
	{0x1C01, 13, 20, false}, {0x1601, 29, 21, false}, {0x5601, 15, 14, true},
	{0x5401, 16, 14, false}, {0x5101, 17, 15, false}, {0x4801, 18, 16, false},
	{0x3801, 19, 17, false}, {0x3401, 20, 18, false}, {0x3001, 21, 19, false},
	{0x2801, 22, 19, false}, {0x2401, 23, 20, false}, {0x2201, 24, 21, false},
	{0x1C01, 25, 22, false}, {0x1801, 26, 23, false}, {0x1601, 27, 24, false},
	{0x1401, 28, 25, false}, {0x1201, 29, 26, false}, {0x1101, 30, 27, false},
	{0x0AC1, 31, 28, false}, {0x09C1, 32, 29, false}, {0x08A1, 33, 30, false},
	{0x0521, 34, 31, false}, {0x0441, 35, 32, false}, {0x02A1, 36, 33, false},
	{0x0221, 37, 34, false}, {0x0141, 38, 35, false}, {0x0111, 39, 36, false},
	{0x0085, 40, 37, false}, {0x0049, 41, 38, false}, {0x0025, 42, 39, false},
	{0x0015, 43, 40, false}, {0x0009, 44, 41, false}, {0x0005, 45, 42, false},
	{0x0001, 45, 43, false}, {0x5601, 46, 46, false},
}

// arithIntDecodeData 算术整数解码数据
type arithIntDecodeData struct {
	nNeedBits int
	nValue    int32
}

// kArithIntDecodeData 算术整数解码数据表
var kArithIntDecodeData = []arithIntDecodeData{
	{2, 0}, {4, 4}, {6, 20}, {8, 84}, {12, 340}, {32, 4436},
}

// ArithQe 算术编码状态
type ArithQe struct {
	Qe     uint16
	NMPS   uint8
	NLPS   uint8
	Switch bool
}

// ArithCtx 算术解码上下文
type ArithCtx struct {
	mps bool
	i   uint8
}

// DecodeNLPS 解码NLPS
// 入参: qe 算术编码状态
// 返回: int 解码值
func (c *ArithCtx) DecodeNLPS(qe ArithQe) int {
	d := 0
	if !c.mps {
		d = 1
	}
	if qe.Switch {
		c.mps = !c.mps
	}
	c.i = qe.NLPS
	return d
}

// DecodeNMPS 解码NMPS
// 入参: qe 算术编码状态
// 返回: int 解码值
func (c *ArithCtx) DecodeNMPS(qe ArithQe) int {
	c.i = qe.NMPS
	if c.mps {
		return 1
	}
	return 0
}

// MPS 获取MPS
// 返回: int MPS值
func (c *ArithCtx) MPS() int {
	if c.mps {
		return 1
	}
	return 0
}

// I 获取I
// 返回: uint8 I值
func (c *ArithCtx) I() uint8 {
	return c.i
}

// ArithDecoder 算术解码器
type ArithDecoder struct {
	stream   *BitStream
	b        uint8
	c        uint32
	a        uint32
	ct       uint32
	complete bool
}

// NewArithDecoder 创建新的算术解码器
// 入参: stream 位流
// 返回: *ArithDecoder 解码器对象
func NewArithDecoder(stream *BitStream) *ArithDecoder {
	ad := &ArithDecoder{stream: stream, a: defaultAValue}
	ad.b = stream.GetCurByteArith()
	ad.c = (uint32(ad.b) ^ 0xff) << 16
	ad.byteIn()
	ad.c = ad.c << 7
	ad.ct = ad.ct - 7
	return ad
}

// Decode 解码
// 入参: cx 上下文
// 返回: int 结果
func (ad *ArithDecoder) Decode(cx *ArithCtx) int {
	if int(cx.I()) >= len(kQeTable) {
		return 0
	}
	qe := kQeTable[cx.I()]
	ad.a -= uint32(qe.Qe)
	if (ad.c >> 16) < ad.a {
		if (ad.a & defaultAValue) != 0 {
			return cx.MPS()
		}
		var d int
		if ad.a < uint32(qe.Qe) {
			d = cx.DecodeNLPS(qe)
		} else {
			d = cx.DecodeNMPS(qe)
		}
		ad.readValueA()
		return d
	}
	ad.c -= ad.a << 16
	var d int
	if ad.a < uint32(qe.Qe) {
		d = cx.DecodeNMPS(qe)
	} else {
		d = cx.DecodeNLPS(qe)
	}
	ad.a = uint32(qe.Qe)
	ad.readValueA()
	return d
}

// IsComplete 是否完成
// 返回: bool 是否完成
func (ad *ArithDecoder) IsComplete() bool {
	return ad.complete
}

// byteIn 读入字节
func (ad *ArithDecoder) byteIn() {
	if ad.b == 0xff {
		b1 := ad.stream.GetNextByteArith()
		if b1 > 0x8f {
			ad.ct = 8
		} else {
			ad.stream.IncByteIdx()
			ad.b = b1
			ad.c = ad.c + 0xfe00 - (uint32(ad.b) << 9)
			ad.ct = 7
		}
	} else {
		ad.stream.IncByteIdx()
		ad.b = ad.stream.GetCurByteArith()
		ad.c = ad.c + 0xff00 - (uint32(ad.b) << 8)
		ad.ct = 8
	}
	if !ad.stream.IsInBounds() {
		ad.complete = true
	}
}

// readValueA 读取A值
func (ad *ArithDecoder) readValueA() {
	for {
		if ad.ct == 0 {
			ad.byteIn()
		}
		ad.a <<= 1
		ad.c <<= 1
		ad.ct--
		if (ad.a & defaultAValue) != 0 {
			break
		}
	}
}

// ArithIntDecoder 算术整数解码器
type ArithIntDecoder struct {
	iax []ArithCtx
}

// NewArithIntDecoder 创建新的算术整数解码器
// 返回: *ArithIntDecoder 解码器对象
func NewArithIntDecoder() *ArithIntDecoder {
	return &ArithIntDecoder{iax: make([]ArithCtx, 512)}
}

// Decode 解码
// 入参: decoder 算术解码器
// 返回: int32 结果, bool 是否成功
func (aid *ArithIntDecoder) Decode(decoder *ArithDecoder) (int32, bool) {
	prev := 1
	s := decoder.Decode(&aid.iax[prev])
	prev = (prev << 1) | s
	idx := aid.recursiveDecode(decoder, &prev, 0)
	nTemp := 0
	for i := 0; i < kArithIntDecodeData[idx].nNeedBits; i++ {
		d := decoder.Decode(&aid.iax[prev])
		prev = (prev << 1) | d
		if prev >= 256 {
			prev = (prev & 511) | 256
		}
		nTemp = (nTemp << 1) | d
	}
	val := kArithIntDecodeData[idx].nValue + int32(nTemp)
	if s == 1 && val > 0 {
		val = -val
	}
	if s == 1 && val == 0 {
		return 0, false
	}
	return val, true
}

// recursiveDecode 递归解码
// 入参: decoder 算术解码器, prev 上一个值, depth 深度
// 返回: int 解码结果
func (aid *ArithIntDecoder) recursiveDecode(decoder *ArithDecoder, prev *int, depth int) int {
	kDepthEnd := len(kArithIntDecodeData) - 1
	if depth == kDepthEnd {
		return kDepthEnd
	}
	cx := &aid.iax[*prev]
	d := decoder.Decode(cx)
	*prev = (*prev << 1) | d
	if d == 0 {
		return depth
	}
	return aid.recursiveDecode(decoder, prev, depth+1)
}

// ArithIaidDecoder IAID解码器
type ArithIaidDecoder struct {
	iaid         []ArithCtx
	sbsymCodeLen uint8
}

// NewArithIaidDecoder 创建新的IAID解码器
// 入参: sbsymCodeLen 符号编码长度
// 返回: *ArithIaidDecoder 解码器对象
func NewArithIaidDecoder(sbsymCodeLen uint8) *ArithIaidDecoder {
	return &ArithIaidDecoder{iaid: make([]ArithCtx, 1<<sbsymCodeLen), sbsymCodeLen: sbsymCodeLen}
}

// Decode 解码
// 入参: decoder 算术解码器
// 返回: uint32 结果, error 错误信息
func (aid *ArithIaidDecoder) Decode(decoder *ArithDecoder) (uint32, error) {
	prev := 1
	for i := uint8(0); i < aid.sbsymCodeLen; i++ {
		if prev >= len(aid.iaid) {
			return 0, errors.New("index out of bounds")
		}
		cx := &aid.iaid[prev]
		d := decoder.Decode(cx)
		prev = (prev << 1) | d
	}
	return uint32(prev - (1 << aid.sbsymCodeLen)), nil
}
