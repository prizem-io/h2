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
	done   chan struct{}
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
	h.bw.WriteString(fmt.Sprintf("HTTP/1.1 %d %s\r\n", status, statusText))

	for _, header := range params.Headers {
		if header.Name != ":status" {
			mimeName := textproto.CanonicalMIMEHeaderKey(header.Name)
			h.bw.WriteString(fmt.Sprintf("%s: %s\r\n", mimeName, header.Value))
		}
	}
	h.bw.WriteString("\r\n")
	if endStream {
		h.bw.Flush()
		close(h.done)
	}
	return nil
}

func (h *http1Bridge) SendPushPromise(stream *Stream, headers Headers, promisedStreamID uint32) error {
	return nil
}

func (h *http1Bridge) SendData(stream *Stream, data []byte, endStream bool) error {
	h.bw.Write(data)
	if endStream {
		h.bw.Flush()
		close(h.done)
	}
	return nil
}

func (h *http1Bridge) SendStreamError(stream *Stream, errorCode frames.ErrorCode) error {
	close(h.done)
	return nil
}
func (h *http1Bridge) SendConnectionError(stream *Stream, lastStreamID uint32, errorCode frames.ErrorCode) error {
	close(h.done)
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
