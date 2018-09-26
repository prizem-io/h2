// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package frames

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Headers frame flags
const (
	HeadersFlagPriority Flag = 0x20
)

type Headers struct {
	StreamID           uint32
	EndStream          bool
	EndHeaders         bool
	Priority           bool
	StreamDependencyID uint32
	Weight             uint8
	Exclusive          bool
	BlockFragment      []byte
}

func (f *Headers) Type() Type {
	return TypeHeaders
}

func (f *Headers) GetStreamID() uint32 {
	return f.StreamID
}

func (f *Headers) StreamEnd() bool {
	return f.EndStream
}

func (f *Headers) Decode(header *FrameHeader, payload []byte) error {
	endStream := FlagEndStream.isSet(header.Flags)
	endHeaders := FlagEndHeaders.isSet(header.Flags)
	padded := FlagPadded.isSet(header.Flags)
	priority := HeadersFlagPriority.isSet(header.Flags)
	var streamDependencyID uint32
	var weight uint8
	var exclusive bool

	var err error
	if padded {
		payload, err = stripPadding(payload)
		if err != nil {
			return err
		}
	}
	if priority {
		if len(payload) <= 5 {
			return NewError(ErrorProtocol,
				"Invalid HEADERS frame: Priority flag set, but payload is too short")
		}
		streamDependencyID = uint32IgnoreFirstBit(payload[0:4])
		weight = payload[4]
		exclusive = payload[0]&0x80 == 1
		payload = payload[5:]
	}

	if err != nil {
		return fmt.Errorf("Error decoding header fields: %v", err)
	}
	*f = Headers{
		StreamID:           header.StreamID,
		EndStream:          endStream,
		EndHeaders:         endHeaders,
		Priority:           priority,
		StreamDependencyID: streamDependencyID,
		Weight:             weight,
		Exclusive:          exclusive,
		BlockFragment:      payload,
	}

	return nil
}

func (f *Headers) Encode(writer io.Writer) error {
	var flags Flag
	var err error

	if f.EndStream {
		FlagEndStream.set(&flags)
	}
	if f.EndHeaders {
		FlagEndHeaders.set(&flags)
	}
	if f.Priority {
		HeadersFlagPriority.set(&flags)
	}

	length := uint32(len(f.BlockFragment))
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
	if f.Priority {
		payload := make([]byte, 5)
		binary.BigEndian.PutUint32(payload[0:4], f.StreamDependencyID)
		payload[4] = f.Weight
		if f.Exclusive {
			payload[0] |= 0x80
		}
		_, err = writer.Write(payload)
		if err != nil {
			return err
		}
	}
	_, err = writer.Write(f.BlockFragment)
	return err
}
