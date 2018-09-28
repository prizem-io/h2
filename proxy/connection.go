// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package proxy

import (
	"crypto/tls"
	"io"
	"net"
	"net/url"

	"github.com/asaskevich/govalidator"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"golang.org/x/net/http2/hpack"

	"github.com/prizem-io/h2/frames"
)

// NamedConnectionAcceptor associates a name with a connection factory.
type NamedConnectionAcceptor struct {
	Name   string
	Accept ConnectionAcceptor
}

// ConnectionAcceptors is a collection of NamedConnection that allows for lookup via `ForName`.
type ConnectionAcceptors []NamedConnectionAcceptor

// ForName returns a `ConnectionFactory` and `ok` as true given a name if the connection factory is registered.
func (c ConnectionAcceptors) ForName(name string) (factory ConnectionAcceptor, ok bool) {
	// Using a slice assuming few enties would exist and therefore perform better than a map.
	for _, item := range c {
		if item.Name == name {
			return item.Accept, true
		}
	}

	return nil, false
}

var DefaultConnectionAcceptors = ConnectionAcceptors{
	{
		Name:   "HTTP",
		Accept: NewHTTPConnection,
	},
}

type NamedUpstreamDialer struct {
	Name string
	Dial UpstreamDialer
}

type UpstreamDialers []NamedUpstreamDialer

func (u UpstreamDialers) ForName(name string) (dialer UpstreamDialer, ok bool) {
	for _, item := range u {
		if item.Name == name {
			return item.Dial, true
		}
	}

	return nil, false
}

var DefaultUpstreamDialers = UpstreamDialers{
	{
		Name: "HTTP/2",
		Dial: NewH2Upstream,
	},
	{
		Name: "HTTP/1",
		Dial: NewH1Upstream,
	},
}

type ConnectionAcceptor func(conn net.Conn, director Director) (Connection, error)

type Connection interface {
	io.Closer
	Serve() error
	GetStream(streamID uint32) (*Stream, bool)
	CreateStream(streamID uint32, headers []hpack.HeaderField) (*Stream, error)

	// Stream creation
	SendHeaders(streamID uint32, params *HeadersParams, endStream bool) error
	SendPushPromise(streamID uint32, headers Headers, promisedStreamID uint32) error

	// Sending data
	SendData(streamID uint32, data []byte, endStream bool) error
	SendWindowUpdate(streamID uint32, windowSizeIncrement uint32) error

	// Error handling
	SendStreamError(streamID uint32, errorCode frames.ErrorCode) error
	SendConnectionError(streamID uint32, lastStreamID uint32, errorCode frames.ErrorCode) error

	LocalAddr() string
	RemoteAddr() string
}

type UpstreamDialer func(url string, tlsConfig *tls.Config) (Upstream, error)

type Upstream interface {
	IsServed() bool
	Serve() error
	StreamCount() int

	// Stream creation
	SendHeaders(stream *Stream, params *HeadersParams, endStream bool) error
	SendPushPromise(stream *Stream, headers Headers, promisedStreamID uint32) error

	// Sending data
	SendData(stream *Stream, data []byte, endStream bool) error
	SendWindowUpdate(streamID uint32, windowSizeIncrement uint32) error

	// Error handling
	SendStreamError(stream *Stream, errorCode frames.ErrorCode) error
	SendConnectionError(stream *Stream, lastStreamID uint32, errorCode frames.ErrorCode) error

	Address() string
}

type Headers []hpack.HeaderField

func (h Headers) ByName(name string) string {
	for _, header := range h {
		if header.Name == name {
			return header.Value
		}
	}

	return ""
}

func (h *Headers) Set(name, value string) {
	for i := range *h {
		if (*h)[i].Name == name {
			(*h)[i].Value = value
			return
		}
	}

	*h = append(*h, hpack.HeaderField{
		Name:  name,
		Value: value,
	})
}

func (h *Headers) Add(name, value string) {
	*h = append(*h, hpack.HeaderField{
		Name:  name,
		Value: value,
	})
}

type Target struct {
	Upstream    Upstream
	Middlewares []Middleware
	Info        interface{}
}

type Director func(remoteAddr net.Addr, headers Headers) (Target, error)

type Middleware interface {
	Name() string
	InitialState() interface{}
}

type MiddlewareLoader func(input interface{}) (Middleware, error)

type HeadersParams struct {
	Headers            Headers
	Priority           bool
	Exclusive          bool
	StreamDependencyID uint32
	Weight             uint8
}

func (h *HeadersParams) ByName(name string) string {
	for _, header := range h.Headers {
		if header.Name == name {
			return header.Value
		}
	}

	return ""
}

func (h *HeadersParams) AsMap() url.Values {
	values := make(url.Values, len(h.Headers))
	for _, header := range h.Headers {
		values.Add(header.Name, header.Value)
	}

	return values
}

func (h *HeadersParams) Set(name, value string) {
	for i := range h.Headers {
		if h.Headers[i].Name == name {
			h.Headers[i].Value = value
			return
		}
	}

	h.Headers = append(h.Headers, hpack.HeaderField{
		Name:  name,
		Value: value,
	})
}

func (h *HeadersParams) Add(name, value string) {
	h.Headers = append(h.Headers, hpack.HeaderField{
		Name:  name,
		Value: value,
	})
}

type HeadersSender interface {
	Middleware
	SendHeaders(ctx *SHContext, params *HeadersParams, endStream bool) error
}

type HeadersReceiver interface {
	Middleware
	ReceiveHeaders(ctx *RHContext, params *HeadersParams, endStream bool) error
}

type DataSender interface {
	Middleware
	SendData(ctx *SDContext, data []byte, endStream bool) error
}

type DataReceiver interface {
	Middleware
	ReceiveData(ctx *RDContext, data []byte, endStream bool) error
}

// ReadConfig reads the configuration into a struct assuming the struct has the proper tags.
func ReadConfig(input interface{}, config interface{}) error {
	err := mapstructure.Decode(input, config)
	if err != nil {
		return errors.Wrap(err, "Error loading configuration")
	}

	_, err = govalidator.ValidateStruct(config)
	if err != nil {
		return errors.Wrap(err, "Invalid configuration")
	}

	return nil
}
