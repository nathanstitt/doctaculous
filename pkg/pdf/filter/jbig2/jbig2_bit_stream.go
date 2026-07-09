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

// BitStream 位流
type BitStream struct {
	data         []byte
	byteIdx      uint32
	bitIdx       uint32
	key          uint64
	littleEndian bool
}

// NewBitStream 创建位流
// 入参: data 数据源, key 键值
// 返回: *BitStream 位流对象
func NewBitStream(data []byte, key uint64) *BitStream {
	if len(data) > 256*1024*1024 {
		data = nil
	}
	return &BitStream{data: data, key: key}
}

// SetLittleEndian 设置小端序
// 入参: le 是否小端序
func (bs *BitStream) SetLittleEndian(le bool) {
	bs.littleEndian = le
}

// readUint32 读取4字节整数
// 入参: data 数据, littleEndian 是否小端序
// 返回: uint32 结果
func readUint32(data []byte, littleEndian bool) uint32 {
	if littleEndian {
		return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
	}
	return uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3])
}

// readUint16 读取2字节整数
// 入参: data 数据, littleEndian 是否小端序
// 返回: uint16 结果
func readUint16(data []byte, littleEndian bool) uint16 {
	if littleEndian {
		return uint16(data[0]) | uint16(data[1])<<8
	}
	return uint16(data[0])<<8 | uint16(data[1])
}

// ReadNBits 读取指定位数的整数
// 入参: bits 位数
// 返回: uint32 结果, error 错误信息
func (b *BitStream) ReadNBits(bits uint32) (uint32, error) {
	if bits > 32 {
		return 0, errors.New("too many bits requested")
	}
	if !b.IsInBounds() {
		return 0, errors.New("out of bounds")
	}
	bitPos := b.GetBitPos()
	lengthInBits := b.lengthInBits()
	if bitPos > lengthInBits {
		return 0, errors.New("bit position out of range")
	}
	var bitsToRead uint32
	if bitPos+bits <= lengthInBits {
		bitsToRead = bits
	} else {
		bitsToRead = lengthInBits - bitPos
	}
	var result uint32
	for i := uint32(0); i < bitsToRead; i++ {
		result = (result << 1) | uint32((b.data[b.byteIdx]>>(7-b.bitIdx))&0x01)
		b.advanceBit()
	}
	return result, nil
}

// ReadNBitsInt32 读取指定位数的有符号整数
// 入参: bits 位数
// 返回: int32 结果, error 错误信息
func (b *BitStream) ReadNBitsInt32(bits uint32) (int32, error) {
	val, err := b.ReadNBits(bits)
	return int32(val), err
}

// Read1Bit 读取1位
// 返回: uint32 结果, error 错误信息
func (b *BitStream) Read1Bit() (uint32, error) {
	if !b.IsInBounds() {
		return 0, errors.New("out of bounds")
	}
	result := uint32((b.data[b.byteIdx] >> (7 - b.bitIdx)) & 0x01)
	b.advanceBit()
	return result, nil
}

// Read1BitBool 读取1位布尔值
// 返回: bool 结果, error 错误信息
func (b *BitStream) Read1BitBool() (bool, error) {
	val, err := b.Read1Bit()
	return val != 0, err
}

// Read1Byte 读取1字节
// 返回: uint8 结果, error 错误信息
func (b *BitStream) Read1Byte() (uint8, error) {
	if !b.IsInBounds() {
		return 0, errors.New("out of bounds")
	}
	result := b.data[b.byteIdx]
	b.byteIdx++
	return result, nil
}

// ReadInteger 读取4字节整数
// 返回: uint32 结果, error 错误信息
func (b *BitStream) ReadInteger() (uint32, error) {
	if uint64(b.byteIdx)+3 >= uint64(len(b.data)) {
		return 0, errors.New("insufficient data")
	}
	result := readUint32(b.data[b.byteIdx:], b.littleEndian)
	b.byteIdx += 4
	return result, nil
}

// ReadShortInteger 读取2字节整数
// 返回: uint16 结果, error 错误信息
func (b *BitStream) ReadShortInteger() (uint16, error) {
	if uint64(b.byteIdx)+1 >= uint64(len(b.data)) {
		return 0, errors.New("insufficient data")
	}
	result := readUint16(b.data[b.byteIdx:], b.littleEndian)
	b.byteIdx += 2
	return result, nil
}

// AlignByte 字节对齐
func (b *BitStream) AlignByte() {
	if b.bitIdx != 0 {
		b.AddOffset(1)
		b.bitIdx = 0
	}
}

// GetCurByte 获取当前字节
// 返回: uint8 当前字节
func (b *BitStream) GetCurByte() uint8 {
	if b.IsInBounds() {
		return b.data[b.byteIdx]
	}
	return 0
}

// IncByteIdx 增加字节索引
func (b *BitStream) IncByteIdx() {
	b.AddOffset(1)
}

// GetCurByteArith 获取算术解码当前字节
// 返回: uint8 当前字节
func (b *BitStream) GetCurByteArith() uint8 {
	if b.IsInBounds() {
		return b.data[b.byteIdx]
	}
	return 0xFF
}

// GetNextByteArith 获取算术解码下一字节
// 返回: uint8 下一字节
func (b *BitStream) GetNextByteArith() uint8 {
	if uint64(b.byteIdx)+1 < uint64(len(b.data)) {
		return b.data[b.byteIdx+1]
	}
	return 0xFF
}

// GetOffset 获取当前偏移量
// 返回: uint32 偏移量
func (b *BitStream) GetOffset() uint32 {
	return b.byteIdx
}

// SetOffset 设置偏移量
// 入参: offset 偏移量
func (b *BitStream) SetOffset(offset uint32) {
	size := uint32(len(b.data))
	if offset > size {
		b.byteIdx = size
	} else {
		b.byteIdx = offset
	}
	b.bitIdx = 0
}

// AddOffset 增加偏移量
// 入参: delta 增量
func (b *BitStream) AddOffset(delta uint32) {
	newOffset := uint64(b.byteIdx) + uint64(delta)
	if newOffset <= uint64(len(b.data)) {
		b.SetOffset(uint32(newOffset))
	} else {
		b.SetOffset(uint32(len(b.data)))
	}
}

// GetBitPos 获取当前位位置
// 返回: uint32 位位置
func (b *BitStream) GetBitPos() uint32 {
	return (b.byteIdx << 3) + b.bitIdx
}

// SetBitPos 设置位位置
// 入参: bitPos 位位置
func (b *BitStream) SetBitPos(bitPos uint32) {
	b.byteIdx = bitPos >> 3
	b.bitIdx = bitPos & 7
}

// GetByteLeft 获取剩余字节数
// 返回: uint32 剩余字节数
func (b *BitStream) GetByteLeft() uint32 {
	if b.byteIdx >= uint32(len(b.data)) {
		return 0
	}
	return uint32(len(b.data)) - b.byteIdx
}

// GetLength 获取总字节数
// 返回: uint32 总字节数
func (b *BitStream) GetLength() uint32 {
	return uint32(len(b.data))
}

// GetPointer 获取数据指针
// 返回: []byte 数据切片
func (b *BitStream) GetPointer() []byte {
	if b.byteIdx >= uint32(len(b.data)) {
		return nil
	}
	return b.data[b.byteIdx:]
}

// GetKey 获取键值
// 返回: uint64 键值
func (b *BitStream) GetKey() uint64 {
	return b.key
}

// IsInBounds 检查是否在边界内
// 返回: bool 是否在边界内
func (b *BitStream) IsInBounds() bool {
	return b.byteIdx < uint32(len(b.data))
}

// advanceBit 前进一位
func (b *BitStream) advanceBit() {
	if b.bitIdx == 7 {
		b.byteIdx++
		b.bitIdx = 0
	} else {
		b.bitIdx++
	}
}

// lengthInBits 获取总位数
// 返回: uint32 总位数
func (b *BitStream) lengthInBits() uint32 {
	return uint32(len(b.data)) * 8
}
