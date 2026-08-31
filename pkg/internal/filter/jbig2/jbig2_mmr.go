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
	"bytes"
	"errors"
	"io"

	"golang.org/x/image/ccitt"
)

// DecodeG4 使用 CCITT Group 4 解码位流到图像
// 入参: stream 位流, image 目标图像
// 返回: error 错误信息
func DecodeG4(stream *BitStream, image *Image) error {
	stream.AlignByte()
	data := stream.GetPointer()
	if data == nil {
		return errors.New("insufficient data for g4 decode")
	}
	reader := bytes.NewReader(data)
	opts := &ccitt.Options{
		Invert: false,
	}
	decoder := ccitt.NewReader(reader, ccitt.MSB, ccitt.Group4, int(image.Width()), int(image.Height()), opts)
	width := int(image.Width())
	height := int(image.Height())
	rowBytes := (width + 7) / 8
	buf := make([]byte, rowBytes)
	imgData := image.Data()
	stride := int(image.Stride())
	for y := 0; y < height; y++ {
		if _, err := io.ReadFull(decoder, buf); err != nil {
			return err
		}
		start := y * stride
		if start+rowBytes > len(imgData) {
			return errors.New("image buffer too small")
		}
		copy(imgData[start:start+rowBytes], buf)
	}
	consumed := int64(len(data)) - int64(reader.Len())
	stream.AddOffset(uint32(consumed))
	return nil
}
