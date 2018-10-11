// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package proxy

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"

	"github.com/pkg/errors"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"

	"github.com/prizem-io/h2/frames"
	"github.com/prizem-io/h2/log"
)

type H2Upstream struct {
	conn net.Conn

	sendMu sync.Mutex
	framer *frames.Framer

	hdec    *hpack.Decoder
	hencbuf *bytes.Buffer
	henc    *hpack.Encoder
	hmu     sync.Mutex

	nextStreamID uint32
	streamsMu    sync.RWMutex
	streams      map[uint32]*Stream
	streamCount  uint32

	continuations map[uint32]*continuation

	maxFrameSize uint32
}

func ConnectTLS(url string, tlsConfig *tls.Config) (net.Conn, error) {
	conn, err := tls.Dial("tcp", url, tlsConfig)
	if err != nil {
		return nil, errors.Wrapf(err, "Error dialing %s", url)
	}

	return conn, nil
}

func Connect(url string) (net.Conn, error) {
	conn, err := net.Dial("tcp", url)
	if err != nil {
		return nil, errors.Wrapf(err, "Error dialing %s", url)
	}

	return conn, nil
}

func NewH2Upstream(url string, tlsConfig *tls.Config) (Upstream, error) {
	var conn net.Conn
	var err error
	if tlsConfig != nil {
		conn, err = ConnectTLS(url, tlsConfig)
	} else {
		conn, err = Connect(url)
	}
	if err != nil {
		return nil, err
	}

	if _, err := io.WriteString(conn, http2.ClientPreface); err != nil {
		return nil, errors.Wrap(err, "error writing client preface")
	}

	framer := frames.NewFramer(conn, conn)
	err = framer.WriteFrame(&frames.Settings{})
	if err != nil {
		return nil, errors.Wrap(err, "error writing settings")
	}

	tableSize := uint32(4 << 10)
	hdec := hpack.NewDecoder(tableSize, func(f hpack.HeaderField) {})
	var hencbuf bytes.Buffer
	henc := hpack.NewEncoder(&hencbuf)

	return &H2Upstream{
		conn:         conn,
		framer:       framer,
		hdec:         hdec,
		hencbuf:      &hencbuf,
		henc:         henc,
		nextStreamID: ^uint32(0),
		streams:      make(map[uint32]*Stream, 100),
		maxFrameSize: initialMaxFrameSize,
	}, nil
}

func (u *H2Upstream) IsServed() bool {
	return true
}

func (u *H2Upstream) Serve() error {
	for {
		frame, err := u.framer.ReadFrame()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			err = u.handleError(err, frame)
			if err != nil {
				return errors.Wrapf(err, "ReadFrame: %v", err)
			}
		}

		//log.Printf("%s, Received: %v", info, frame)

		switch f := frame.(type) {
		case *frames.Ping:
			if f.Ack {
				continue
			}
			u.sendMu.Lock()
			err = u.framer.WriteFrame(&frames.Ping{
				Ack:     true,
				Payload: f.Payload,
			})
			u.sendMu.Unlock()
			if err != nil {
				return errors.Wrapf(err, "WritePing: %v", err)
			}
		case *frames.Settings:
			if f.Ack {
				continue
			}
			if maxFrameSize, ok := f.Settings[frames.SettingsMaxFrameSize]; ok {
				log.Debugf("Setting max frame size to %d\n", maxFrameSize)
				u.maxFrameSize = maxFrameSize
			}
			u.sendMu.Lock()
			err = u.framer.WriteFrame(&frames.Settings{
				Ack: true,
			})
			u.sendMu.Unlock()
			if err != nil {
				log.Errorf("WriteSettingsAck: %v", err)
			}
		case *frames.WindowUpdate:
			if f.StreamID != 0 {
				stream, ok := u.getRemoteStream(f.StreamID)
				if !ok {
					return errors.Errorf("Could not from stream ID %d", frame.GetStreamID())
				}
				err = stream.Connection.SendWindowUpdate(stream, f.WindowSizeIncrement)
			}
		case *frames.Data:
			stream, ok := u.getRemoteStream(f.StreamID)
			if !ok {
				// Ignore processing (most likely the stream was cancelled due to timeout, retry, etc.)
				continue
			}
			context := RDContext{
				Stream: stream,
			}
			err = context.Next(f.Data, f.EndStream)

			// Increase connection-level window size.
			err = u.SendWindowUpdate(0, uint32(len(f.Data)))

			if f.EndStream {
				u.closeStream(stream)
			}
		case *frames.Headers:
			stream, ok := u.getRemoteStream(f.StreamID)
			if !ok {
				// Ignore processing (most likely the stream was cancelled due to timeout, retry, etc.)
				// but we still need to update the hpack decoder.
				if f.EndHeaders {
					_, err = u.hdec.DecodeFull(f.BlockFragment)
				} else {
					continuation := acquireContinuation()
					u.continuations[stream.RemoteID] = continuation
					continuation.lastHeaders = f
					_, err = continuation.blockbuf.Write(f.BlockFragment)
				}
				if err != nil {
					return errors.Wrapf(err, "error updating hpack decoder for cancelled stream: %v", err)
				}
				continue
			}
			if f.EndHeaders {
				err = u.handleHeaders(stream, f, f.BlockFragment)
				if f.EndStream {
					u.closeStream(stream)
				}
			} else {
				continuation := acquireContinuation()
				u.continuations[stream.RemoteID] = continuation
				continuation.lastHeaders = f
				_, err = continuation.blockbuf.Write(f.BlockFragment)
			}
		case *frames.PushPromise:
			stream, ok := u.getRemoteStream(f.StreamID)
			if !ok {
				return errors.Errorf("getRemoteStream %s, %d", frame.Type(), f.StreamID)
			}
			if f.EndHeaders {
				err = u.handlePushPromise(stream, f, f.BlockFragment)
				if err != nil {
					log.Errorf("HTTP/2 push promise error: %v", err)
				}
			} else {
				continuation := acquireContinuation()
				u.continuations[stream.RemoteID] = continuation
				continuation.lastPushPromise = f
				_, err = continuation.blockbuf.Write(f.BlockFragment)
			}
		case *frames.Continuation:
			continuation, ok := u.continuations[f.StreamID]
			if !ok {
				return errors.Errorf("could not continue stream ID %d", f.StreamID)
			}
			stream, ok := u.getRemoteStream(f.StreamID)
			if !ok {
				// Ignore processing (most likely the stream was cancelled due to timeout, retry, etc.)
				// but we still need to update the hpack decoder.
				_, err = continuation.blockbuf.Write(f.BlockFragment)
				if err == nil && f.EndHeaders {
					_, err = u.hdec.DecodeFull(continuation.blockbuf.Bytes())
					delete(u.continuations, stream.RemoteID)
					releaseContinuation(continuation)
				}
				if f.EndHeaders {
					delete(u.continuations, stream.RemoteID)
					releaseContinuation(continuation)
				}
				if err != nil {
					return errors.Wrapf(err, "error updating hpack decoder for cancelled stream: %v", err)
				}
				continue
			}

			_, err = continuation.blockbuf.Write(f.BlockFragment)
			if err == nil && f.EndHeaders {
				if continuation.lastHeaders != nil {
					err = u.handleHeaders(stream, continuation.lastHeaders, continuation.blockbuf.Bytes())
					if err != nil {
						log.Errorf("HTTP/2 header continuation error: %v", err)
					}
					if continuation.lastHeaders.EndStream {
						u.closeStream(stream)
					}
				} else if continuation.lastPushPromise != nil {
					err = u.handlePushPromise(stream, continuation.lastPushPromise, continuation.blockbuf.Bytes())
					if err != nil {
						log.Errorf("HTTP/2 push promise continuation error: %v", err)
					}
				}

				delete(u.continuations, stream.RemoteID)
				releaseContinuation(continuation)
			}
		case *frames.Priority:
			// Do nothing
		case *frames.RSTStream:
			stream, ok := u.getRemoteStream(f.StreamID)
			if !ok {
				return errors.Errorf("Could not from stream ID %d", frame.GetStreamID())
			}
			stream.Connection.SendStreamError(stream, f.ErrorCode)
		case *frames.GoAway:
			return nil // Close the connection
		default:
			log.Errorf("Unhandled upstream frame of type %s", f.Type())
		}

		if err != nil {
			err = u.handleError(err, frame)
			if err != nil {
				return errors.Wrapf(err, "ProcessFrame: %v", err)
			}
		}
	}
}

func (u *H2Upstream) handleHeaders(stream *Stream, frame *frames.Headers, blockFragment []byte) error {
	headers, err := u.hdec.DecodeFull(blockFragment)
	if err != nil {
		return errors.Wrapf(err, "HeadersFrame: %v", err)
	}

	streamDependencyID, err := u.toLocalStreamID(frame.StreamDependencyID)
	if err != nil {
		return err
	}
	context := RHContext{
		Stream: stream,
	}
	return context.Next(
		&HeadersParams{
			Headers:            headers,
			Priority:           frame.Priority,
			Exclusive:          frame.Exclusive,
			StreamDependencyID: streamDependencyID,
			Weight:             frame.Weight,
		}, frame.EndStream)
}

func (u *H2Upstream) handlePushPromise(stream *Stream, frame *frames.PushPromise, blockFragment []byte) error {
	headers, err := u.hdec.DecodeFull(blockFragment)
	if err != nil {
		return errors.Wrapf(err, "PushPromiseFrame: %v", err)
	}
	// TODO: Determine how to create the new local stream ID
	promisedStreamID, err := u.toLocalStreamID(frame.PromisedStreamID)
	if err != nil {
		return err
	}
	return stream.Connection.SendPushPromise(stream, headers, promisedStreamID)
}

func (u *H2Upstream) encodeHeaders(fields []hpack.HeaderField) ([]byte, error) {
	u.hencbuf.Reset()
	for _, header := range fields {
		err := u.henc.WriteField(header)
		if err != nil {
			log.Errorf("WriteHeaders: %v", err)
			return nil, err
		}
	}

	return u.hencbuf.Bytes(), nil
}

func (u *H2Upstream) handleError(err error, frame frames.Frame) error {
	// TODO close all streams for a single connection
	switch e := err.(type) {
	case frames.ConnectionError:
		log.Errorf("Handing connection error: %v", e)
		u.sendMu.Lock()
		err = u.framer.WriteFrame(&frames.GoAway{
			ErrorCode: e.ErrorCode,
			// TODO: LastStreamID
		})
		u.sendMu.Unlock()
		if err != nil {
			return errors.Wrapf(err, "WritePing: %v", err)
		}
		return e
	default:
		log.Errorf("Handing internal error: %v", e)
		u.sendMu.Lock()
		err = u.framer.WriteFrame(&frames.GoAway{
			ErrorCode: frames.ErrorInternal,
			// TODO: LastStreamID
		})
		u.sendMu.Unlock()
		if err != nil {
			return errors.Wrapf(err, "WritePing: %v", err)
		}
		return e
	}
}

func (u *H2Upstream) SendData(stream *Stream, data []byte, endStream bool) error {
	u.sendMu.Lock()
	defer u.sendMu.Unlock()

	//log.Infof("Writing %d bytes to server", len(data))
	err := sendData(u.framer, u.maxFrameSize, stream.RemoteID, data, endStream)
	if err != nil {
		// TODO
		return err
	}

	return nil
}

func (u *H2Upstream) closeStream(stream *Stream) {
	u.streamsMu.Lock()
	log.Debugf("Closing upstream stream %d -> %d", stream.LocalID, stream.RemoteID)
	delete(u.streams, stream.RemoteID)
	u.streamCount = uint32(len(u.streams))
	log.Debugf("-- upstream stream count: %d", len(u.streams))
	u.streamsMu.Unlock()
	delete(u.continuations, stream.RemoteID)
}

func (u *H2Upstream) CancelStream(stream *Stream) {
	u.closeStream(stream)
	stream.RemoteID = 0
}

func (u *H2Upstream) RetryStream(stream *Stream) {
	u.streamsMu.Lock()
	if stream.RemoteID != 0 {
		log.Debugf("Closing upstream stream %d -> %d", stream.LocalID, stream.RemoteID)
		delete(u.streams, stream.RemoteID)
		u.streamCount = uint32(len(u.streams))
		log.Debugf("-- upstream stream count: %d", len(u.streams))
		delete(u.continuations, stream.RemoteID)
	}
	u.nextStreamID += 2
	stream.RemoteID = u.nextStreamID
	log.Debugf("Opening upstream stream %d -> %d", stream.LocalID, stream.RemoteID)
	u.streams[stream.RemoteID] = stream
	u.streamCount = uint32(len(u.streams))
	log.Debugf("++ upstream stream count: %d", len(u.streams))
	u.streamsMu.Unlock()
	// Close callback is already added
}

func (u *H2Upstream) StreamCount() int {
	return int(atomic.LoadUint32(&u.streamCount))
}

func (u *H2Upstream) SendHeaders(stream *Stream, params *HeadersParams, endStream bool) error {
	if stream.RemoteID == 0 {
		u.streamsMu.Lock()
		u.nextStreamID += 2
		stream.RemoteID = u.nextStreamID
		log.Debugf("Opening upstream stream %d -> %d", stream.LocalID, stream.RemoteID)
		u.streams[stream.RemoteID] = stream
		u.streamCount = uint32(len(u.streams))
		log.Debugf("++ upstream stream count: %d", len(u.streams))
		stream.AddCloseCallback(u.closeStream)
		u.streamsMu.Unlock()
	}

	streamDependencyID := params.StreamDependencyID
	if streamDependencyID != 0 {
		other, ok := u.getRemoteStream(streamDependencyID)
		if !ok {
			return frames.ConnectionError{
				ErrorCode: frames.ErrorInternal,
				Reason:    fmt.Sprintf("Could not find remote stream for local stream ID %d", streamDependencyID),
			}
		}
		streamDependencyID = other.RemoteID
	}

	blockFragment, err := u.encodeHeaders(params.Headers)
	if err != nil {
		return err
	}

	u.sendMu.Lock()
	defer u.sendMu.Unlock()
	return sendHeaders(u.framer, u.maxFrameSize, stream.RemoteID, params.Priority, params.Exclusive, streamDependencyID, params.Weight, blockFragment, endStream)
}

func (u *H2Upstream) SendPushPromise(stream *Stream, headers Headers, promisedStreamID uint32) error {
	var err error

	// TODO: how to handle translating a stream ID when it does not exist in the connection
	promisedStreamID, err = u.toRemoteStreamID(stream.Connection, promisedStreamID)
	if err != nil {
		return err
	}

	blockFragment, err := u.encodeHeaders(headers)
	if err != nil {
		return err
	}

	u.sendMu.Lock()
	defer u.sendMu.Unlock()
	return sendPushPromise(u.framer, u.maxFrameSize, stream.RemoteID, promisedStreamID, blockFragment)
}

func (u *H2Upstream) SendStreamError(stream *Stream, errorCode frames.ErrorCode) error {
	u.sendMu.Lock()
	defer u.sendMu.Unlock()

	err := u.framer.WriteFrame(&frames.RSTStream{
		StreamID:  stream.RemoteID,
		ErrorCode: errorCode,
	})

	stream.FullClose() // ???

	return err
}

func (u *H2Upstream) SendWindowUpdate(streamID uint32, windowSizeIncrement uint32) error {
	u.sendMu.Lock()
	defer u.sendMu.Unlock()

	return u.framer.WriteFrame(&frames.WindowUpdate{
		StreamID:            streamID,
		WindowSizeIncrement: windowSizeIncrement,
	})
}

func (u *H2Upstream) SendConnectionError(stream *Stream, lastStreamID uint32, errorCode frames.ErrorCode) error {
	u.sendMu.Lock()
	defer u.sendMu.Unlock()
	var err error

	lastStreamID, err = u.toRemoteStreamID(stream.Connection, lastStreamID)
	if err != nil {
		return err
	}

	err = u.framer.WriteFrame(&frames.GoAway{
		LastStreamID: lastStreamID,
		ErrorCode:    errorCode,
	})

	// TODO: Close all for connections
	stream.Connection.Close()

	return err
}

func (u *H2Upstream) Address() string {
	return u.conn.RemoteAddr().String()
}

func (u *H2Upstream) getRemoteStream(remoteStreamID uint32) (*Stream, bool) {
	u.streamsMu.RLock()
	stream, ok := u.streams[remoteStreamID]
	u.streamsMu.RUnlock()
	return stream, ok
}

func (u *H2Upstream) toLocalStreamID(remoteStreamID uint32) (uint32, error) {
	if remoteStreamID != 0 {
		other, ok := u.getRemoteStream(remoteStreamID)
		if !ok {
			return 0, frames.ConnectionError{
				ErrorCode: frames.ErrorInternal,
				Reason:    fmt.Sprintf("toLocalStreamID: remote stream %d not found", remoteStreamID),
			}
		}
		return other.LocalID, nil
	}

	return 0, nil
}

func (u *H2Upstream) toRemoteStreamID(conn Connection, localStreamID uint32) (uint32, error) {
	if localStreamID != 0 {
		other, ok := conn.GetStream(localStreamID)
		if !ok {
			return 0, frames.ConnectionError{
				ErrorCode: frames.ErrorInternal,
				Reason:    fmt.Sprintf("toRemoteStreamID: local stream %d not found", localStreamID),
			}
		}
		return other.RemoteID, nil
	}

	return 0, nil
}
