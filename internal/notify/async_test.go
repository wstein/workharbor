package notify

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
)

type blockingNotifier struct {
	release chan struct{}
	got     chan Message
}

func (b blockingNotifier) Notify(ctx context.Context, m Message) error {
	select {
	case <-b.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	b.got <- m
	return nil
}

// #79: a slow relay must not hold up the caller; the queue is bounded and a
// full queue drops the message with an error instead of blocking.
func TestAsyncNeverBlocksTheCaller(t *testing.T) {
	b := blockingNotifier{release: make(chan struct{}), got: make(chan Message, 8)}
	a := NewAsync(b, 2, time.Second)
	start := time.Now()
	accepted, refused := 0, 0
	for i := range 10 {
		err := a.Notify(context.Background(), Message{TaskID: domain.ID("t" + string(rune('0'+i))), Kind: KindQuestion})
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrQueueFull):
			refused++
		default:
			t.Fatalf("message %d: %v", i, err)
		}
	}
	if time.Since(start) > time.Second {
		t.Fatal("Notify blocked on a slow relay")
	}
	if accepted < 2 || accepted > 3 || refused == 0 {
		t.Fatalf("accepted %d, refused %d; want the queue (2, plus one in flight) to fill and refuse the rest", accepted, refused)
	}
	close(b.release)
	a.Close()
	if len(b.got) == 0 {
		t.Fatal("nothing was delivered after the relay recovered")
	}
}

// #79: two runs of one task that end within the window are two messages.
func TestRunEndedIsPerRun(t *testing.T) {
	th := &Throttle{}
	for _, run := range []domain.ID{"r1", "r2"} {
		if !th.Allow(Message{TaskID: "t1", Kind: KindRunEnded, RunID: run}) {
			t.Fatalf("the end of %s was dropped", run)
		}
	}
	if th.Allow(Message{TaskID: "t1", Kind: KindRunEnded, RunID: "r1"}) {
		t.Fatal("a repeated end of r1 was sent twice")
	}
}

// #79: entries older than the window are pruned, so the map does not grow
// without bound.
func TestThrottlePrunes(t *testing.T) {
	now := time.Unix(0, 0)
	th := &Throttle{Window: time.Minute, Now: func() time.Time { return now }}
	for i := range 50 {
		th.Allow(Message{TaskID: domain.ID("t" + string(rune('A'+i))), Kind: KindQuestion})
	}
	now = now.Add(2 * time.Minute)
	th.Allow(Message{TaskID: "fresh", Kind: KindQuestion})
	if n := len(th.seen); n != 1 {
		t.Fatalf("seen holds %d entries after the window, want 1", n)
	}
	if n := len(th.sent); n != 1 {
		t.Fatalf("sent holds %d tasks after the window, want 1", n)
	}
}

func TestAsyncAfterCloseRefuses(t *testing.T) {
	a := NewAsync(blockingNotifier{release: make(chan struct{}), got: make(chan Message, 1)}, 1, time.Millisecond)
	a.Close()
	a.Close() // twice is fine
	if err := a.Notify(context.Background(), Message{TaskID: "t1"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Notify after Close = %v, want ErrClosed", err)
	}
}
