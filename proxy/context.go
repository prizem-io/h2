// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package proxy

// SHContext is the context for sending headers.
type SHContext struct {
	*Stream
	currentFilter int
}

// RHContext is the context for receiving headers.
type RHContext struct {
	*Stream
	currentFilter int
}

// SDContext is the context for sending data.
type SDContext struct {
	*Stream
	currentFilter int
}

// RDContext is the context for receiving data.
type RDContext struct {
	*Stream
	currentFilter int
}

// Next handles invoking the next `SendHeaders` middleware.
func (c *SHContext) Next(params *HeadersParams, endStream bool) error {
	filters := c.Stream.headersSenders

	// Finished processing the filters - call upstream
	if c.currentFilter >= len(filters) {
		return c.Stream.Upstream.SendHeaders(c.Stream, params, endStream)
	}

	next := filters[c.currentFilter]
	c.currentFilter++
	c.Stream.setMiddlewareName(next.Name())
	return next.SendHeaders(c, params, endStream)
}

// Next handles invoking the next `ReceiveHeaders` middleware.
func (c *RHContext) Next(params *HeadersParams, endStream bool) error {
	filters := c.Stream.headersReceivers

	// Finished processing the filters - call connection
	if c.currentFilter >= len(filters) {
		if c.Stream == nil {
			println("oh damn 1")
		}
		if c.Stream.Connection == nil {
			println("oh damn 2")
		}
		return c.Stream.Connection.SendHeaders(c.Stream.LocalID, params, endStream)
	}

	next := filters[c.currentFilter]
	c.currentFilter++
	c.Stream.setMiddlewareName(next.Name())
	return next.ReceiveHeaders(c, params, endStream)
}

// Next handles invoking the next `SendData` middleware.
func (c *SDContext) Next(data []byte, endStream bool) error {
	filters := c.Stream.dataSenders

	// Finished processing the filters - call upstream
	if c.currentFilter >= len(filters) {
		return c.Stream.Upstream.SendData(c.Stream, data, endStream)
	}

	next := filters[c.currentFilter]
	c.currentFilter++
	c.Stream.setMiddlewareName(next.Name())
	return next.SendData(c, data, endStream)
}

// Next handles invoking the next `ReceiveData` middleware.
func (c *RDContext) Next(data []byte, endStream bool) error {
	filters := c.Stream.dataReceivers

	// Finished processing the filters - call connection
	if c.currentFilter >= len(filters) {
		return c.Stream.Connection.SendData(c.Stream.LocalID, data, endStream)
	}

	next := filters[c.currentFilter]
	c.currentFilter++
	c.Stream.setMiddlewareName(next.Name())
	return next.ReceiveData(c, data, endStream)
}
