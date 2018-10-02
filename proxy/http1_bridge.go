// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package proxy

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/textproto"
	"strconv"

	"github.com/prizem-io/h2/frames"
	"golang.org/x/net/http2/hpack"
)

type http1Bridge struct {
	conn   net.Conn
	stream *Stream
	bw     *bufio.Writer
}

func (h *http1Bridge) Close() error {
	return nil
}

func (h *http1Bridge) Serve() error {
	return nil
}

func (h *http1Bridge) SendHeaders(stream *Stream, params *HeadersParams, endStream bool) error {
	statusStr := Headers(params.Headers).ByName(":status")
	status, _ := strconv.Atoi(statusStr)
	statusText := http.StatusText(status)
	_, err := h.bw.WriteString(fmt.Sprintf("HTTP/1.1 %d %s\r\n", status, statusText))
	if err != nil {
		return err
	}

	for _, header := range params.Headers {
		if header.Name != ":status" {
			mimeName := textproto.CanonicalMIMEHeaderKey(header.Name)
			_, err = h.bw.WriteString(fmt.Sprintf("%s: %s\r\n", mimeName, header.Value))
			if err != nil {
				return err
			}
		}
	}
	_, err = h.bw.WriteString("\r\n")
	if err != nil {
		return err
	}
	if endStream {
		err = h.bw.Flush()
	}
	return err
}

func (h *http1Bridge) SendPushPromise(stream *Stream, headers Headers, promisedStreamID uint32) error {
	return nil
}

func (h *http1Bridge) SendData(stream *Stream, data []byte, endStream bool) error {
	_, err := h.bw.Write(data)
	if err != nil {
		return err
	}
	if endStream {
		err = h.bw.Flush()
	}
	return err
}

func (h *http1Bridge) SendStreamError(stream *Stream, errorCode frames.ErrorCode) error {
	return nil
}
func (h *http1Bridge) SendConnectionError(stream *Stream, lastStreamID uint32, errorCode frames.ErrorCode) error {
	return nil
}

func (h *http1Bridge) SendWindowUpdate(stream *Stream, windowSizeIncrement uint32) error {
	return nil
}

func (h *http1Bridge) GetStream(streamID uint32) (*Stream, bool) {
	return h.stream, false
}

func (h *http1Bridge) CreateStream(streamID uint32, headers []hpack.HeaderField) (*Stream, error) {
	return h.stream, nil
}

func (h *http1Bridge) LocalAddr() string {
	return h.conn.LocalAddr().String()
}

func (h *http1Bridge) RemoteAddr() string {
	return h.conn.RemoteAddr().String()
}
