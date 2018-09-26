// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package frames

import (
	"io"
)

type Raw struct {
	Header  FrameHeader
	Payload []byte
}

func (f *Raw) Type() Type {
	return f.Header.Type
}

func (f *Raw) GetStreamID() uint32 {
	return f.Header.StreamID
}

func (f *Raw) StreamEnd() bool {
	// TODO: Make real
	return false
}

func (f *Raw) Decode(header *FrameHeader, payload []byte) error {
	*f = Raw{
		Header:  *header,
		Payload: payload,
	}
	return nil
}

func (f *Raw) Encode(writer io.Writer) error {
	var err error
	err = f.Header.Encode(writer)
	if err != nil {
		return err
	}
	_, err = writer.Write(f.Payload)
	return err
}
