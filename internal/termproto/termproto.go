// Package termproto is the framing of a terminal session between `whr console`
// and the supervisor (design D43). After an HTTP upgrade the connection carries
// frames both ways: a frame is one type byte, a four-byte big-endian length and
// that many bytes. The client sends the terminal's input (D) and its size (R);
// the server sends the terminal's output (D) and, last, the exit code (X). A frame
// is at most MaxPayload bytes, so a peer cannot make the other allocate without
// bound.
package termproto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// Upgrade is the value of the Upgrade header that asks for a terminal stream.
const Upgrade = "whr-terminal/1"

// SSHUpgrade is the value of the Upgrade header that asks for an SSH connection
// to the console: after the 101, the bytes are the SSH protocol, not framed.
const SSHUpgrade = "whr-ssh/1"

// Frame types.
const (
	TypeData   byte = 'D' // bytes of the terminal's input or output
	TypeResize byte = 'R' // client to server: the terminal's size, two big-endian uint16 (columns, rows)
	TypeExit   byte = 'X' // server to client: the command's exit code, a big-endian int32; the last frame
)

// MaxPayload is the largest frame.
const MaxPayload = 64 << 10

// ErrFrame is matched by every error of a malformed frame.
var ErrFrame = errors.New("termproto: malformed frame")

// Frame is one frame.
type Frame struct {
	Type byte
	Data []byte
}

// Write writes a frame in one Write call, so frames from two goroutines that
// share a mutex never interleave.
func Write(w io.Writer, f Frame) error {
	if len(f.Data) > MaxPayload {
		return fmt.Errorf("%w: %d bytes is more than %d", ErrFrame, len(f.Data), MaxPayload)
	}
	buf := make([]byte, 5+len(f.Data))
	buf[0] = f.Type
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(f.Data))) //nolint:gosec // at most MaxPayload, checked above
	copy(buf[5:], f.Data)
	_, err := w.Write(buf)
	return err
}

// Read reads one frame. A clean end of the stream before a frame is io.EOF.
func Read(r io.Reader) (Frame, error) {
	var head [5]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return Frame{}, err
	}
	n := binary.BigEndian.Uint32(head[1:5])
	if n > MaxPayload {
		return Frame{}, fmt.Errorf("%w: %d bytes is more than %d", ErrFrame, n, MaxPayload)
	}
	f := Frame{Type: head[0], Data: make([]byte, n)}
	if _, err := io.ReadFull(r, f.Data); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return Frame{}, err
	}
	return f, nil
}

// ResizeFrame is the frame of a new terminal size.
func ResizeFrame(cols, rows uint16) Frame {
	b := make([]byte, 4)
	binary.BigEndian.PutUint16(b[0:2], cols)
	binary.BigEndian.PutUint16(b[2:4], rows)
	return Frame{Type: TypeResize, Data: b}
}

// ParseResize reads the size of a resize frame. A size of zero is refused.
func ParseResize(f Frame) (cols, rows uint16, err error) {
	if f.Type != TypeResize || len(f.Data) != 4 {
		return 0, 0, fmt.Errorf("%w: not a resize frame", ErrFrame)
	}
	cols, rows = binary.BigEndian.Uint16(f.Data[0:2]), binary.BigEndian.Uint16(f.Data[2:4])
	if cols == 0 || rows == 0 {
		return 0, 0, fmt.Errorf("%w: a terminal of %d by %d", ErrFrame, cols, rows)
	}
	return cols, rows, nil
}

// ExitFrame is the frame of the command's exit code.
func ExitFrame(code int) Frame {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(int32(max(math.MinInt32, min(math.MaxInt32, code))))) //nolint:gosec // clamped to the int32 range
	return Frame{Type: TypeExit, Data: b}
}

// ParseExit reads the exit code of an exit frame.
func ParseExit(f Frame) (int, error) {
	if f.Type != TypeExit || len(f.Data) != 4 {
		return 0, fmt.Errorf("%w: not an exit frame", ErrFrame)
	}
	return int(int32(binary.BigEndian.Uint32(f.Data))), nil //nolint:gosec // the bytes are an int32 written by ExitFrame
}
