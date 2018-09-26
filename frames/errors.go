// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package frames

type ErrorCode uint32

// Error codes
const (
	ErrorNone               ErrorCode = 0x0
	ErrorProtocol           ErrorCode = 0x1
	ErrorInternal           ErrorCode = 0x2
	ErrorFlowControl        ErrorCode = 0x3
	ErrorSettingsTimeout    ErrorCode = 0x4
	ErrorStreamClosed       ErrorCode = 0x5
	ErrorFrameSize          ErrorCode = 0x6
	ErrorRefusedStream      ErrorCode = 0x7
	ErrorCancel             ErrorCode = 0x8
	ErrorCompression        ErrorCode = 0x9
	ErrorConnect            ErrorCode = 0xa
	ErrorEnhanceYourCalm    ErrorCode = 0xb
	ErrorInadequateSecurity ErrorCode = 0xc
	ErrorHTTP11Required     ErrorCode = 0xd
)

func (e ErrorCode) String() string {
	switch e {
	case ErrorNone:
		return "NO_ERROR"
	case ErrorProtocol:
		return "PROTOCOL_ERROR"
	case ErrorInternal:
		return "INTERNAL_ERROR"
	case ErrorFlowControl:
		return "FLOW_CONTROL_ERROR"
	case ErrorSettingsTimeout:
		return "SETTINGS_TIMEOUT"
	case ErrorStreamClosed:
		return "STREAM_CLOSED"
	case ErrorFrameSize:
		return "FRAME_SIZE_ERROR"
	case ErrorRefusedStream:
		return "REFUSED_STREAM"
	case ErrorCancel:
		return "CANCEL"
	case ErrorCompression:
		return "COMPRESSION_ERROR"
	case ErrorConnect:
		return "CONNECT_ERROR"
	case ErrorEnhanceYourCalm:
		return "ENHANCE_YOUR_CALM"
	case ErrorInadequateSecurity:
		return "INADEQUATE_SECURITY"
	case ErrorHTTP11Required:
		return "HTTP_1_1_REQUIRED"
	default:
		return "UNKNOWN_ERROR"
	}
}

type ConnectionError struct {
	ErrorCode ErrorCode
	Reason    string
}

func NewError(errorCode ErrorCode, reason string) ConnectionError {
	return ConnectionError{
		ErrorCode: errorCode,
		Reason:    reason,
	}
}

func (e ConnectionError) Error() string {
	return e.Reason
}
