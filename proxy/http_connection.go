// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package proxy

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"sync"

	"github.com/pkg/errors"
	"golang.org/x/net/http2/hpack"

	"github.com/prizem-io/h2/frames"
	"github.com/prizem-io/h2/log"
)

const initialMaxFrameSize = 16384

var bytesClientPreface = []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
var bytesClientPrefaceLen = len(bytesClientPreface)

var bytesClientPrefaceBody = []byte("SM\r\n\r\n")
var bytesClientPrefaceBodyLen = len(bytesClientPrefaceBody)

type HTTPConnection struct {
	conn net.Conn
	rw   *bufio.ReadWriter

	sendMu sync.Mutex
	framer *frames.Framer

	blockbuf *bytes.Buffer
	hdec     *hpack.Decoder
	hencbuf  *bytes.Buffer
	henc     *hpack.Encoder
	hmu      sync.Mutex

	director  Director
	streamsMu sync.RWMutex
	streams   map[uint32]*Stream

	lastHeaders     *frames.Headers
	lastPushPromise *frames.PushPromise

	maxFrameSize uint32
}

func Listen(ln net.Listener, director Director) error {
	for {
		conn, err := Accept(ln, director)
		if err != nil {
			return err
		}

		go conn.Serve()
	}
}

func Accept(ln net.Listener, director Director) (Connection, error) {
	conn, err := ln.Accept()
	if err != nil {
		return nil, errors.Wrap(err, "error accepting connection")
	}

	return NewHTTPConnection(conn, director)
}

func NewHTTPConnection(conn net.Conn, director Director) (Connection, error) {
	tableSize := uint32(4 << 10)
	hdec := hpack.NewDecoder(tableSize, func(f hpack.HeaderField) {})
	var hencbuf bytes.Buffer
	henc := hpack.NewEncoder(&hencbuf)
	var blockbuf bytes.Buffer

	br := bufio.NewReader(conn)
	bw := bufio.NewWriter(conn)
	rw := bufio.ReadWriter{Reader: br, Writer: bw}

	return &HTTPConnection{
		conn:         conn,
		rw:           &rw,
		framer:       frames.NewFramer(rw.Writer, rw.Reader),
		blockbuf:     &blockbuf,
		hdec:         hdec,
		hencbuf:      &hencbuf,
		henc:         henc,
		director:     director,
		streams:      make(map[uint32]*Stream, 25),
		maxFrameSize: initialMaxFrameSize,
	}, nil
}

func (c *HTTPConnection) Close() error {
	c.streamsMu.Lock()
	streams := make(map[uint32]*Stream, len(c.streams))
	for k, v := range c.streams {
		streams[k] = v
	}
	c.streamsMu.Unlock()

	for _, stream := range streams {
		stream.Upstream.SendStreamError(stream, frames.ErrorNone)
	}

	return c.conn.Close()
}

func (c *HTTPConnection) Serve() error {
	log.Infof("New connection from %s", c.conn.RemoteAddr())
	defer log.Infof("Disconnected from %s", c.conn.RemoteAddr())
	defer func() { _ = c.Close() }()

	streamID := uint32(1)

	for {
		var rh RequestHeader
		err := rh.Read(c.rw.Reader)
		if err != nil {
			return err
		}

		if bytes.Equal(rh.method, []byte("PRI")) && bytes.Equal(rh.requestURI, []byte("*")) && bytes.Equal(rh.protocol, []byte("HTTP/2.0")) {
			buffer := make([]byte, bytesClientPrefaceBodyLen)
			n, err := c.rw.Reader.Read(buffer)
			if err != nil {
				return errors.Wrap(err, "error reading preface")
			}

			if n != bytesClientPrefaceBodyLen && !bytes.Equal(buffer, bytesClientPrefaceBody) {
				return errors.Wrap(err, "HTTP 2 client preface was expected")
			}

			log.Debug("Upgraded HTTP connection to H2")
			return c.serveH2()
		}

		err = c.handleHTTP1Request(&rh, streamID)
		if err != nil {
			return err
		}

		streamID += 2

		// TODO???
		// if rh.connectionClose
	}
}

func (c *HTTPConnection) handleHTTP1Request(rh *RequestHeader, streamID uint32) error {
	var body []byte
	var err error
	if rh.contentLength > 0 {
		body, err = readBody(c.rw.Reader, rh.contentLength, 1000000, body)
		if err != nil {
			return errors.Wrap(err, "error reading HTTP/1 body")
		}
	}

	headers := make(Headers, 0, len(rh.headers)+4)
	headers = append(headers, hpack.HeaderField{
		Name:  ":method",
		Value: string(rh.method),
	})
	headers = append(headers, hpack.HeaderField{
		Name:  ":authority",
		Value: string(rh.host),
	})
	headers = append(headers, hpack.HeaderField{
		Name:  ":scheme",
		Value: "https",
	})
	headers = append(headers, hpack.HeaderField{
		Name:  ":path",
		Value: string(rh.requestURI),
	})
	headers = append(headers, rh.headers...)

	stream := AcquireStream()
	bridge := http1Bridge{
		conn:   c.conn,
		stream: stream,
		bw:     c.rw.Writer,
		done:   make(chan struct{}, 1),
	}
	target, err := c.director(c.conn.RemoteAddr(), headers)
	if err != nil {
		if err == ErrNotFound {
			respondWithError(&bridge, err, streamID, 404)
			return nil
		} else if err == ErrServiceUnavailable {
			respondWithError(&bridge, err, streamID, 503)
			return nil
		}
		log.Errorf("director error: %v", err)
		respondWithError(&bridge, ErrInternalServerError, streamID, 500)
		return nil
	}

	stream.LocalID = streamID
	stream.Connection = &bridge
	stream.Upstream = target.Upstream
	stream.Info = target.Info
	stream.AddMiddleware(target.Middlewares...)

	hasBody := len(body) > 0

	context := SHContext{
		Stream: stream,
	}
	err = context.Next(&HeadersParams{
		Headers: headers,
	}, !hasBody)
	if err != nil {
		log.Errorf("HTTP/1 send header error: %v", err)
		respondWithError(&bridge, ErrInternalServerError, streamID, 500)
		return nil
	}

	if hasBody {
		context := SDContext{
			Stream: stream,
		}
		err = context.Next(body, true)
		if err != nil {
			log.Errorf("HTTP/1 send data error: %v", err)
			respondWithError(&bridge, ErrInternalServerError, streamID, 500)
			return nil
		}
	}

	// TODO timeout
	<-bridge.done
	stream.FullClose()

	return nil
}

func (c *HTTPConnection) serveH2() error {
	// Send initial settings
	err := c.SendSettings(false, map[frames.Setting]uint32{
		frames.SettingsInitialWindowSize: 1073741824,
	})
	if err != nil {
		return errors.Wrap(err, "error sending initial settings")
	}

	for {
		frame, err := c.framer.ReadFrame()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return errors.Wrapf(err, "ReadFrame: %v", err)
		}

		//log.Infof("Received: %s -> %v", frame.Type(), frame)

		switch f := frame.(type) {
		case *frames.Ping:
			c.sendMu.Lock()
			err = c.framer.WriteFrame(&frames.Ping{
				Ack:     true,
				Payload: f.Payload,
			})
			c.rw.Writer.Flush()
			c.sendMu.Unlock()
		case *frames.Settings:
			if f.Ack {
				continue
			}

			if maxFrameSize, ok := f.Settings[frames.SettingsMaxFrameSize]; ok {
				log.Debugf("Setting max frame size to %d", maxFrameSize)
				c.maxFrameSize = maxFrameSize
			}

			err = c.SendSettings(true, nil)
		case *frames.WindowUpdate:
			if f.StreamID != 0 {
				stream, ok := c.GetStream(f.StreamID)
				if !ok {
					return errors.Errorf("could not from stream ID %d", frame.GetStreamID())
				}
				err = stream.Upstream.SendWindowUpdate(stream.RemoteID, f.WindowSizeIncrement)
				if err != nil {
					log.Errorf("HTTP/2 stream window update error: %v", err)
				}
			}
		case *frames.Data:
			stream, ok := c.GetStream(f.StreamID)
			if !ok {
				return errors.Errorf("could not from stream ID %d", frame.GetStreamID())
			}
			context := SDContext{
				Stream: stream,
			}
			err = context.Next(f.Data, f.EndStream)
			if err != nil {
				log.Errorf("HTTP/2 data error: %v", err)
				respondWithError(c, ErrInternalServerError, f.StreamID, 500)
			} else {
				// Increase connection-level window size.
				err = c.SendWindowUpdate(0, uint32(len(f.Data)))
				if err != nil {
					log.Errorf("HTTP/2 connection window update error: %v", err)
				}
			}
		case *frames.Headers:
			if f.EndHeaders {
				err = c.handleHeaders(f, f.BlockFragment)
				if err != nil {
					log.Errorf("HTTP/2 header error: %v", err)
				}
			} else {
				c.lastHeaders = f
				c.blockbuf.Reset()
				_, _ = c.blockbuf.Write(f.BlockFragment)
			}
		case *frames.PushPromise:
			if f.EndHeaders {
				err = c.handlePushPromise(f, f.BlockFragment)
				if err != nil {
					log.Errorf("HTTP/2 push promise error: %v", err)
				}
			} else {
				c.lastPushPromise = f
				c.blockbuf.Reset()
				_, _ = c.blockbuf.Write(f.BlockFragment)
			}
		case *frames.Continuation:
			_, _ = c.blockbuf.Write(f.BlockFragment)
			if f.EndHeaders {
				if c.lastHeaders != nil {
					err = c.handleHeaders(c.lastHeaders, c.blockbuf.Bytes())
					if err != nil {
						log.Errorf("HTTP/2 continuation error: %v", err)
					}
					c.lastHeaders = nil
				} else if c.lastPushPromise != nil {
					err = c.handlePushPromise(c.lastPushPromise, c.blockbuf.Bytes())
					if err != nil {
						log.Errorf("HTTP/2 continuation error: %v", err)
					}
					c.lastPushPromise = nil
				}
			} else {
				c.blockbuf.Reset()
				_, err = c.blockbuf.Write(f.BlockFragment)
				if err != nil {
					log.Errorf("HTTP/2 continuation error: %v", err)
				}
			}
		case *frames.RSTStream:
			stream, ok := c.GetStream(f.StreamID)
			if !ok {
				return errors.Errorf("Could not from stream ID %d", frame.GetStreamID())
			}
			stream.Connection.SendStreamError(stream.RemoteID, f.ErrorCode)
		case *frames.GoAway:
			//c.Close()

			return nil
		default:
			log.Errorf("unexpected connection frame of type %s", frame.Type())
		}

		if err != nil {
			return err
		}
	}
}

func (c *HTTPConnection) handleHeaders(frame *frames.Headers, blockFragment []byte) error {
	headers, err := c.hdec.DecodeFull(blockFragment)
	if err != nil {
		return errors.Wrapf(err, "HeadersFrame: %v", err)
	}
	stream, ok := c.GetStream(frame.StreamID)
	if !ok {
		stream, err = c.CreateStream(frame.StreamID, headers)
		if err != nil {
			return err
		}
	}
	if stream == nil {
		return nil
	}
	context := SHContext{
		Stream: stream,
	}
	return context.Next(&HeadersParams{
		Headers:            headers,
		Priority:           frame.Priority,
		Exclusive:          frame.Exclusive,
		StreamDependencyID: frame.StreamDependencyID,
		Weight:             frame.Weight,
	}, frame.EndStream)
}

func (c *HTTPConnection) handlePushPromise(frame *frames.PushPromise, blockFragment []byte) error {
	stream, ok := c.GetStream(frame.StreamID)
	if !ok {
		return errors.Errorf("could not from stream ID %d", frame.StreamID)
	}
	headers, err := c.hdec.DecodeFull(blockFragment)
	if err != nil {
		return errors.Wrapf(err, "PushPromiseFrame: %v", err)
	}
	return stream.Upstream.SendPushPromise(stream, headers, frame.PromisedStreamID)
}

func (c *HTTPConnection) encodeHeaders(fields []hpack.HeaderField) ([]byte, error) {
	c.hmu.Lock()
	defer c.hmu.Unlock()

	c.hencbuf.Reset()
	for _, header := range fields {
		err := c.henc.WriteField(header)
		if err != nil {
			log.Errorf("WriteHeaders: %v", err)
			return nil, err
		}
	}

	return c.hencbuf.Bytes(), nil
}

func (c *HTTPConnection) SendSettings(ack bool, settings map[frames.Setting]uint32) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	err := c.framer.WriteFrame(&frames.Settings{
		Ack:      ack,
		Settings: settings,
	})
	if err != nil {
		return err
	}
	err = c.rw.Writer.Flush()

	return err
}

func (c *HTTPConnection) SendData(streamID uint32, data []byte, endStream bool) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	err := sendData(c.framer, c.maxFrameSize, streamID, data, endStream)
	c.rw.Writer.Flush()

	/*if endStream {
		c.closeStream(streamID)
	}*/

	return err
}

func (c *HTTPConnection) SendHeaders(streamID uint32, params *HeadersParams, endStream bool) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	blockFragment, err := c.encodeHeaders(params.Headers)
	if err != nil {
		return err
	}

	err = sendHeaders(c.framer, c.maxFrameSize, streamID, params.Priority, params.Exclusive, params.StreamDependencyID, params.Weight, blockFragment, endStream)
	c.rw.Writer.Flush()

	/*if endStream {
		c.closeStream(streamID)
	}*/

	return err
}

func (c *HTTPConnection) SendPushPromise(streamID uint32, headers Headers, promisedStreamID uint32) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	blockFragment, err := c.encodeHeaders(headers)
	if err != nil {
		return err
	}

	err = sendPushPromise(c.framer, c.maxFrameSize, streamID, promisedStreamID, blockFragment)
	c.rw.Writer.Flush()

	return err
}

func (c *HTTPConnection) SendWindowUpdate(streamID uint32, windowSizeIncrement uint32) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	err := c.framer.WriteFrame(&frames.WindowUpdate{
		StreamID:            streamID,
		WindowSizeIncrement: windowSizeIncrement,
	})
	if err != nil {
		return err
	}
	err = c.rw.Writer.Flush()

	return err
}

func (c *HTTPConnection) SendStreamError(streamID uint32, errorCode frames.ErrorCode) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	err := c.framer.WriteFrame(&frames.RSTStream{
		StreamID:  streamID,
		ErrorCode: errorCode,
	})
	c.rw.Writer.Flush()

	//c.closeStream(streamID)

	return err
}

func (c *HTTPConnection) SendConnectionError(streamID uint32, lastStreamID uint32, errorCode frames.ErrorCode) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	err := c.framer.WriteFrame(&frames.GoAway{
		LastStreamID: lastStreamID,
		ErrorCode:    errorCode,
	})
	c.rw.Writer.Flush()
	return err
}

func (c *HTTPConnection) CreateStream(streamID uint32, headers []hpack.HeaderField) (*Stream, error) {
	target, err := c.director(c.conn.RemoteAddr(), headers)
	if err != nil {
		if err == ErrNotFound {
			respondWithError(c, err, streamID, 404)
			return nil, nil
		} else if err == ErrServiceUnavailable {
			respondWithError(c, err, streamID, 503)
			return nil, nil
		}
		log.Errorf("director error: %v", err)
		respondWithError(c, ErrInternalServerError, streamID, 500)
		return nil, nil
	}

	stream := AcquireStream()
	stream.LocalID = streamID
	if c == nil {
		log.Warn("createStream c is nil")
	}
	stream.Connection = c
	stream.Upstream = target.Upstream
	stream.Info = target.Info
	stream.AddCloseCallback(c.closeStream)
	stream.AddMiddleware(target.Middlewares...)

	c.streamsMu.Lock()
	c.streams[streamID] = stream
	c.streamsMu.Unlock()

	return stream, nil
}

func (c *HTTPConnection) GetStream(streamID uint32) (*Stream, bool) {
	c.streamsMu.RLock()
	stream, ok := c.streams[streamID]
	c.streamsMu.RUnlock()
	return stream, ok
}

func (c *HTTPConnection) LocalAddr() string {
	return c.conn.LocalAddr().String()
}

func (c *HTTPConnection) RemoteAddr() string {
	return c.conn.RemoteAddr().String()
}

func (c *HTTPConnection) closeStream(stream *Stream) {
	c.streamsMu.Lock()
	delete(c.streams, stream.LocalID)
	c.streamsMu.Unlock()
}
