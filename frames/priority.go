// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package frames

import (
	"encoding/binary"
	"fmt"
	"io"
)

type Priority struct {
	StreamID           uint32
	StreamDependencyID uint32
	Weight             uint8
	Exclusive          bool
}

func (f *Priority) Type() Type {
	return TypePriority
}

func (f *Priority) GetStreamID() uint32 {
	return f.StreamID
}

func (f *Priority) StreamEnd() bool {
	return false
}

func (f *Priority) Decode(header *FrameHeader, payload []byte) error {
	if len(payload) != 5 {
		return fmt.Errorf("FRAME_SIZE_ERROR: Received PRIORITY frame of length %d", len(payload))
	}
	streamDependencyID := uint32IgnoreFirstBit(payload[0:4])
	weight := payload[4]
	exclusive := payload[0]&0x80 == 1
	*f = Priority{
		StreamID:           header.StreamID,
		StreamDependencyID: streamDependencyID,
		Weight:             weight,
		Exclusive:          exclusive,
	}
	return nil
}

func (f *Priority) Encode(writer io.Writer) error {
	var err error
	payload := make([]byte, 5)
	binary.BigEndian.PutUint32(payload[0:4], f.StreamDependencyID)
	payload[4] = f.Weight
	if f.Exclusive {
		payload[0] |= 0x80
	}
	header := FrameHeader{
		Length:   5,
		Type:     TypePriority,
		StreamID: f.StreamID,
	}
	err = header.Encode(writer)
	if err != nil {
		return err
	}
	_, err = writer.Write(payload)
	return err
}
