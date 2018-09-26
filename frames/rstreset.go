// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package frames

import (
	"encoding/binary"
	"fmt"
	"io"
)

type RSTStream struct {
	StreamID  uint32
	ErrorCode ErrorCode
}

func (f *RSTStream) Type() Type {
	return TypeRSTStream
}

func (f *RSTStream) GetStreamID() uint32 {
	return f.StreamID
}

func (f *RSTStream) StreamEnd() bool {
	return true
}

func (f *RSTStream) Decode(header *FrameHeader, payload []byte) error {
	if len(payload) != 4 {
		return fmt.Errorf("FRAME_SIZE_ERROR: Received RST_STREAM frame of length %d", len(payload))
	}
	*f = RSTStream{
		StreamID:  header.StreamID,
		ErrorCode: ErrorCode(binary.BigEndian.Uint32(payload)),
	}
	return nil
}

func (f *RSTStream) Encode(writer io.Writer) error {
	var err error
	header := FrameHeader{
		Length:   4,
		Type:     TypeRSTStream,
		StreamID: f.StreamID,
	}
	err = header.Encode(writer)
	if err != nil {
		return err
	}
	result := make([]byte, 4)
	binary.BigEndian.PutUint32(result, uint32(f.ErrorCode))
	_, err = writer.Write(result)
	return err
}
