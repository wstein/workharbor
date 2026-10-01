package runtime

import (
	"errors"
	"testing"
)

type fakeStream struct {
	chunks []Chunk
	code   int
	err    error
}

func (f fakeStream) Chunks() <-chan Chunk {
	ch := make(chan Chunk, len(f.chunks))
	for _, c := range f.chunks {
		ch <- c
	}
	close(ch)
	return ch
}

func (f fakeStream) Wait() (int, error) { return f.code, f.err }

func TestCollectSeparatesOutputAndReturnsTheExitCode(t *testing.T) {
	boom := errors.New("boom")
	out, errOut, code, err := Collect(fakeStream{
		chunks: []Chunk{{Stdout, []byte("a")}, {Stderr, []byte("x")}, {Stdout, []byte("b")}, {Stderr, []byte("y")}},
		code:   3,
		err:    boom,
	})
	if string(out) != "ab" || string(errOut) != "xy" || code != 3 || !errors.Is(err, boom) {
		t.Errorf("Collect = %q, %q, %d, %v", out, errOut, code, err)
	}
}
