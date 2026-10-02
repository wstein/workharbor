package termproto

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestFramesRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := []Frame{{TypeData, []byte("ls -la\r")}, ResizeFrame(120, 50), {TypeData, nil}, ExitFrame(3), ExitFrame(-1)}
	for _, f := range want {
		if err := Write(&buf, f); err != nil {
			t.Fatal(err)
		}
	}
	for i, w := range want {
		got, err := Read(&buf)
		if err != nil || got.Type != w.Type || !bytes.Equal(got.Data, w.Data) {
			t.Fatalf("frame %d = %+v, %v; want %+v", i, got, err, w)
		}
	}
	if _, err := Read(&buf); !errors.Is(err, io.EOF) {
		t.Errorf("the end of the stream = %v, want io.EOF", err)
	}
	cols, rows, err := ParseResize(ResizeFrame(120, 50))
	if err != nil || cols != 120 || rows != 50 {
		t.Errorf("ParseResize = %d %d %v", cols, rows, err)
	}
	if code, err := ParseExit(ExitFrame(-7)); err != nil || code != -7 {
		t.Errorf("ParseExit = %d %v", code, err)
	}
}

func TestMalformedFramesAreRefused(t *testing.T) {
	if err := Write(io.Discard, Frame{TypeData, make([]byte, MaxPayload+1)}); !errors.Is(err, ErrFrame) {
		t.Errorf("an oversized frame was written: %v", err)
	}
	// A length over the maximum is refused before anything is allocated for it.
	huge := []byte{TypeData, 0xff, 0xff, 0xff, 0xff}
	if _, err := Read(bytes.NewReader(huge)); !errors.Is(err, ErrFrame) {
		t.Errorf("a huge length: %v", err)
	}
	// A cut frame is not a clean end.
	if _, err := Read(strings.NewReader("D\x00\x00\x00\x05ab")); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("a cut frame: %v", err)
	}
	if _, err := Read(strings.NewReader("D\x00\x00")); err == nil || errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("a cut header: %v", err)
	}
	for _, f := range []Frame{{TypeData, []byte{0, 80, 0, 24}}, {TypeResize, []byte{1}}, ResizeFrame(0, 24), ResizeFrame(80, 0)} {
		if _, _, err := ParseResize(f); !errors.Is(err, ErrFrame) {
			t.Errorf("ParseResize(%+v) = %v", f, err)
		}
	}
	if _, err := ParseExit(Frame{TypeExit, []byte{1}}); !errors.Is(err, ErrFrame) {
		t.Errorf("ParseExit = %v", err)
	}
}
