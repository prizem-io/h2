// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package proxy

import (
	"github.com/prizem-io/h2/frames"
	"github.com/prizem-io/h2/log"
)

func SetLogger(logger log.Logger) {
	log.SetLogger(logger)
}

func sendHeaders(framer *frames.Framer, maxFrameSize uint32, streamID uint32, priority bool, exclusive bool, streamDependencyID uint32, weight uint8, blockFragment []byte, endStream bool) (err error) {
	first := true
	endHeaders := false
	b := blockFragment

	// Sends the headers in a single batch.
	for !endHeaders {
		size := uint32(len(b))
		if size > maxFrameSize {
			size = maxFrameSize
		} else {
			endHeaders = true
		}
		next := b[:size]
		b = b[size:]
		if len(next) == 0 {
			break
		}
		if first {
			err = framer.WriteFrame(&frames.Headers{
				StreamID:           streamID,
				EndHeaders:         endHeaders,
				EndStream:          endStream,
				Priority:           priority,
				Exclusive:          exclusive,
				StreamDependencyID: streamDependencyID,
				Weight:             weight,
				BlockFragment:      next,
			})
			first = false
		} else {
			err = framer.WriteFrame(&frames.Continuation{
				StreamID:      streamID,
				EndHeaders:    endHeaders,
				BlockFragment: next,
			})
		}

		if err != nil {
			return
		}
	}

	return
}

func sendPushPromise(framer *frames.Framer, maxFrameSize uint32, streamID uint32, promisedStreamID uint32, blockFragment []byte) (err error) {
	first := true
	endHeaders := false
	b := blockFragment

	// Sends the headers in a single batch.
	for !endHeaders {
		size := uint32(len(b))
		if size > maxFrameSize {
			size = maxFrameSize
		} else {
			endHeaders = true
		}
		next := b[:size]
		b = b[size:]
		if len(next) == 0 {
			break
		}
		if first {
			err = framer.WriteFrame(&frames.PushPromise{
				StreamID:         streamID,
				EndHeaders:       endHeaders,
				PromisedStreamID: promisedStreamID,
				BlockFragment:    next,
			})
			first = false
		} else {
			err = framer.WriteFrame(&frames.Continuation{
				StreamID:      streamID,
				EndHeaders:    endHeaders,
				BlockFragment: next,
			})
		}

		if err != nil {
			return
		}
	}

	return
}

func sendData(framer *frames.Framer, maxFrameSize uint32, streamID uint32, data []byte, endStream bool) error {
	b := data
	size := uint32(len(b))
	last := false

	for size > 0 {
		if size > maxFrameSize {
			size = maxFrameSize
		} else {
			last = true
		}
		next := b[:size]
		b = b[size:]
		size = uint32(len(b))

		err := framer.WriteFrame(&frames.Data{
			StreamID:  streamID,
			EndStream: last && endStream,
			Data:      next,
		})
		if err != nil {
			// TODO
			return err
		}
	}

	return nil
}
