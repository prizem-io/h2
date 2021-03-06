// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package proxy

import (
	"crypto/tls"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"github.com/valyala/fasthttp"
	"golang.org/x/net/http2/hpack"

	"github.com/prizem-io/h2/frames"
)

type H1Upstream struct {
	url     string
	baseURL string

	currentStreamID uint32
	streamCount     uint32

	requestMu sync.Mutex
	requests  map[uint32]*fasthttp.Request

	client *fasthttp.Client
}

func NewH1Upstream(url string, tlsConfig *tls.Config) (Upstream, error) {
	client := &fasthttp.Client{
		MaxIdleConnDuration: 60 * time.Second,
		TLSConfig:           tlsConfig,
		DialDualStack:       true, // Support IPV6
	}

	return &H1Upstream{
		url:             url,
		baseURL:         fmt.Sprintf("http://%s", url),
		currentStreamID: ^uint32(0),
		client:          client,
		requests:        make(map[uint32]*fasthttp.Request, 30),
	}, nil
}

func (u *H1Upstream) IsServed() bool {
	return false
}

func (u *H1Upstream) Serve() error {
	return nil
}

func (u *H1Upstream) StreamCount() int {
	return int(atomic.LoadUint32(&u.streamCount))
}

func (u *H1Upstream) SendHeaders(stream *Stream, params *HeadersParams, endStream bool) error {
	var req *fasthttp.Request

	if stream.RemoteID == 0 {
		nextStreamID := atomic.AddUint32(&u.currentStreamID, 2)
		stream.RemoteID = nextStreamID
		req = fasthttp.AcquireRequest()
		u.requestMu.Lock()
		u.requests[nextStreamID] = req
		u.streamCount = uint32(len(u.requests))
		u.requestMu.Unlock()
		stream.AddCloseCallback(u.closeRequest)
	} else {
		var ok bool
		u.requestMu.Lock()
		req, ok = u.requests[stream.RemoteID]
		u.requestMu.Unlock()
		if !ok {
			return errors.Errorf("could not find stream %d", stream.RemoteID)
		}
	}

	headers := params.Headers
	method := headers.ByName(":method")
	authority := headers.ByName(":authority")
	path := headers.ByName(":path")

	req.SetRequestURI(fmt.Sprintf("%s%s", u.baseURL, path))
	req.Header.SetMethod(method)
	req.Header.SetHost(authority)

	for _, h := range headers {
		if h.Name[0] == ':' {
			continue
		}

		req.Header.Add(h.Name, h.Value)
	}

	if endStream {
		go u.handleRequest(req, stream)
	}

	return nil
}

func (u *H1Upstream) closeRequest(stream *Stream) {
	u.requestMu.Lock()
	defer u.requestMu.Unlock()
	req, ok := u.requests[stream.RemoteID]
	if ok {
		fasthttp.ReleaseRequest(req)
		delete(u.requests, stream.RemoteID)
		u.streamCount = uint32(len(u.requests))
	}
}

func (u *H1Upstream) SendPushPromise(stream *Stream, headers Headers, promisedStreamID uint32) error {
	return nil
}

func (u *H1Upstream) SendData(stream *Stream, data []byte, endStream bool) error {
	u.requestMu.Lock()
	req, ok := u.requests[stream.RemoteID]
	u.requestMu.Unlock()
	if !ok {
		return errors.Errorf("could not find stream %d", stream.RemoteID)
	}

	req.AppendBody(data)

	if endStream {
		go u.handleRequest(req, stream)
	}

	return nil
}

func (u *H1Upstream) SendWindowUpdate(streamID uint32, windowSizeIncrement uint32) error {
	return nil
}

func (u *H1Upstream) SendStreamError(stream *Stream, errorCode frames.ErrorCode) error {
	return nil
}

func (u *H1Upstream) SendConnectionError(stream *Stream, lastStreamID uint32, errorCode frames.ErrorCode) error {
	return nil
}

func (u *H1Upstream) CancelStream(stream *Stream) {
	u.closeRequest(stream)
	stream.RemoteID = 0
}

func (u *H1Upstream) RetryStream(stream *Stream) {
	u.requestMu.Lock()
	defer u.requestMu.Unlock()
	if stream.RemoteID != 0 {
		req, ok := u.requests[stream.RemoteID]
		if ok {
			fasthttp.ReleaseRequest(req)
			delete(u.requests, stream.RemoteID)
			u.streamCount = uint32(len(u.requests))
		}
	}
	nextStreamID := atomic.AddUint32(&u.currentStreamID, 2)
	stream.RemoteID = nextStreamID
	req := fasthttp.AcquireRequest()
	u.requests[nextStreamID] = req
	u.streamCount = uint32(len(u.requests))
}

func (u *H1Upstream) Address() string {
	return u.url
}

func (u *H1Upstream) handleRequest(req *fasthttp.Request, stream *Stream) {
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)
	err := u.client.Do(req, resp)
	if err != nil {
		HandleNetworkError(stream, err)
		return
	}

	bodyBytes := resp.Body()
	hasBody := len(bodyBytes) > 0

	respHeaders := make(Headers, 0, resp.Header.Len()+1)
	respHeaders = append(respHeaders, hpack.HeaderField{
		Name:  ":status",
		Value: strconv.Itoa(resp.StatusCode()),
	})
	resp.Header.VisitAll(func(key, value []byte) {
		respHeaders = append(respHeaders, hpack.HeaderField{
			Name:  strings.ToLower(string(key)),
			Value: string(value),
		})
	})

	context := RHContext{
		Stream: stream,
	}
	err = context.Next(
		&HeadersParams{
			Headers: respHeaders,
		}, !hasBody)
	if err != nil {
		HandleNetworkError(stream, err)
		return
	}

	if hasBody {
		context := RDContext{
			Stream: stream,
		}
		err = context.Next(bodyBytes, true)
		if err != nil {
			HandleNetworkError(stream, err)
			return
		}
	}
}
