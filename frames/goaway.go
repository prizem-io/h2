// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package frames

import (
	"encoding/binary"
	"fmt"
	"io"
)

type GoAway struct {
	LastStreamID uint32
	ErrorCode    ErrorCode
	DebugData    []byte
}

func (f *GoAway) Type() Type {
	return TypeGoAway
}

func (f *GoAway) GetStreamID() uint32 {
	return 0
}

func (f *GoAway) StreamEnd() bool {
	return true
}

func (f *GoAway) Decode(header *FrameHeader, payload []byte) error {
	if len(payload) < 8 {
		return fmt.Errorf("FRAME_SIZE_ERROR: Received GOAWAY frame of length %v", len(payload))
	}
	var debugData []byte
	if len(payload) > 8 {
		debugData = payload[8:]
	}
	*f = GoAway{
		LastStreamID: uint32IgnoreFirstBit(payload[0:4]),
		ErrorCode:    ErrorCode(binary.BigEndian.Uint32(payload[4:8])),
		DebugData:    debugData,
	}
	return nil
}

func (f *GoAway) Encode(writer io.Writer) error {
	var err error
	header := FrameHeader{
		Length: 8 + uint32(len(f.DebugData)),
		Type:   TypeGoAway,
	}
	err = header.Encode(writer)
	if err != nil {
		return err
	}
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[0:4], f.LastStreamID)
	binary.BigEndian.PutUint32(payload[4:8], uint32(f.ErrorCode))
	_, err = writer.Write(payload)
	if err != nil {
		return err
	}
	if len(f.DebugData) > 0 {
		_, err = writer.Write(f.DebugData)
	}
	return err
}
