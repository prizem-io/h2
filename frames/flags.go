// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package frames

// Flag is a bitwise mask to determine is a flag is set in a frame.
type Flag byte

// Common flags
const (
	FlagEndStream  Flag = 0x01
	FlagEndHeaders Flag = 0x04
	FlagPadded     Flag = 0x08
)

func (f Flag) isSet(flagsByte Flag) bool {
	return flagsByte&f != 0
}

func (f Flag) Has(other Flag) bool {
	return other&f != 0
}

func (f Flag) set(flagsByte *Flag) {
	*flagsByte = *flagsByte | f
}
