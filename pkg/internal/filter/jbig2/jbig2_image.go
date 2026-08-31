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

// Image 图像结构体
type Image struct {
	width  int32
	height int32
	stride int32
	data   []byte
}

// NewImage 创建新图像
// 入参: width 宽度, height 高度
// 返回: *Image 图像对象
func NewImage(width, height int32) *Image {
	if width <= 0 || height <= 0 {
		return nil
	}
	stride := (width + 7) / 8
	if stride <= 0 || height > 2147483647/stride {
		return nil
	}
	size := stride * height
	data := make([]byte, size)
	return &Image{
		width:  width,
		height: height,
		stride: stride,
		data:   data,
	}
}

// Width 获取宽度
// 返回: int32 宽度
func (i *Image) Width() int32 {
	return i.width
}

// Height 获取高度
// 返回: int32 高度
func (i *Image) Height() int32 {
	return i.height
}

// Stride 获取跨度
// 返回: int32 跨度
func (i *Image) Stride() int32 {
	return i.stride
}

// Data 获取数据
// 返回: []byte 数据切片
func (i *Image) Data() []byte {
	return i.data
}

// GetPixel 获取像素值
// 入参: x 轴坐标, y 轴坐标
// 返回: int 像素值
func (i *Image) GetPixel(x, y int32) int {
	if x < 0 || x >= i.width || y < 0 || y >= i.height {
		return 0
	}
	byteIdx := y*i.stride + (x >> 3)
	bitIdx := 7 - (x & 7)
	return int((i.data[byteIdx] >> bitIdx) & 1)
}

// SetPixel 设置像素值
// 入参: x 轴坐标, y 轴坐标, v 像素值
func (i *Image) SetPixel(x, y int32, v int) {
	if x < 0 || x >= i.width || y < 0 || y >= i.height {
		return
	}
	byteIdx := y*i.stride + (x >> 3)
	bitIdx := 7 - (x & 7)
	mask := byte(1 << bitIdx)
	if v != 0 {
		i.data[byteIdx] |= mask
	} else {
		i.data[byteIdx] &^= mask
	}
}

// Fill 填充图像
// 入参: v 填充值
func (i *Image) Fill(v bool) {
	var val byte
	if v {
		val = 0xFF
	}
	for idx := range i.data {
		i.data[idx] = val
	}
}

// Invert 反转图像像素
func (i *Image) Invert() {
	for idx := range i.data {
		i.data[idx] = ^i.data[idx]
	}
}

// ComposeTo 将当前图像组合到目标图像
// 入参: dst 目标图像, x 轴坐标, y 轴坐标, op 组合操作
func (i *Image) ComposeTo(dst *Image, x, y int32, op ComposeOp) {
	if i == nil || dst == nil {
		return
	}
	srcX0 := int64(0)
	srcY0 := int64(0)
	srcX1 := int64(i.width)
	srcY1 := int64(i.height)
	if x < 0 {
		srcX0 = -int64(x)
	}
	if y < 0 {
		srcY0 = -int64(y)
	}
	if right := int64(x) + srcX1; right > int64(dst.width) {
		srcX1 = int64(dst.width) - int64(x)
	}
	if bottom := int64(y) + srcY1; bottom > int64(dst.height) {
		srcY1 = int64(dst.height) - int64(y)
	}
	if srcX0 >= srcX1 || srcY0 >= srcY1 {
		return
	}
	for h := int32(srcY0); h < int32(srcY1); h++ {
		for w := int32(srcX0); w < int32(srcX1); w++ {
			dstX := x + w
			dstY := y + h
			srcBit := i.GetPixel(w, h)
			dstBit := dst.GetPixel(dstX, dstY)
			var resBit int
			switch op {
			case ComposeOr:
				resBit = dstBit | srcBit
			case ComposeAnd:
				resBit = dstBit & srcBit
			case ComposeXor:
				resBit = dstBit ^ srcBit
			case ComposeXnor:
				if dstBit == srcBit {
					resBit = 1
				} else {
					resBit = 0
				}
			case ComposeReplace:
				resBit = srcBit
			default:
				resBit = dstBit
			}
			dst.SetPixel(dstX, dstY, resBit)
		}
	}
}

// ComposeFrom 从源图像组合到当前图像
// 入参: x 轴坐标, y 轴坐标, src 源图像, op 组合操作
func (i *Image) ComposeFrom(x, y int32, src *Image, op ComposeOp) {
	if src != nil {
		src.ComposeTo(i, x, y, op)
	}
}

// SubImage 获取子图像
// 入参: x 轴坐标, y 轴坐标, w 宽度, h 高度
// 返回: *Image 子图像对象
func (i *Image) SubImage(x, y, w, h int32) *Image {
	if w <= 0 || h <= 0 {
		return nil
	}
	sub := NewImage(w, h)
	if sub == nil {
		return nil
	}
	sub.Fill(false)
	for r := int32(0); r < h; r++ {
		for c := int32(0); c < w; c++ {
			sub.SetPixel(c, r, i.GetPixel(x+c, y+r))
		}
	}
	return sub
}

// Expand 扩展图像高度
// 入参: height 新高度, defaultPixel 默认填充值
func (i *Image) Expand(height int32, defaultPixel bool) {
	if height <= i.height {
		return
	}
	newStride := i.stride
	newHeight := height
	newData := make([]byte, newStride*newHeight)
	copy(newData, i.data)
	start := i.stride * i.height
	fill := byte(0x00)
	if defaultPixel {
		fill = 0xFF
	}
	for j := start; j < int32(len(newData)); j++ {
		newData[j] = fill
	}
	i.data = newData
	i.height = newHeight
}

// Duplicate 复制图像
// 返回: *Image 新图像
func (i *Image) Duplicate() *Image {
	if i == nil {
		return nil
	}
	newImg := NewImage(i.width, i.height)
	if newImg != nil {
		copy(newImg.data, i.data)
	}
	return newImg
}

// CopyLine 复制行
// 入参: h 目标行号, srcH 源行号
func (i *Image) CopyLine(h, srcH int32) {
	if h < 0 || h >= i.height || srcH < 0 || srcH >= i.height {
		return
	}
	start := h * i.stride
	end := start + i.stride
	srcStart := srcH * i.stride
	srcEnd := srcStart + i.stride
	copy(i.data[start:end], i.data[srcStart:srcEnd])
}
