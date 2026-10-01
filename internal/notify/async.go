package notify

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrQueueFull is returned when a message is dropped because the queue of an
// Async notifier is full. The message is still in the inbox.
var (
	ErrQueueFull = errors.New("the notification queue is full")
	ErrClosed    = errors.New("the notifier is closed")
)

// Async delivers messages from a bounded queue on its own goroutine, so a slow
// or unreachable relay never holds up the caller (design §9.4: a push is best
// effort). Each delivery gets its own timeout.
type Async struct {
	next    Notifier
	timeout time.Duration
	queue   chan Message
	errs    func(error)
	once    sync.Once
	done    chan struct{}

	mu     sync.RWMutex // guards closed against a send on the closed queue
	closed bool
}

// NewAsync starts an Async notifier with room for size queued messages; each
// delivery may take up to timeout.
func NewAsync(next Notifier, size int, timeout time.Duration) *Async {
	if size < 1 {
		size = 1
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	a := &Async{next: next, timeout: timeout, queue: make(chan Message, size), done: make(chan struct{})}
	go a.run()
	return a
}

// OnError sets where delivery errors go; by default they are dropped.
func (a *Async) OnError(f func(error)) { a.errs = f }

func (a *Async) run() {
	defer close(a.done)
	for m := range a.queue {
		ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
		if err := a.next.Notify(ctx, m); err != nil && a.errs != nil {
			a.errs(err)
		}
		cancel()
	}
}

// Notify queues the message and returns at once; a full queue drops it with
// ErrQueueFull.
func (a *Async) Notify(_ context.Context, m Message) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed {
		return ErrClosed
	}
	select {
	case a.queue <- m:
		return nil
	default:
		return ErrQueueFull
	}
}

// Close stops taking messages and waits until the queued ones are delivered
// or have timed out. A Notify after Close returns ErrClosed.
func (a *Async) Close() {
	a.once.Do(func() {
		a.mu.Lock()
		a.closed = true
		close(a.queue)
		a.mu.Unlock()
	})
	<-a.done
}
