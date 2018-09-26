// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package frames

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Setting denotes a specific setting in the settings frame.
type Setting uint16

// Settings
const (
	SettingsHeaderTableSize      Setting = 0x01
	SettingsEnablePush           Setting = 0x02
	SettingsMaxConcurrentStreams Setting = 0x03
	SettingsInitialWindowSize    Setting = 0x04
	SettingsMaxFrameSize         Setting = 0x05
	SettingsMaxHeaderListSize    Setting = 0x06
)

// Settings frame flags
const (
	SettingsFlagAck Flag = 0x01
)

func (s Setting) String() string {
	switch s {
	case SettingsHeaderTableSize:
		return "SETTINGS_HEADER_TABLE_SIZE"
	case SettingsEnablePush:
		return "SETTINGS_ENABLE_PUSH"
	case SettingsMaxConcurrentStreams:
		return "SETTINGS_MAX_CONCURRENT_STREAMS"
	case SettingsInitialWindowSize:
		return "SETTINGS_INITIAL_WINDOW_SIZE"
	case SettingsMaxFrameSize:
		return "SETTINGS_MAX_FRAME_SIZE"
	case SettingsMaxHeaderListSize:
		return "SETTINGS_MAX_HEADER_LIST_SIZE"
	default:
		return fmt.Sprintf("Unknown setting %v", uint16(s))
	}
}

func (s Setting) IsSet(f *Settings) bool {
	_, ok := f.Settings[s]
	return ok
}

func (s Setting) Get(f *Settings) uint32 {
	val, _ := f.Settings[s]
	return val
}

func (s Setting) Known() bool {
	v := uint16(s)
	return v <= uint16(SettingsMaxHeaderListSize)
}

type Settings struct {
	Ack bool
	// TODO make into a slice with backing array
	Settings map[Setting]uint32
}

func (f *Settings) Type() Type {
	return TypeSettings
}

func (f *Settings) GetStreamID() uint32 {
	return 0
}

func (f *Settings) StreamEnd() bool {
	return false
}

func (f *Settings) Decode(header *FrameHeader, payload []byte) error {
	if header.StreamID != 0 {
		return fmt.Errorf("Connection error: Received ping frame with stream id %d", header.StreamID)
	}
	if len(payload)%6 != 0 {
		return fmt.Errorf("Invalid SETTINGS frame")
	}
	var settings map[Setting]uint32
	if len(payload) > 5 {
		settings = make(map[Setting]uint32, len(payload)/6)
		for i := 0; i < len(payload); i += 6 {
			setting := Setting(binary.BigEndian.Uint16(payload[i : i+2]))
			value := binary.BigEndian.Uint32(payload[i+2 : i+6])
			// Unknown settings are ignored
			if setting.Known() {
				settings[setting] = value
			}
		}
	}
	*f = Settings{
		Ack:      SettingsFlagAck.isSet(header.Flags),
		Settings: settings,
	}
	return nil
}

func (f *Settings) Encode(writer io.Writer) error {
	var flags Flag
	var err error

	if f.Ack {
		SettingsFlagAck.set(&flags)
	}

	header := FrameHeader{
		Length: uint32(len(f.Settings) * 6),
		Type:   TypeSettings,
		Flags:  flags,
	}
	err = header.Encode(writer)
	if err != nil {
		return err
	}

	for id, value := range f.Settings {
		idBytes := make([]byte, 2)
		binary.BigEndian.PutUint16(idBytes, uint16(id))
		_, err = writer.Write(idBytes)
		if err != nil {
			return err
		}
		valueBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(valueBytes, value)
		_, err = writer.Write(valueBytes)
		if err != nil {
			return err
		}
	}
	return nil
}
