// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package proxy

import (
	"bytes"
	"sync"

	"github.com/prizem-io/h2/frames"
)

var continuationPool = sync.Pool{
	New: func() interface{} {
		return new(continuation)
	},
}

type continuation struct {
	lastHeaders     *frames.Headers
	lastPushPromise *frames.PushPromise
	blockbuf        bytes.Buffer
}

func acquireContinuation() *continuation {
	return continuationPool.Get().(*continuation)
}

func releaseContinuation(continuation *continuation) {
	continuation.lastHeaders = nil
	continuation.lastPushPromise = nil
	continuation.blockbuf.Reset()
	continuationPool.Put(continuation)
}
