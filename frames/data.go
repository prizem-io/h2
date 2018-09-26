// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package frames

import (
	"io"
)

type Data struct {
	StreamID  uint32
	Data      []byte
	EndStream bool
}

func (f *Data) Type() Type {
	return TypeData
}

func (f *Data) GetStreamID() uint32 {
	return f.StreamID
}

func (f *Data) StreamEnd() bool {
	return f.EndStream
}

func (f *Data) Decode(header *FrameHeader, payload []byte) error {
	endStream := FlagEndStream.isSet(header.Flags)
	padded := FlagPadded.isSet(header.Flags)
	var err error
	if padded {
		payload, err = stripPadding(payload)
		if err != nil {
			return err
		}
	}
	*f = Data{
		StreamID:  header.StreamID,
		Data:      payload,
		EndStream: endStream,
	}
	return nil
}

func (f *Data) Encode(writer io.Writer) error {
	var flags Flag
	var err error
	if f.EndStream {
		FlagEndStream.set(&flags)
	}
	header := FrameHeader{
		Length:   uint32(len(f.Data)),
		Type:     TypeData,
		Flags:    flags,
		StreamID: f.StreamID,
	}
	err = header.Encode(writer)
	if err != nil {
		return err
	}
	_, err = writer.Write(f.Data)
	return err
}
