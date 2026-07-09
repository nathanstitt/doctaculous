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

const (
	mmrPass    = 0
	mmrHoriz   = 1
	mmrV0      = 2
	mmrVR1     = 3
	mmrVR2     = 4
	mmrVR3     = 5
	mmrVL1     = 6
	mmrVL2     = 7
	mmrVL3     = 8
	mmrExt2D   = 9
	mmrExt1D   = 10
	mmrEOL     = -1
	mmrEOF     = -3
	mmrInvalid = -2
)

// mmrCode MMR 编码字
type mmrCode struct {
	bitLength int
	codeWord  int
	runLength int
	subTable  []*mmrCode
}

var (
	modeCodes = [][]int{
		{4, 0x1, mmrPass},
		{3, 0x1, mmrHoriz},
		{1, 0x1, mmrV0},
		{3, 0x3, mmrVR1},
		{6, 0x3, mmrVR2},
		{7, 0x3, mmrVR3},
		{3, 0x2, mmrVL1},
		{6, 0x2, mmrVL2},
		{7, 0x2, mmrVL3},
		{10, 0xf, mmrExt2D},
		{12, 0xf, mmrExt1D},
		{12, 0x1, mmrEOL},
	}
	whiteCodes = [][]int{
		{4, 0x07, 2}, {4, 0x08, 3}, {4, 0x0B, 4}, {4, 0x0C, 5}, {4, 0x0E, 6}, {4, 0x0F, 7},
		{5, 0x12, 128}, {5, 0x13, 8}, {5, 0x14, 9}, {5, 0x1B, 64}, {5, 0x07, 10}, {5, 0x08, 11},
		{6, 0x17, 192}, {6, 0x18, 1664}, {6, 0x2A, 16}, {6, 0x2B, 17}, {6, 0x03, 13}, {6, 0x34, 14},
		{6, 0x35, 15}, {6, 0x07, 1}, {6, 0x08, 12}, {7, 0x13, 26}, {7, 0x17, 21}, {7, 0x18, 28},
		{7, 0x24, 27}, {7, 0x27, 18}, {7, 0x28, 24}, {7, 0x2B, 25}, {7, 0x03, 22}, {7, 0x37, 256},
		{7, 0x04, 23}, {7, 0x08, 20}, {7, 0xC, 19}, {8, 0x12, 33}, {8, 0x13, 34}, {8, 0x14, 35},
		{8, 0x15, 36}, {8, 0x16, 37}, {8, 0x17, 38}, {8, 0x1A, 31}, {8, 0x1B, 32}, {8, 0x02, 29},
		{8, 0x24, 53}, {8, 0x25, 54}, {8, 0x28, 39}, {8, 0x29, 40}, {8, 0x2A, 41}, {8, 0x2B, 42},
		{8, 0x2C, 43}, {8, 0x2D, 44}, {8, 0x03, 30}, {8, 0x32, 61}, {8, 0x33, 62}, {8, 0x34, 63},
		{8, 0x35, 0}, {8, 0x36, 320}, {8, 0x37, 384}, {8, 0x04, 45}, {8, 0x4A, 59}, {8, 0x4B, 60},
		{8, 0x5, 46}, {8, 0x52, 49}, {8, 0x53, 50}, {8, 0x54, 51}, {8, 0x55, 52}, {8, 0x58, 55},
		{8, 0x59, 56}, {8, 0x5A, 57}, {8, 0x5B, 58}, {8, 0x64, 448}, {8, 0x65, 512}, {8, 0x67, 640},
		{8, 0x68, 576}, {8, 0x0A, 47}, {8, 0x0B, 48}, {9, 0x98, 1472}, {9, 0x99, 1536},
		{9, 0x9A, 1600}, {9, 0x9B, 1728}, {9, 0xCC, 704}, {9, 0xCD, 768}, {9, 0xD2, 832},
		{9, 0xD3, 896}, {9, 0xD4, 960}, {9, 0xD5, 1024}, {9, 0xD6, 1088}, {9, 0xD7, 1152},
		{9, 0xD8, 1216}, {9, 0xD9, 1280}, {9, 0xDA, 1344}, {9, 0xDB, 1408}, {11, 0x08, 1792},
		{11, 0x0C, 1856}, {11, 0x0D, 1920}, {12, 0x00, mmrEOF}, {12, 0x01, mmrEOL},
		{12, 0x12, 1984}, {12, 0x13, 2048}, {12, 0x14, 2112}, {12, 0x15, 2176}, {12, 0x16, 2240},
		{12, 0x17, 2304}, {12, 0x1C, 2368}, {12, 0x1D, 2432}, {12, 0x1E, 2496}, {12, 0x1F, 2560},
	}
	blackCodes = [][]int{
		{2, 0x02, 3}, {2, 0x03, 2}, {3, 0x02, 1}, {3, 0x03, 4}, {4, 0x02, 6}, {4, 0x03, 5},
		{5, 0x03, 7}, {6, 0x04, 9}, {6, 0x05, 8}, {7, 0x04, 10}, {7, 0x05, 11}, {7, 0x07, 12},
		{8, 0x04, 13}, {8, 0x07, 14}, {9, 0x18, 15}, {10, 0x17, 16}, {10, 0x18, 17}, {10, 0x37, 0},
		{10, 0x08, 18}, {10, 0x0F, 64}, {11, 0x17, 24}, {11, 0x18, 25}, {11, 0x28, 23},
		{11, 0x37, 22}, {11, 0x67, 19}, {11, 0x68, 20}, {11, 0x6C, 21}, {11, 0x08, 1792},
		{11, 0x0C, 1856}, {11, 0x0D, 1920}, {12, 0x00, mmrEOF}, {12, 0x01, mmrEOL},
		{12, 0x12, 1984}, {12, 0x13, 2048}, {12, 0x14, 2112}, {12, 0x15, 2176}, {12, 0x16, 2240},
		{12, 0x17, 2304}, {12, 0x1C, 2368}, {12, 0x1D, 2432}, {12, 0x1E, 2496}, {12, 0x1F, 2560},
		{12, 0x24, 52}, {12, 0x27, 55}, {12, 0x28, 56}, {12, 0x2B, 59}, {12, 0x2C, 60},
		{12, 0x33, 320}, {12, 0x34, 384}, {12, 0x35, 448}, {12, 0x37, 53}, {12, 0x38, 54},
		{12, 0x52, 50}, {12, 0x53, 51}, {12, 0x54, 44}, {12, 0x55, 45}, {12, 0x56, 46},
		{12, 0x57, 47}, {12, 0x58, 57}, {12, 0x59, 58}, {12, 0x5A, 61}, {12, 0x5B, 256},
		{12, 0x64, 48}, {12, 0x65, 49}, {12, 0x66, 62}, {12, 0x67, 63}, {12, 0x68, 30},
		{12, 0x69, 31}, {12, 0x6A, 32}, {12, 0x6B, 33}, {12, 0x6C, 40}, {12, 0x6D, 41},
		{12, 0xC8, 128}, {12, 0xC9, 192}, {12, 0xCA, 26}, {12, 0xCB, 27}, {12, 0xCC, 28},
		{12, 0xCD, 29}, {12, 0xD2, 34}, {12, 0xD3, 35}, {12, 0xD4, 36}, {12, 0xD5, 37},
		{12, 0xD6, 38}, {12, 0xD7, 39}, {12, 0xDA, 42}, {12, 0xDB, 43}, {13, 0x4A, 640},
		{13, 0x4B, 704}, {13, 0x4C, 768}, {13, 0x4D, 832}, {13, 0x52, 1280}, {13, 0x53, 1344},
		{13, 0x54, 1408}, {13, 0x55, 1472}, {13, 0x5A, 1536}, {13, 0x5B, 1600}, {13, 0x64, 1664},
		{13, 0x65, 1728}, {13, 0x6C, 512}, {13, 0x6D, 576}, {13, 0x72, 896}, {13, 0x73, 960},
		{13, 0x74, 1024}, {13, 0x75, 1088}, {13, 0x76, 1152}, {13, 0x77, 1216},
	}
	whiteTable []*mmrCode
	blackTable []*mmrCode
	modeTable  []*mmrCode
)

const (
	firstLevelTableSize  = 8
	firstLevelTableMask  = (1 << firstLevelTableSize) - 1
	secondLevelTableSize = 5
	secondLevelTableMask = (1 << secondLevelTableSize) - 1
	codeOffset           = 24
)

func init() {
	whiteTable = createLittleEndianTable(whiteCodes)
	blackTable = createLittleEndianTable(blackCodes)
	modeTable = createLittleEndianTable(modeCodes)
}

// createLittleEndianTable 创建小端序解码表
// 入参: codes 编码集
// 返回: []*mmrCode 解码表
func createLittleEndianTable(codes [][]int) []*mmrCode {
	table := make([]*mmrCode, firstLevelTableMask+1)
	for _, c := range codes {
		code := &mmrCode{bitLength: c[0], codeWord: c[1], runLength: c[2]}
		if code.bitLength <= firstLevelTableSize {
			variantLength := firstLevelTableSize - code.bitLength
			baseWord := code.codeWord << variantLength
			for variant := (1 << variantLength) - 1; variant >= 0; variant-- {
				index := baseWord | variant
				table[index] = code
			}
		} else {
			firstLevelIndex := code.codeWord >> uint(code.bitLength-firstLevelTableSize)
			if table[firstLevelIndex] == nil {
				table[firstLevelIndex] = &mmrCode{subTable: make([]*mmrCode, secondLevelTableMask+1)}
			}
			if code.bitLength <= firstLevelTableSize+secondLevelTableSize {
				subTable := table[firstLevelIndex].subTable
				variantLength := firstLevelTableSize + secondLevelTableSize - code.bitLength
				baseWord := (code.codeWord << uint(variantLength)) & secondLevelTableMask
				for variant := (1 << variantLength) - 1; variant >= 0; variant-- {
					subTable[baseWord|variant] = code
				}
			}
		}
	}
	return table
}

// MMRDecompressor MMR 解码器
type MMRDecompressor struct {
	width      int
	height     int
	stream     *BitStream
	lastCode   int
	lastOffset int
}

// NewMMRDecompressor 创建新的 MMR 解码器
// 入参: width 宽度, height 高度, stream 位流
// 返回: *MMRDecompressor 解码器对象
func NewMMRDecompressor(width, height int, stream *BitStream) *MMRDecompressor {
	return &MMRDecompressor{
		width:      width,
		height:     height,
		stream:     stream,
		lastOffset: -1,
	}
}

// getNextCode 获取下一个编码
// 入参: table 解码表
// 返回: *mmrCode 编码对象, error 错误信息
func (m *MMRDecompressor) getNextCode(table []*mmrCode) (*mmrCode, error) {
	codeWord, err := m.getNextCodeWord()
	if err != nil {
		return nil, err
	}
	idx := (codeWord >> (codeOffset - firstLevelTableSize)) & firstLevelTableMask
	res := table[idx]
	if res != nil && res.subTable != nil {
		idx2 := (codeWord >> (codeOffset - firstLevelTableSize - secondLevelTableSize)) & secondLevelTableMask
		res = res.subTable[idx2]
	}
	return res, nil
}

// getNextCodeWord 获取下一个编码字
// 返回: int 编码字, error 错误信息
func (m *MMRDecompressor) getNextCodeWord() (int, error) {
	offset := int(m.stream.GetBitPos())
	if offset != m.lastOffset {
		savedBitPos := m.stream.GetBitPos()
		val, err := m.stream.ReadNBits(24)
		if err != nil {
			return 0, err
		}
		m.stream.SetBitPos(savedBitPos)
		m.lastCode = int(val)
		m.lastOffset = offset
	}
	return m.lastCode, nil
}

// Uncompress 解压缩图像
// 返回: *Image 图像对象, error 错误信息
func (m *MMRDecompressor) Uncompress() (*Image, error) {
	img := NewImage(int32(m.width), int32(m.height))
	if img == nil {
		return nil, errors.New("failed to create image")
	}
	img.Fill(false)
	currOffsets := make([]int, m.width+5)
	refOffsets := make([]int, m.width+5)
	refOffsets[0] = m.width
	refRunLength := 1
	for y := 0; y < m.height; y++ {
		count, err := m.uncompress2D(refOffsets, refRunLength, currOffsets)
		if err != nil {
			return nil, err
		}
		if count == mmrEOF {
			break
		}
		if count > 0 {
			m.fillBitmap(img, y, currOffsets, count)
		}
		copy(refOffsets, currOffsets)
		refRunLength = count
	}
	m.detectAndSkipEOL()
	m.stream.AlignByte()
	return img, nil
}

// uncompress2D 2D 解压缩一行
// 入参: refOffsets 参考行偏移, refRunLength 参考行游程长度, currOffsets 当前行偏移
// 返回: int 偏移计数, error 错误信息
func (m *MMRDecompressor) uncompress2D(refOffsets []int, refRunLength int, currOffsets []int) (int, error) {
	refIdx := 0
	currIdx := 0
	bitPos := 0
	whiteRun := true
	refOffsets[refRunLength] = m.width
	refOffsets[refRunLength+1] = m.width
	refOffsets[refRunLength+2] = m.width + 1
	refOffsets[refRunLength+3] = m.width + 1
	for bitPos < m.width {
		code, err := m.getNextCode(modeTable)
		if err != nil {
			return 0, err
		}
		if code == nil {
			break
		}
		m.stream.SetBitPos(m.stream.GetBitPos() + uint32(code.bitLength))
		switch code.runLength {
		case mmrPass:
			refIdx++
			bitPos = refOffsets[refIdx]
			refIdx++
			continue
		case mmrHoriz:
			for i := 0; i < 2; i++ {
				var table []*mmrCode
				if (i == 0 && whiteRun) || (i == 1 && !whiteRun) {
					table = whiteTable
				} else {
					table = blackTable
				}
				run := 0
				for {
					c, err := m.getNextCode(table)
					if err != nil {
						return 0, err
					}
					if c == nil {
						return 0, errors.New("invalid code in horiz run")
					}
					m.stream.SetBitPos(m.stream.GetBitPos() + uint32(c.bitLength))
					if c.runLength < 0 {
						return 0, errors.New("mmr error in horiz run")
					}
					run += c.runLength
					if c.runLength < 64 {
						break
					}
				}
				bitPos += run
				currOffsets[currIdx] = bitPos
				currIdx++
			}
			for bitPos < m.width && refOffsets[refIdx] <= bitPos {
				refIdx += 2
			}
			continue
		case mmrV0:
			bitPos = refOffsets[refIdx]
		case mmrVR1:
			bitPos = refOffsets[refIdx] + 1
		case mmrVR2:
			bitPos = refOffsets[refIdx] + 2
		case mmrVR3:
			bitPos = refOffsets[refIdx] + 3
		case mmrVL1:
			bitPos = refOffsets[refIdx] - 1
		case mmrVL2:
			bitPos = refOffsets[refIdx] - 2
		case mmrVL3:
			bitPos = refOffsets[refIdx] - 3
		default:
			return 0, errors.New("unsupported mmr mode")
		}
		if bitPos <= m.width {
			currOffsets[currIdx] = bitPos
			currIdx++
			whiteRun = !whiteRun
			if refIdx > 0 {
				refIdx--
			} else {
				refIdx++
			}
			for bitPos < m.width && refOffsets[refIdx] <= bitPos {
				refIdx += 2
			}
		}
	}
	if currIdx == 0 || currOffsets[currIdx-1] != m.width {
		currOffsets[currIdx] = m.width
		currIdx++
	}
	return currIdx, nil
}

// fillBitmap 填充图像位图
// 入参: img 图像对象, y 轴坐标, offsets 偏移集合, count 计数
func (m *MMRDecompressor) fillBitmap(img *Image, y int, offsets []int, count int) {
	x := 0
	for i := 0; i < count; i++ {
		target := offsets[i]
		val := byte(0)
		if i%2 != 0 {
			val = 1
		}
		for x < target && x < m.width {
			img.SetPixel(int32(x), int32(y), int(val))
			x++
		}
	}
}

// detectAndSkipEOL 检测并跳过 EOL
func (m *MMRDecompressor) detectAndSkipEOL() {
	for {
		code, err := m.getNextCode(modeTable)
		if err != nil {
			break
		}
		if code != nil && code.runLength == mmrEOL {
			m.stream.SetBitPos(m.stream.GetBitPos() + uint32(code.bitLength))
		} else {
			break
		}
	}
}
