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

// TableLine 霍夫曼表行定义
type TableLine struct {
	PrefLen  int32
	RangeLen int32
	RangeLow int32
}

// HuffmanTable 霍夫曼表
type HuffmanTable struct {
	HTOOB    bool
	NTEMP    uint32
	CODES    []HuffmanCode
	RANGELEN []int32
	RANGELOW []int32
	Ok       bool
}

// NewStandardTable 从标准表创建霍夫曼表
// 入参: idx 表索引
// 返回: *HuffmanTable 霍夫曼表
func NewStandardTable(idx int) *HuffmanTable {
	ht := &HuffmanTable{}
	ht.parseFromStandardTable(idx)
	return ht
}

// NewTableFromStream 从流创建霍夫曼表
// 入参: stream 位流
// 返回: *HuffmanTable 霍夫曼表
func NewTableFromStream(stream *BitStream) *HuffmanTable {
	ht := &HuffmanTable{}
	ht.parseFromCodedBuffer(stream)
	return ht
}

// Size 获取霍夫曼表大小
// 返回: uint32 大小
func (h *HuffmanTable) Size() uint32 {
	return uint32(len(h.CODES))
}

// IsHTOOB 是否包含越界符
// 返回: bool 是否包含
func (h *HuffmanTable) IsHTOOB() bool {
	return h.HTOOB
}

// IsOK 霍夫曼表是否有效
// 返回: bool 是否有效
func (h *HuffmanTable) IsOK() bool {
	return h.Ok
}

// parseFromStandardTable 从标准表解析
// 入参: idx 表索引
// 返回: bool 是否成功
func (h *HuffmanTable) parseFromStandardTable(idx int) bool {
	if idx < 1 || idx >= len(kHuffmanTables) {
		return false
	}
	def := kHuffmanTables[idx]
	h.HTOOB = def.HTOOB
	h.NTEMP = uint32(len(def.Lines))
	h.CODES = make([]HuffmanCode, h.NTEMP)
	for i := 0; i < int(h.NTEMP); i++ {
		h.CODES[i].Codelen = def.Lines[i].PrefLen
		h.CODES[i].Val1 = def.Lines[i].RangeLen
		h.CODES[i].Val2 = def.Lines[i].RangeLow
	}
	h.extendBuffers(false)
	if err := HuffmanAssignCode(h.CODES); err != nil {
		h.Ok = false
	} else {
		h.Ok = true
	}
	return h.Ok
}

// parseFromCodedBuffer 从编码位流解析
// 入参: stream 位流
// 返回: bool 是否成功
func (h *HuffmanTable) parseFromCodedBuffer(stream *BitStream) bool {
	flags, err := stream.Read1Byte()
	if err != nil {
		return false
	}
	h.HTOOB = (flags & 0x01) != 0
	prefixSizeBits := uint32((flags>>1)&0x07) + 1
	rangeSizeBits := uint32((flags>>4)&0x07) + 1
	val, err := stream.ReadInteger()
	if err != nil {
		return false
	}
	lowestValue := int32(val)
	val, err = stream.ReadInteger()
	if err != nil {
		return false
	}
	highestValue := int32(val)
	currentRangeLow := int64(lowestValue)
	for currentRangeLow < int64(highestValue) {
		prefixLength, err := stream.ReadNBits(prefixSizeBits)
		if err != nil {
			return false
		}
		rangeLength, err := stream.ReadNBits(rangeSizeBits)
		if err != nil {
			return false
		}
		h.CODES = append(h.CODES, HuffmanCode{
			Codelen: int32(prefixLength),
			Val1:    int32(rangeLength),
			Val2:    int32(currentRangeLow),
		})
		currentRangeLow += int64(1) << rangeLength
	}
	prefixLength, err := stream.ReadNBits(prefixSizeBits)
	if err != nil {
		return false
	}
	h.CODES = append(h.CODES, HuffmanCode{
		Codelen:    int32(prefixLength),
		Val1:       32,
		Val2:       lowestValue - 1,
		LowerRange: true,
	})
	prefixLength, err = stream.ReadNBits(prefixSizeBits)
	if err != nil {
		return false
	}
	h.CODES = append(h.CODES, HuffmanCode{
		Codelen: int32(prefixLength),
		Val1:    32,
		Val2:    highestValue,
	})
	if h.HTOOB {
		prefixLength, err = stream.ReadNBits(prefixSizeBits)
		if err != nil {
			return false
		}
		h.CODES = append(h.CODES, HuffmanCode{
			Codelen: int32(prefixLength),
		})
	}
	h.NTEMP = uint32(len(h.CODES))
	h.extendBuffers(false)
	if err := HuffmanAssignCode(h.CODES); err != nil {
		h.Ok = false
	} else {
		h.Ok = true
	}
	return h.Ok
}

// extendBuffers 扩展内部缓冲区
// 入参: increment 是否增量
func (h *HuffmanTable) extendBuffers(increment bool) {
	h.RANGELEN = make([]int32, len(h.CODES))
	h.RANGELOW = make([]int32, len(h.CODES))
	for i := range h.CODES {
		h.RANGELEN[i] = h.CODES[i].Val1
		h.RANGELOW[i] = h.CODES[i].Val2
	}
}

// HuffmanDecoder 霍夫曼解码器
type HuffmanDecoder struct {
	stream *BitStream
}

// NewHuffmanDecoder 创建新的霍夫曼解码器
// 入参: stream 位流
// 返回: *HuffmanDecoder 解码器对象
func NewHuffmanDecoder(stream *BitStream) *HuffmanDecoder {
	return &HuffmanDecoder{stream: stream}
}

// DecodeAValue 解码一个数值
// 入参: table 霍夫曼表, result 结果指针
// 返回: int 状态码
func (h *HuffmanDecoder) DecodeAValue(table *HuffmanTable, result *int32) int {
	var val int32
	var nBits int
	for {
		if nBits > 32 {
			return -1
		}
		bit, err := h.stream.Read1Bit()
		if err != nil {
			return -1
		}
		val = (val << 1) | int32(bit)
		nBits++
		for i := 0; i < len(table.CODES); i++ {
			if table.CODES[i].Codelen == int32(nBits) && table.CODES[i].Code == val {
				if table.HTOOB && i == len(table.CODES)-1 {
					return JBig2OOB
				}
				rlen := table.RANGELEN[i]
				rlow := table.RANGELOW[i]
				if rlen < 0 {
					return JBig2OOB
				}
				if rlen > 0 {
					offset, err := h.stream.ReadNBits(uint32(rlen))
					if err != nil {
						return -1
					}
					if table.CODES[i].LowerRange {
						*result = rlow - int32(offset)
					} else {
						*result = rlow + int32(offset)
					}
				} else {
					*result = rlow
				}
				return 0
			}
		}
	}
}

// HuffmanAssignCode 为霍夫曼表分配编码
// 入参: symcodes 霍夫曼编码列表
// 返回: error 错误信息
func HuffmanAssignCode(symcodes []HuffmanCode) error {
	lenMax := int32(0)
	for _, sc := range symcodes {
		if sc.Codelen > lenMax {
			lenMax = sc.Codelen
		}
	}
	lenCounts := make([]int, lenMax+1)
	firstCodes := make([]int32, lenMax+1)
	for _, sc := range symcodes {
		if sc.Codelen > 0 {
			lenCounts[sc.Codelen]++
		}
	}
	lenCounts[0] = 0
	for i := int32(1); i <= lenMax; i++ {
		firstCodes[i] = (firstCodes[i-1] + int32(lenCounts[i-1])) << 1
		curCode := firstCodes[i]
		for j := range symcodes {
			if symcodes[j].Codelen == i {
				symcodes[j].Code = curCode
				curCode++
			}
		}
	}
	return nil
}

// standardTableDef 标准表定义
type standardTableDef struct {
	HTOOB bool
	Lines []TableLine
}

// kHuffmanTables 标准霍夫曼表集
var kHuffmanTables = []standardTableDef{
	{false, nil},
	{false, []TableLine{{1, 4, 0}, {2, 8, 16}, {3, 16, 272}, {3, 32, 65808}}},
	{true, []TableLine{{1, 0, 0}, {2, 0, 1}, {3, 0, 2}, {4, 3, 3}, {5, 6, 11}, {6, 32, 75}, {6, -1, 0}}},
	{true, []TableLine{{8, 8, -256}, {1, 0, 0}, {2, 0, 1}, {3, 0, 2}, {4, 3, 3}, {5, 6, 11}, {8, 32, -257}, {7, 32, 75}, {6, -1, 0}}},
	{false, []TableLine{{1, 0, 1}, {2, 0, 2}, {3, 0, 3}, {4, 3, 4}, {5, 6, 12}, {5, 32, 76}}},
	{false, []TableLine{{7, 8, -255}, {1, 0, 1}, {2, 0, 2}, {3, 0, 3}, {4, 3, 4}, {5, 6, 12}, {7, 32, -256}, {6, 32, 76}}},
	{false, []TableLine{{5, 10, -2048}, {4, 9, -1024}, {4, 8, -512}, {4, 7, -256}, {5, 6, -128}, {5, 5, -64}, {4, 5, -32}, {2, 7, 0}, {3, 7, 128}, {3, 8, 256}, {4, 9, 512}, {4, 10, 1024}, {6, 32, -2049}, {6, 32, 2048}}},
	{false, []TableLine{{4, 9, -1024}, {3, 8, -512}, {4, 7, -256}, {5, 6, -128}, {5, 5, -64}, {4, 5, -32}, {4, 5, 0}, {5, 5, 32}, {5, 6, 64}, {4, 7, 128}, {3, 8, 256}, {3, 9, 512}, {3, 10, 1024}, {5, 32, -1025}, {5, 32, 2048}}},
	{true, []TableLine{{8, 3, -15}, {9, 1, -7}, {8, 1, -5}, {9, 0, -3}, {7, 0, -2}, {4, 0, -1}, {2, 1, 0}, {5, 0, 2}, {6, 0, 3}, {3, 4, 4}, {6, 1, 20}, {4, 4, 22}, {4, 5, 38}, {5, 6, 70}, {5, 7, 134}, {6, 7, 262}, {7, 8, 390}, {6, 10, 646}, {9, 32, -16}, {9, 32, 1670}, {2, -1, 0}}},
	{true, []TableLine{{8, 4, -31}, {9, 2, -15}, {8, 2, -11}, {9, 1, -7}, {7, 1, -5}, {4, 1, -3}, {3, 1, -1}, {3, 1, 1}, {5, 1, 3}, {6, 1, 5}, {3, 5, 7}, {6, 2, 39}, {4, 5, 43}, {4, 6, 75}, {5, 7, 139}, {5, 8, 267}, {6, 8, 523}, {7, 9, 779}, {6, 11, 1291}, {9, 32, -32}, {9, 32, 3339}, {2, -1, 0}}},
	{true, []TableLine{{7, 4, -21}, {8, 0, -5}, {7, 0, -4}, {5, 0, -3}, {2, 2, -2}, {5, 0, 2}, {6, 0, 3}, {7, 0, 4}, {8, 0, 5}, {2, 6, 6}, {5, 5, 70}, {6, 5, 102}, {6, 6, 134}, {6, 7, 198}, {6, 8, 326}, {6, 9, 582}, {6, 10, 1094}, {7, 11, 2118}, {8, 32, -22}, {8, 32, 4166}, {2, -1, 0}}},
	{false, []TableLine{{1, 0, 1}, {2, 1, 2}, {4, 0, 4}, {4, 1, 5}, {5, 1, 7}, {5, 2, 9}, {6, 2, 13}, {7, 2, 17}, {7, 3, 21}, {7, 4, 29}, {7, 5, 45}, {7, 6, 77}, {7, 32, 141}}},
	{false, []TableLine{{1, 0, 1}, {2, 0, 2}, {3, 1, 3}, {5, 0, 5}, {5, 1, 6}, {6, 1, 8}, {7, 0, 10}, {7, 1, 11}, {7, 2, 13}, {7, 3, 17}, {7, 4, 25}, {8, 5, 41}, {8, 32, 73}}},
	{false, []TableLine{{1, 0, 1}, {3, 0, 2}, {4, 0, 3}, {5, 0, 4}, {4, 1, 5}, {3, 3, 7}, {6, 1, 15}, {6, 2, 17}, {6, 3, 21}, {6, 4, 29}, {6, 5, 45}, {7, 6, 77}, {7, 32, 141}}},
	{false, []TableLine{{3, 0, -2}, {3, 0, -1}, {1, 0, 0}, {3, 0, 1}, {3, 0, 2}}},
	{false, []TableLine{{7, 4, -24}, {6, 2, -8}, {5, 1, -4}, {4, 0, -2}, {3, 0, -1}, {1, 0, 0}, {3, 0, 1}, {4, 0, 2}, {5, 1, 3}, {6, 2, 5}, {7, 4, 9}, {7, 32, -25}, {7, 32, 25}}},
}
