// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package frames

import (
	"io"
)

// Continuation frame flags
const (
	ContinuationFlagEndHeaders Flag = 0x04
)

type Continuation struct {
	StreamID      uint32
	EndHeaders    bool
	BlockFragment []byte
}

func (f *Continuation) Type() Type {
	return TypeContinuation
}

func (f *Continuation) GetStreamID() uint32 {
	return f.StreamID
}

func (f *Continuation) StreamEnd() bool {
	return false
}

func (f *Continuation) Decode(header *FrameHeader, payload []byte) error {
	if header.StreamID == 0 {
		return NewError(ErrorProtocol, "CONTINUATION frame with stream ID 0")
	}
	endHeaders := ContinuationFlagEndHeaders.isSet(header.Flags)
	*f = Continuation{
		StreamID:      header.StreamID,
		EndHeaders:    endHeaders,
		BlockFragment: payload,
	}
	return nil
}

func (f *Continuation) Encode(writer io.Writer) error {
	var err error

	length := uint32(len(f.BlockFragment))
	header := FrameHeader{
		Length:   length,
		Type:     TypeContinuation,
		StreamID: f.StreamID,
	}
	err = header.Encode(writer)
	if err != nil {
		return err
	}
	_, err = writer.Write(f.BlockFragment)
	return err
}
