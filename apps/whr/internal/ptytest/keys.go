// SPDX-License-Identifier: EUPL-1.2

package ptytest

// Key is a named keystroke sent to the terminal as the bytes a terminal
// emulator in normal cursor-key mode would send.
type Key string

// Common keys.
const (
	KeyEnter     Key = "\r"
	KeyTab       Key = "\t"
	KeyBackspace Key = "\x7f"
	KeyEscape    Key = "\x1b"
	KeyCtrlC     Key = "\x03"
	KeyCtrlD     Key = "\x04"
	KeyUp        Key = "\x1b[A"
	KeyDown      Key = "\x1b[B"
	KeyRight     Key = "\x1b[C"
	KeyLeft      Key = "\x1b[D"
	KeyHome      Key = "\x1b[H"
	KeyEnd       Key = "\x1b[F"
	KeyDelete    Key = "\x1b[3~"
)
