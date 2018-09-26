// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package frames

import (
	"encoding/binary"
	"io"
)

type PushPromise struct {
	StreamID         uint32
	EndHeaders       bool
	PromisedStreamID uint32
	BlockFragment    []byte
}

func (f *PushPromise) Type() Type {
	return TypePushPromise
}

func (f *PushPromise) GetStreamID() uint32 {
	return f.StreamID
}

func (f *PushPromise) StreamEnd() bool {
	return false
}

func (f *PushPromise) Decode(header *FrameHeader, payload []byte) error {
	endHeaders := FlagEndHeaders.isSet(header.Flags)
	padded := FlagPadded.isSet(header.Flags)
	var err error
	if padded {
		payload, err = stripPadding(payload)
		if err != nil {
			return err
		}
	}
	promisedStreamID := uint32IgnoreFirstBit(payload[0:4])
	blockFragment := payload[4:]
	*f = PushPromise{
		StreamID:         header.StreamID,
		PromisedStreamID: promisedStreamID,
		EndHeaders:       endHeaders,
		BlockFragment:    blockFragment,
	}
	return nil
}

func (f *PushPromise) Encode(writer io.Writer) error {
	var flags Flag
	var err error

	if f.EndHeaders {
		FlagEndHeaders.set(&flags)
	}

	promisedStreamID := make([]byte, 4)
	binary.BigEndian.PutUint32(promisedStreamID, f.PromisedStreamID)
	length := uint32(len(f.BlockFragment)) + 4

	header := FrameHeader{
		Length:   length,
		Type:     TypeHeaders,
		Flags:    flags,
		StreamID: f.StreamID,
	}
	err = header.Encode(writer)
	if err != nil {
		return err
	}
	_, err = writer.Write(promisedStreamID)
	if err != nil {
		return err
	}
	_, err = writer.Write(f.BlockFragment)
	return err
}
