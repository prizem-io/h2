// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package frames

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

const (
	frameHeaderLen = 9
	maxFrameSize   = 1<<24 - 1
)

// ErrFrameTooLarge is returned from Framer.ReadFrame when the peer
// sends a frame that is larger than declared with SetMaxReadFrameSize.
var ErrFrameTooLarge = errors.New("http2: frame too large")

// A Framer reads and writes Frames.
type Framer struct {
	r         io.Reader
	lastFrame Frame
	errDetail error

	// lastHeaderStream is non-zero if the last frame was an
	// unfinished HEADERS/CONTINUATION.
	lastHeaderStream uint32

	maxReadSize uint32
	headerBuf   [frameHeaderLen]byte

	// TODO: let getReadBuf be configurable, and use a less memory-pinning
	// allocator in server.go to minimize memory pinned for many idle conns.
	// Will probably also need to make frame invalidation have a hook too.
	getReadBuf func(size uint32) []byte
	readBuf    []byte // cache for default getReadBuf

	//maxWriteSize uint32 // zero means unlimited; TODO: implement

	w    io.Writer
	wbuf []byte

	dataFrame         Data
	headersFrame      Headers
	priorityFrame     Priority
	rstStreamFrame    RSTStream
	settingsFrame     Settings
	pushPromiseFrame  PushPromise
	pingFrame         Ping
	goAwayFrame       GoAway
	windowUpdateFrame WindowUpdate
	continuationFrame Continuation
	rawFrame          Raw
}

func NewFramer(w io.Writer, r io.Reader) *Framer {
	fr := &Framer{
		w: w,
		r: r,
		//logReads:          logFrameReads,
		//logWrites:         logFrameWrites,
		//debugReadLoggerf:  log.Printf,
		//debugWriteLoggerf: log.Printf,
	}
	fr.getReadBuf = func(size uint32) []byte {
		if cap(fr.readBuf) >= int(size) {
			return fr.readBuf[:size]
		}
		fr.readBuf = make([]byte, size)
		return fr.readBuf
	}
	fr.SetMaxReadFrameSize(maxFrameSize)
	return fr
}

// SetMaxReadFrameSize sets the maximum size of a frame
// that will be read by a subsequent call to ReadFrame.
// It is the caller's responsibility to advertise this
// limit with a SETTINGS frame.
func (fr *Framer) SetMaxReadFrameSize(v uint32) {
	if v > maxFrameSize {
		v = maxFrameSize
	}
	fr.maxReadSize = v
}

func (fr *Framer) WriteFrame(frame Frame) error {
	fr.wbuf = fr.wbuf[:0]
	buf := bytes.NewBuffer(fr.wbuf)
	err := frame.Encode(buf)
	if err != nil {
		return err
	}
	_, err = fr.w.Write(buf.Bytes())
	return err
	//return frame.Encode(fr.w)
}

func (fr *Framer) ReadFrame() (Frame, error) {
	fr.errDetail = nil
	/*if fr.lastFrame != nil {
		fr.lastFrame.invalidate()
	}*/
	fh, err := readFrameHeader(fr.headerBuf[:], fr.r)
	if err != nil {
		return nil, err
	}
	if fh.Length > fr.maxReadSize {
		return nil, ErrFrameTooLarge
	}
	payload := fr.getReadBuf(fh.Length)
	if _, err := io.ReadFull(fr.r, payload); err != nil {
		return nil, err
	}
	f, err := typeFrameParser(fh.Type)(fr, fh, payload)
	if err != nil {
		/*if ce, ok := err.(connError); ok {
			return nil, fr.connError(ce.Code, ce.Reason)
		}*/
		return nil, err
	}
	if err := fr.checkFrameOrder(fh, f); err != nil {
		return nil, err
	}
	return f, nil
	//return nil, nil
}

func (fr *Framer) ReadHeader() (FrameHeader, error) {
	fr.errDetail = nil
	/*if fr.lastFrame != nil {
		fr.lastFrame.invalidate()
	}*/
	fh, err := readFrameHeader(fr.headerBuf[:], fr.r)
	if err != nil {
		return fh, err
	}
	if fh.Length > fr.maxReadSize {
		return fh, ErrFrameTooLarge
	}
	return fh, nil
}

func (fr *Framer) ParseFrame(fh FrameHeader, payload []byte) (Frame, error) {
	f, err := typeFrameParser(fh.Type)(fr, fh, payload)
	if err != nil {
		/*if ce, ok := err.(connError); ok {
			return nil, fr.connError(ce.Code, ce.Reason)
		}*/
		return nil, err
	}
	return f, nil
}

func readFrameHeader(buf []byte, r io.Reader) (FrameHeader, error) {
	var fh FrameHeader
	_, err := io.ReadFull(r, buf)
	if err != nil {
		return fh, err
	}

	fh.Decode(buf)
	return fh, nil
}

// checkFrameOrder reports an error if f is an invalid frame to return
// next from ReadFrame. Mostly it checks whether HEADERS and
// CONTINUATION frames are contiguous.
func (fr *Framer) checkFrameOrder(fh FrameHeader, f Frame) error {
	last := fr.lastFrame
	fr.lastFrame = f

	if fr.lastHeaderStream != 0 {
		if fh.Type != TypeContinuation {
			return fr.connError(ErrorProtocol,
				fmt.Sprintf("got %s for stream %d; expected CONTINUATION following %s for stream %d",
					fh.Type, fh.StreamID,
					last.Type(), fr.lastHeaderStream))
		}
		if fh.StreamID != fr.lastHeaderStream {
			return fr.connError(ErrorProtocol,
				fmt.Sprintf("got CONTINUATION for stream %d; expected stream %d",
					fh.StreamID, fr.lastHeaderStream))
		}
	} else if fh.Type == TypeContinuation {
		return fr.connError(ErrorProtocol, fmt.Sprintf("unexpected CONTINUATION for stream %d", fh.StreamID))
	}

	switch fh.Type {
	case TypeHeaders, TypePushPromise, TypeContinuation:
		if fh.Flags.Has(FlagEndHeaders) {
			fr.lastHeaderStream = 0
		} else {
			fr.lastHeaderStream = fh.StreamID
		}
	}

	return nil
}

// connError returns ConnectionError(code) but first
// stashes away a public reason to the caller can optionally relay it
// to the peer before hanging up on them. This might help others debug
// their implementations.
func (fr *Framer) connError(code ErrorCode, reason string) error {
	fr.errDetail = errors.New(reason)
	return ConnectionError{code, reason}
}

// a frameParser parses a frame given its FrameHeader and payload
// bytes. The length of payload will always equal fh.Length (which
// might be 0).
type frameParser func(fc *Framer, fh FrameHeader, payload []byte) (Frame, error)

var frameParsers = []frameParser{
	parseDataFrame,
	parseHeadersFrame,
	parsePriorityFrame,
	parseRSTStreamFrame,
	parseSettingsFrame,
	parsePushPromise,
	parsePingFrame,
	parseGoAwayFrame,
	parseWindowUpdateFrame,
	parseContinuationFrame,
}

func typeFrameParser(t Type) frameParser {
	v := int(t)
	if v >= len(frameParsers) {
		return parseUnknownFrame
	}
	return frameParsers[v]
}

func parseDataFrame(fr *Framer, fh FrameHeader, payload []byte) (Frame, error) {
	err := fr.dataFrame.Decode(&fh, payload)
	if err != nil {
		return nil, err
	}
	return &fr.dataFrame, nil
}

func parseHeadersFrame(fr *Framer, fh FrameHeader, payload []byte) (Frame, error) {
	err := fr.headersFrame.Decode(&fh, payload)
	if err != nil {
		return nil, err
	}
	return &fr.headersFrame, nil
}

func parsePriorityFrame(fr *Framer, fh FrameHeader, payload []byte) (Frame, error) {
	err := fr.priorityFrame.Decode(&fh, payload)
	if err != nil {
		return nil, err
	}
	return &fr.priorityFrame, nil
}

func parseRSTStreamFrame(fr *Framer, fh FrameHeader, payload []byte) (Frame, error) {
	err := fr.rstStreamFrame.Decode(&fh, payload)
	if err != nil {
		return nil, err
	}
	return &fr.rstStreamFrame, nil
}

func parseSettingsFrame(fr *Framer, fh FrameHeader, payload []byte) (Frame, error) {
	err := fr.settingsFrame.Decode(&fh, payload)
	if err != nil {
		return nil, err
	}
	return &fr.settingsFrame, nil
}

func parsePushPromise(fr *Framer, fh FrameHeader, payload []byte) (Frame, error) {
	err := fr.pushPromiseFrame.Decode(&fh, payload)
	if err != nil {
		return nil, err
	}
	return &fr.pushPromiseFrame, nil
}

func parsePingFrame(fr *Framer, fh FrameHeader, payload []byte) (Frame, error) {
	err := fr.pingFrame.Decode(&fh, payload)
	if err != nil {
		return nil, err
	}
	return &fr.pingFrame, nil
}

func parseGoAwayFrame(fr *Framer, fh FrameHeader, payload []byte) (Frame, error) {
	err := fr.goAwayFrame.Decode(&fh, payload)
	if err != nil {
		return nil, err
	}
	return &fr.goAwayFrame, nil
}

func parseWindowUpdateFrame(fr *Framer, fh FrameHeader, payload []byte) (Frame, error) {
	err := fr.windowUpdateFrame.Decode(&fh, payload)
	if err != nil {
		return nil, err
	}
	return &fr.windowUpdateFrame, nil
}

func parseContinuationFrame(fr *Framer, fh FrameHeader, payload []byte) (Frame, error) {
	err := fr.continuationFrame.Decode(&fh, payload)
	if err != nil {
		return nil, err
	}
	return &fr.continuationFrame, nil
}

func parseUnknownFrame(fr *Framer, fh FrameHeader, payload []byte) (Frame, error) {
	err := fr.rawFrame.Decode(&fh, payload)
	if err != nil {
		return nil, err
	}
	return &fr.rawFrame, nil
}
