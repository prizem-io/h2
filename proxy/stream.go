// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package proxy

import (
	"sync"
	"sync/atomic"

	"github.com/prizem-io/h2/log"
)

type StreamState int32

const (
	StreamStateIdle StreamState = iota
	StreamStateOpen
	StreamStateClosed
)

var streamPool = sync.Pool{
	New: func() interface{} {
		s := new(Stream)
		return s
	},
}

type StreamClosed func(stream *Stream)

type keyedState struct {
	key   string
	state interface{}
}

type Stream struct {
	LocalID    uint32
	RemoteID   uint32
	Connection Connection
	Upstream   Upstream
	Info       interface{}

	middlewareName string
	state          []keyedState
	_state         [20]keyedState
	statePos       int
	stateDir       int

	headersSenders    []HeadersSender
	_headersSenders   [20]HeadersSender
	headersReceivers  []HeadersReceiver
	_headersReceivers [20]HeadersReceiver

	dataSenders    []DataSender
	_dataSenders   [20]DataSender
	dataReceivers  []DataReceiver
	_dataReceivers [20]DataReceiver

	callbackMu      sync.RWMutex
	closeCallbacks  []StreamClosed
	_closeCallbacks [5]StreamClosed

	localState  StreamState
	remoteState StreamState
}

func AcquireStream() *Stream {
	s := streamPool.Get().(*Stream)
	s.reset()
	return s
}

func ReleaseStream(stream *Stream) {
	log.Debugf("Releasing %d -> %d", stream.LocalID, stream.RemoteID)
	streamPool.Put(stream)
}

func (s *Stream) reset() {
	s.LocalID = 0
	s.RemoteID = 0
	s.Connection = nil
	s.Upstream = nil

	s.middlewareName = ""
	s.statePos = 0
	s.stateDir = 1
	s.localState = 0
	s.remoteState = 0

	s.state = s._state[:0]
	s.headersSenders = s._headersSenders[:0]
	s.headersReceivers = s._headersReceivers[:0]
	s.dataSenders = s._dataSenders[:0]
	s.dataReceivers = s.dataReceivers[:0]
	s.closeCallbacks = s._closeCallbacks[:0]
}

func (s *Stream) AddCloseCallback(callback StreamClosed) {
	s.callbackMu.Lock()
	s.closeCallbacks = append(s.closeCallbacks, callback)
	s.callbackMu.Unlock()
}

func (s *Stream) CloseLocal() {
	atomic.StoreInt32((*int32)(&s.localState), int32(StreamStateClosed))
	log.Debugf("Stream %d/%d closed LOCAL", s.LocalID, s.RemoteID)
	s.checkFullyClosed()
}

func (s *Stream) CloseRemote() {
	atomic.StoreInt32((*int32)(&s.remoteState), int32(StreamStateClosed))
	log.Debugf("Stream %d/%d closed REMOTE", s.LocalID, s.RemoteID)
	s.checkFullyClosed()
}

func (s *Stream) FullClose() {
	atomic.StoreInt32((*int32)(&s.localState), int32(StreamStateClosed))
	atomic.StoreInt32((*int32)(&s.remoteState), int32(StreamStateClosed))
	s.invokeCallbacksAndRelease()
}

func (s *Stream) checkFullyClosed() {
	var localState, remoteState StreamState
	localState = StreamState(atomic.LoadInt32((*int32)(&s.localState)))
	remoteState = StreamState(atomic.LoadInt32((*int32)(&s.remoteState)))
	if localState == StreamStateClosed && remoteState == StreamStateClosed {
		log.Debugf("Stream %d/%d is fully closed!", s.LocalID, s.RemoteID)
		s.invokeCallbacksAndRelease()
	}
}

func (s *Stream) invokeCallbacksAndRelease() {
	s.callbackMu.RLock()
	for _, callback := range s.closeCallbacks {
		callback(s)
	}
	s.callbackMu.RUnlock()
	ReleaseStream(s)
}

func (s *Stream) setMiddlewareName(name string) {
	s.middlewareName = name
}

func (s *Stream) State() interface{} {
	l := len(s.state)
	originalPos := s.statePos

	for i := 0; i < 2; i++ {
		if s.stateDir == 1 {
			for s.statePos < l && s.stateDir == 1 {
				i := s.statePos
				s.statePos++

				if s.state[i].key == s.middlewareName {
					return s.state[i].state
				}
			}

			s.statePos = originalPos - 1
			s.stateDir = -1
		} else {
			for s.statePos >= 0 && s.stateDir != 1 {
				i := s.statePos
				s.statePos--

				if s.state[i].key == s.middlewareName {
					return s.state[i].state
				}
			}

			s.statePos = originalPos + 1
			s.stateDir = 1
		}
	}

	return nil
}

func (s *Stream) addState(key string, state interface{}) {
	s.state = append(s.state, keyedState{
		key:   key,
		state: state,
	})
}

func (s *Stream) SendHeaders(params HeadersParams, endStream bool) error {
	return nil
}

func (s *Stream) SendData(data []byte, endStream bool) error {
	return nil
}

func (s *Stream) ReceiveHeaders(params HeadersParams, endStream bool) error {
	return nil
}

func (s *Stream) ReceiveData(data []byte, endStream bool) error {
	return nil
}

func (s *Stream) addHeadersSender(sender HeadersSender) {
	s.headersSenders = append(s.headersSenders, sender)
}

func (s *Stream) addHeadersReceiver(receiver HeadersReceiver) {
	s.headersReceivers = append(s.headersReceivers, receiver)
}

func (s *Stream) addDataSender(sender DataSender) {
	s.dataSenders = append(s.dataSenders, sender)
}

func (s *Stream) addDataReceiver(receiver DataReceiver) {
	s.dataReceivers = append(s.dataReceivers, receiver)
}

func (s *Stream) AddMiddleware(middlewares ...Middleware) {
	for _, middleware := range middlewares {
		s.addState(middleware.Name(), middleware.InitialState())

		if headersSender, ok := middleware.(HeadersSender); ok {
			log.Debugf("Adding Headers Sender %s", headersSender.Name())
			s.addHeadersSender(headersSender)
		}

		if dataSender, ok := middleware.(DataSender); ok {
			log.Debugf("Adding Data Sender %s", dataSender.Name())
			s.addDataSender(dataSender)
		}

		if headersReceiver, ok := middleware.(HeadersReceiver); ok {
			log.Debugf("Adding Headers Receiver %s", headersReceiver.Name())
			s.addHeadersReceiver(headersReceiver)
		}

		if dataReceiver, ok := middleware.(DataReceiver); ok {
			log.Debugf("Adding Data Receiver %s", dataReceiver.Name())
			s.addDataReceiver(dataReceiver)
		}
	}
}
