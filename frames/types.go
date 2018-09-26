// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package frames

import (
	"fmt"
)

// Type determines the type of frame.
type Type byte

// The types of frames in HTTP/2.
const (
	TypeData         Type = 0x00
	TypeHeaders      Type = 0x01
	TypePriority     Type = 0x02
	TypeRSTStream    Type = 0x03
	TypeSettings     Type = 0x04
	TypePushPromise  Type = 0x05
	TypePing         Type = 0x06
	TypeGoAway       Type = 0x07
	TypeWindowUpdate Type = 0x08
	TypeContinuation Type = 0x09
)

func (t Type) String() string {
	switch t {
	case TypeData:
		return "DATA"
	case TypeHeaders:
		return "HEADERS"
	case TypePriority:
		return "PRIORITY"
	case TypeRSTStream:
		return "RST_STREAM"
	case TypeSettings:
		return "SETTINGS"
	case TypePushPromise:
		return "PUSH_PROMISE"
	case TypePing:
		return "PING"
	case TypeGoAway:
		return "GOAWAY"
	case TypeWindowUpdate:
		return "WINDOW_UPDATE"
	case TypeContinuation:
		return "CONTINUATION"
	default:
		return fmt.Sprintf("'Unknown frame type 0x%02X'", byte(t))
	}
}
