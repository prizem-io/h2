// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package frames

import (
	"encoding/binary"
	"fmt"
	"io"
)

type WindowUpdate struct {
	StreamID            uint32
	WindowSizeIncrement uint32
}

func (f *WindowUpdate) Type() Type {
	return TypeWindowUpdate
}

func (f *WindowUpdate) GetStreamID() uint32 {
	return f.StreamID
}

func (f *WindowUpdate) StreamEnd() bool {
	return false
}

func (f *WindowUpdate) Decode(header *FrameHeader, payload []byte) error {
	if len(payload) < 4 {
		return fmt.Errorf("FRAME_SIZE_ERROR: Received WINDOW_UPDATE frame of length %d", len(payload))
	}
	*f = WindowUpdate{
		StreamID:            header.StreamID,
		WindowSizeIncrement: uint32IgnoreFirstBit(payload[0:4]),
	}
	return nil
}

func (f *WindowUpdate) Encode(writer io.Writer) error {
	var err error
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, uint32(f.WindowSizeIncrement))

	header := FrameHeader{
		Length:   4,
		Type:     TypeWindowUpdate,
		StreamID: f.StreamID,
	}
	err = header.Encode(writer)
	if err != nil {
		return err
	}

	_, err = writer.Write(payload)
	return err
}
