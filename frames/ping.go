// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package frames

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	PingFlagAck Flag = 0x01
)

type Ping struct {
	Payload uint64
	Ack     bool
}

func (f *Ping) Type() Type {
	return TypePing
}

func (f *Ping) GetStreamID() uint32 {
	return 0
}

func (f *Ping) StreamEnd() bool {
	return false
}

func (f *Ping) Decode(header *FrameHeader, payload []byte) error {
	if header.StreamID != 0 {
		return fmt.Errorf("Connection error: Received ping frame with stream id %d", header.StreamID)
	}
	if len(payload) != 8 {
		return fmt.Errorf("Connection error: Received ping frame with %d bytes payload", len(payload))
	}
	*f = Ping{
		Payload: binary.BigEndian.Uint64(payload),
		Ack:     PingFlagAck.isSet(header.Flags),
	}
	return nil
}

func (f *Ping) Encode(writer io.Writer) error {
	var flags Flag
	var err error

	if f.Ack {
		PingFlagAck.set(&flags)
	}
	payloadBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(payloadBytes, f.Payload)

	header := FrameHeader{
		Length: 8,
		Type:   TypePing,
		Flags:  flags,
	}
	err = header.Encode(writer)
	if err != nil {
		return err
	}
	_, err = writer.Write(payloadBytes)
	return err
}
