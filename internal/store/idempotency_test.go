package store

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/exitcode"
)

// doAppend is a command that appends one audit event and answers "ok".
func doAppend(calls *atomic.Int32) func(tx *Tx) ([]byte, error) {
	return func(tx *Tx) ([]byte, error) {
		calls.Add(1)
		if _, err := tx.Append(bg, audit("t1", domain.EventRunStarted)); err != nil {
			return nil, err
		}
		return []byte("ok"), nil
	}
}

func TestDoRunsOnceAndReplaysTheStoredResponse(t *testing.T) {
	s := openTemp(t)
	hash := RequestHash("run", "t1")
	var calls atomic.Int32

	resp, replayed, err := s.Do(bg, "key-1", hash, doAppend(&calls))
	if err != nil || replayed || string(resp) != "ok" {
		t.Fatalf("first call = %q, replayed %v, %v", resp, replayed, err)
	}
	for range 3 {
		resp, replayed, err = s.Do(bg, "key-1", hash, doAppend(&calls))
		if err != nil || !replayed || string(resp) != "ok" {
			t.Fatalf("replay = %q, replayed %v, %v", resp, replayed, err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("the command ran %d times, want once", calls.Load())
	}
	if events, _ := s.EventsSince(bg, "", 0, 0); len(events) != 1 {
		t.Errorf("%d events written, want one: a replay must not do anything again", len(events))
	}
}

func TestDoRefusesAKeyUsedForAnotherRequest(t *testing.T) {
	s := openTemp(t)
	var calls atomic.Int32
	if _, _, err := s.Do(bg, "key-1", RequestHash("run", "t1"), doAppend(&calls)); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Do(bg, "key-1", RequestHash("run", "t2"), doAppend(&calls))
	if !errors.Is(err, ErrKeyReuse) {
		t.Fatalf("a key reused for another request = %v, want ErrKeyReuse", err)
	}
	if got := exitcode.From(err); got != exitcode.Conflict {
		t.Errorf("exit code = %d, want Conflict (%d)", got, exitcode.Conflict)
	}
	if calls.Load() != 1 {
		t.Errorf("the command ran %d times, want once", calls.Load())
	}
}

func TestDoStoresNothingWhenTheCommandFails(t *testing.T) {
	s := openTemp(t)
	hash := RequestHash("run", "t1")
	boom := errors.New("boom")
	_, _, err := s.Do(bg, "key-1", hash, func(tx *Tx) ([]byte, error) {
		if _, err := tx.Append(bg, audit("t1", "partial")); err != nil {
			return nil, err
		}
		return nil, boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the command's", err)
	}
	if got, _ := s.EventsSince(bg, "", 0, 0); len(got) != 0 {
		t.Errorf("a failed command left %d events", len(got))
	}
	// The key is free again: a retry runs the command.
	var calls atomic.Int32
	resp, replayed, err := s.Do(bg, "key-1", hash, doAppend(&calls))
	if err != nil || replayed || string(resp) != "ok" || calls.Load() != 1 {
		t.Errorf("retry = %q, replayed %v, calls %d, %v", resp, replayed, calls.Load(), err)
	}
}

func TestDoValidatesItsArguments(t *testing.T) {
	s := openTemp(t)
	var calls atomic.Int32
	long := make([]byte, maxKeyLength+1)
	for i := range long {
		long[i] = 'k'
	}
	for name, args := range map[string][2]string{
		"empty key":  {"", "hash"},
		"long key":   {string(long), "hash"},
		"empty hash": {"key", ""},
	} {
		if _, _, err := s.Do(bg, args[0], args[1], doAppend(&calls)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if calls.Load() != 0 {
		t.Errorf("the command ran %d times for invalid arguments", calls.Load())
	}
}

func TestDoKeysAreIndependent(t *testing.T) {
	s := openTemp(t)
	var calls atomic.Int32
	for _, key := range []string{"a", "b", "c"} {
		if _, replayed, err := s.Do(bg, key, RequestHash("same", "request"), doAppend(&calls)); err != nil || replayed {
			t.Fatalf("key %s: replayed %v, %v", key, replayed, err)
		}
	}
	if calls.Load() != 3 {
		t.Errorf("the command ran %d times, want 3", calls.Load())
	}
}

// Many callers retrying the same request at once: the command runs once.
func TestDoUnderConcurrency(t *testing.T) {
	s := openTemp(t)
	hash := RequestHash("run", "t1")
	var calls atomic.Int32
	var replays atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, replayed, err := s.Do(bg, "key-1", hash, doAppend(&calls))
			if err != nil || string(resp) != "ok" {
				t.Errorf("Do = %q, %v", resp, err)
			}
			if replayed {
				replays.Add(1)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 || replays.Load() != 7 {
		t.Errorf("the command ran %d times with %d replays, want 1 and 7", calls.Load(), replays.Load())
	}
}

func TestRequestHashIsUnambiguous(t *testing.T) {
	if RequestHash("ab", "c") == RequestHash("a", "bc") {
		t.Error("splitting the parts differently must change the hash")
	}
	first, second := RequestHash("run", "t1"), RequestHash("run", "t1")
	if first != second {
		t.Error("the hash must be deterministic")
	}
	if RequestHash() == RequestHash("") {
		t.Error("no parts and one empty part must differ")
	}
}
