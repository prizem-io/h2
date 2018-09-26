// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package frames

import (
	"encoding/binary"
	"io"
)

type Frame interface {
	Type() Type
	GetStreamID() uint32
	StreamEnd() bool
	Decode(header *FrameHeader, payload []byte) error
	Encode(writer io.Writer) error
}

type FrameHeader struct {
	Length   uint32
	Type     Type
	Flags    Flag
	StreamID uint32
}

func (h *FrameHeader) Decode(data []byte) {
	*h = FrameHeader{
		Length:   uint32IgnoreFirstBit(data[0:3]),
		Type:     Type(data[3]),
		Flags:    Flag(data[4]),
		StreamID: uint32IgnoreFirstBit(data[5:9]),
	}
}

func (h *FrameHeader) Encode(writer io.Writer) error {
	bytes := make([]byte, 4)
	var err error
	binary.BigEndian.PutUint32(bytes, h.Length)
	_, err = writer.Write(bytes[1:])
	if err != nil {
		return err
	}
	bytes[0] = byte(h.Type)
	bytes[1] = byte(h.Flags)
	_, err = writer.Write(bytes[0:2])
	if err != nil {
		return err
	}
	binary.BigEndian.PutUint32(bytes, h.StreamID)
	_, err = writer.Write(bytes)
	return err
}

func (h *FrameHeader) isFlagSet(flag Flag) bool {
	return flag.isSet(h.Flags)
}
