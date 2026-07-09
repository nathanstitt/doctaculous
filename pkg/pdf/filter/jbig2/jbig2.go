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

// Package jbig2 一个高性能、零依赖的纯 Go 语言 JBIG2 解码器
package jbig2

import (
	"bytes"
	"compress/zlib"
	"errors"
	"image"
	"image/color"
	"io"
)

// jbig2Signature JBIG2文件签名
var jbig2Signature = []byte{0x97, 0x4A, 0x42, 0x32, 0x0D, 0x0A, 0x1A, 0x0A}

// Decoder JBIG2解码器
type Decoder struct {
	doc       *Document
	pageIndex uint32
}

// NewDecoder 创建解码器
// 入参: r 读取器
// 返回: *Decoder 解码器, error 错误信息
func NewDecoder(r io.Reader) (*Decoder, error) {
	data, err := readDecoderData(r)
	if err != nil {
		return nil, err
	}
	data, randomAccess, littleEndian, orgMode, grouped := probeConfigs(data)
	if data == nil {
		return nil, errors.New("no valid jbig2 configuration found")
	}
	doc := NewDocument(data, nil, randomAccess, littleEndian)
	doc.OrgMode = orgMode
	doc.Grouped = grouped
	return &Decoder{doc: doc, pageIndex: 0}, nil
}

// NewDecoderWithGlobals 创建带全局段的解码器
// 入参: r 读取器, globals 全局段数据
// 返回: *Decoder 解码器, error 错误信息
func NewDecoderWithGlobals(r io.Reader, globals []byte) (*Decoder, error) {
	data, err := readDecoderData(r)
	if err != nil {
		return nil, err
	}
	probedData, randomAccess, littleEndian, orgMode, grouped := probeConfigs(data)
	if probedData == nil {
		if len(globals) > 0 {
			probedData = data
			randomAccess = false
			littleEndian = false
			orgMode = 0
			grouped = false
			if len(data) >= 4 {
				if data[0] != 0 && data[1] == 0 && data[2] == 0 && data[3] == 0 {
					littleEndian = true
				}
			}
		} else {
			return nil, errors.New("no valid jbig2 configuration found")
		}
	} else {
		data = probedData
	}
	doc := NewDocument(data, globals, randomAccess, littleEndian)
	doc.OrgMode = orgMode
	doc.Grouped = grouped
	if err := doc.parseGlobalSegments(); err != nil {
		return nil, err
	}
	return &Decoder{doc: doc, pageIndex: 0}, nil
}

// parseGlobalSegments 解析全局段
// 返回: error 错误信息
func (d *Document) parseGlobalSegments() error {
	if d == nil || d.globalContext == nil {
		return nil
	}
	for {
		res := d.globalContext.DecodeSequential()
		if res == ResultEndReached {
			return nil
		}
		if res == ResultFailure {
			return errors.New("failed to parse global segments")
		}
	}
}

// readDecoderData 读取解码数据
// 入参: r 读取器
// 返回: []byte 数据, error 错误信息
func readDecoderData(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return normalizeDecoderData(data)
}

// normalizeDecoderData 规范化解码数据
// 入参: data 数据
// 返回: []byte 数据, error 错误信息
func normalizeDecoderData(data []byte) ([]byte, error) {
	if len(data) <= 8 || data[0] != 'C' || data[1] != 'W' || data[2] != 'S' {
		return data, nil
	}
	zr, err := zlib.NewReader(bytes.NewReader(data[8:]))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	decompressed, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	data = skipSWFHeader(decompressed)
	if idx := bytes.Index(data, jbig2Signature); idx != -1 {
		data = data[idx:]
	}
	return data, nil
}

// skipSWFHeader 跳过SWF头和标签
// 入参: data 数据
// 返回: []byte 数据
func skipSWFHeader(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	nbits := int(data[0] >> 3)
	rectBits := 5 + nbits*4
	rectBytes := (rectBits + 7) / 8
	startOffset := rectBytes + 4
	if len(data) <= startOffset {
		return data
	}
	data = data[startOffset:]
	for len(data) >= 2 {
		tagCodeAndLen := int(readUint16(data, true))
		tagCode := tagCodeAndLen >> 6
		tagLen := tagCodeAndLen & 0x3F
		headerLen := 2
		if tagLen == 0x3F {
			if len(data) < 6 {
				break
			}
			tagLen = int(readUint32(data[2:], true))
			headerLen = 6
		}
		if tagCode == 0 {
			break
		}
		if tagCode == 6 || tagCode == 21 || tagCode == 35 || tagCode == 90 {
			skipBytes := 2
			if tagCode == 35 || tagCode == 90 {
				skipBytes = 6
			}
			payloadOffset := headerLen + skipBytes
			if len(data) > payloadOffset {
				return data[payloadOffset:]
			}
		}
		nextOffset := headerLen + tagLen
		if len(data) < nextOffset {
			break
		}
		data = data[nextOffset:]
	}
	return data
}

// Decode 解码下一页
// 返回: image.Image 图像, error 错误信息
func (d *Decoder) Decode() (image.Image, error) {
	if d.doc == nil {
		return nil, errors.New("decoder not initialized")
	}
	for {
		res := d.doc.DecodeSequential()
		if res == ResultEndReached {
			if d.doc.inPage && d.doc.page != nil {
				d.doc.inPage = false
				d.pageIndex++
				img := d.doc.page.ToGoImage()
				if !d.doc.Grouped {
					d.doc.ReleasePageSegments(d.pageIndex)
				}
				return img, nil
			}
			return nil, io.EOF
		}
		if res == ResultPageCompleted {
			if d.doc.page == nil {
				return nil, errors.New("page completed but no image found")
			}
			d.pageIndex++
			img := d.doc.page.ToGoImage()
			if !d.doc.Grouped {
				d.doc.ReleasePageSegments(d.pageIndex)
			}
			return img, nil
		}
		if res == ResultFailure {
			return nil, errors.New("decoding failed")
		}
	}
}

// DecodeAll 解码所有剩余页面
// 返回: []image.Image 图像列表, error 错误信息
func (d *Decoder) DecodeAll() ([]image.Image, error) {
	var images []image.Image
	for {
		img, err := d.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			return images, err
		}
		images = append(images, img)
	}
	return images, nil
}

// GetDocument 获取文档对象
// 返回: *Document 文档对象
func (d *Decoder) GetDocument() *Document {
	return d.doc
}

// Decode 解码JBIG2数据包含的第一页
// 入参: r 读取器
// 返回: image.Image 图像, error 错误信息
func Decode(r io.Reader) (image.Image, error) {
	dec, err := NewDecoder(r)
	if err != nil {
		return nil, err
	}
	return dec.Decode()
}

// DecodeConfig 获取JBIG2图像配置
// 入参: r 读取器
// 返回: image.Config 图像配置, error 错误信息
func DecodeConfig(r io.Reader) (image.Config, error) {
	dec, err := NewDecoder(r)
	if err != nil {
		return image.Config{}, err
	}
	for {
		if len(dec.doc.pageInfoList) > 0 {
			info := dec.doc.pageInfoList[0]
			return image.Config{
				ColorModel: color.GrayModel,
				Width:      int(info.Width),
				Height:     int(info.Height),
			}, nil
		}
		res := dec.doc.DecodeSequential()
		if res == ResultEndReached {
			break
		}
		if res == ResultFailure {
			return image.Config{}, errors.New("decoding failed while looking for config")
		}
	}
	return image.Config{}, errors.New("page information not found")
}

// probeConfigs 探测JBIG2文件的配置
// 入参: data 数据
// 返回: probed 探测后的数据, randomAccess 是否随机访问, littleEndian 是否小端序, orgMode 组织模式, grouped 是否分组
func probeConfigs(data []byte) (probed []byte, randomAccess bool, littleEndian bool, orgMode int, grouped bool) {
	if len(data) < 9 || !bytes.HasPrefix(data, jbig2Signature) {
		return nil, false, false, 0, false
	}
	offset := 9
	if (data[8] & 0x02) == 0 {
		offset = 13
	}
	if len(data) <= offset {
		return nil, false, false, 0, false
	}
	randomAccess = (data[8] & 0x01) == 0
	return data[offset:], randomAccess, false, 0, randomAccess
}

func init() {
	image.RegisterFormat("jbig2", "\x97\x4A\x42\x32\x0D\x0A\x1A\x0A", Decode, DecodeConfig)
}

// ToGoImage 转换为Go标准库Image
// 返回: image.Image 图像
func (i *Image) ToGoImage() image.Image {
	if i == nil {
		return nil
	}
	rect := image.Rect(0, 0, int(i.width), int(i.height))
	img := image.NewGray(rect)
	w, h := int(i.width), int(i.height)
	for y := 0; y < h; y++ {
		src := i.data[int32(y)*i.stride:]
		dst := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			bit := (src[x>>3] >> uint(7-(x&7))) & 1
			if bit == 0 {
				dst[x] = 255
			} else {
				dst[x] = 0
			}
		}
	}
	return img
}
