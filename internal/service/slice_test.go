package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
)

func next(t *testing.T, ch <-chan domain.Event) (domain.Event, bool) {
	t.Helper()
	select {
	case e, ok := <-ch:
		return e, ok
	case <-time.After(5 * time.Second):
		t.Fatal("no event within 5 s")
		return domain.Event{}, false
	}
}

// nextKind returns the next event of a kind, skipping the agent's own
// transcript events, which also arrive live.
func nextKind(t *testing.T, ch <-chan domain.Event, kind domain.EventKind) domain.Event {
	t.Helper()
	for {
		e, ok := next(t, ch)
		if !ok {
			t.Fatalf("the channel closed before a %s event", kind)
		}
		if e.Kind == kind {
			return e
		}
	}
}

func TestSayDeliversRecordsAndShowsTheDelivery(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.live()
	d, err := r.svc.Say(bg, "t1", "use the helper")
	if err != nil {
		t.Fatal(err)
	}
	if d != agent.DeliveryNextTurn {
		t.Errorf("delivery = %q, want next_turn (the fake's blocked session)", d)
	}
	evs, err := r.store.EventsSince(bg, "t1", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var got *domain.Event
	for i := range evs {
		if evs[i].Kind == domain.EventInstruction {
			got = &evs[i]
		}
	}
	if got == nil || got.Tier != domain.TierAudit {
		t.Fatalf("no audit instruction event in %+v", evs)
	}
	var p domain.InstructionSent
	if err := json.Unmarshal(got.Payload, &p); err != nil || p.RunID != "r1" || p.Delivery != "next_turn" || p.Text != "use the helper" {
		t.Errorf("payload %+v, %v", p, err)
	}
}

func TestSayNeedsARunningRunWithASession(t *testing.T) {
	t.Parallel()
	r := newRig(t) // the run is running in the database but no session is attached
	var c *domain.ConflictError
	if _, err := r.svc.Say(bg, "t1", "hello"); !asConflict(err, &c) || c.Rule != domain.RuleRunLive {
		t.Errorf("no session: %v", err)
	}
	r.live()
	if err := r.svc.Cancel(bg, "t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Say(bg, "t1", "hello"); !asConflict(err, &c) {
		t.Errorf("a cancelled task: %v", err)
	}
	if _, err := r.svc.Say(bg, "nope", "hello"); err == nil {
		t.Error("an unknown task was accepted")
	}
}

func TestListAndShow(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	list, err := r.svc.List(bg, true)
	if err != nil || len(list) != 1 || list[0].ID != "t1" || list[0].State != domain.TaskRunning {
		t.Errorf("list = %+v, %v", list, err)
	}
	v, err := r.svc.Show(bg, "t1")
	if err != nil || v.Task.ID != "t1" || len(v.Runs) != 1 || v.Runs[0].ID != "r1" || v.Candidate != nil || len(v.Open) != 0 {
		t.Errorf("show = %+v, %v", v, err)
	}
	if _, err := r.svc.Show(bg, "nope"); err == nil {
		t.Error("an unknown task was shown")
	}
}

// Subscribe replays what is stored, then follows live events, among them
// ephemeral ones that were never stored.
func TestSubscribeReplaysThenFollows(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.live()
	stored, _ := r.store.EventsSince(bg, "t1", 0, 100)
	if len(stored) == 0 {
		t.Fatal("setup: the task has no events")
	}
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	ch, err := r.svc.Subscribe(ctx, "t1", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range stored {
		e, ok := next(t, ch)
		if !ok || e.Seq != want.Seq {
			t.Fatalf("replay: got seq %d (%v), want %d", e.Seq, ok, want.Seq)
		}
	}
	// A live durable event.
	if _, err := r.svc.Say(bg, "t1", "hello"); err != nil {
		t.Fatal(err)
	}
	e := nextKind(t, ch, domain.EventInstruction)
	if e.Seq == 0 {
		t.Errorf("live event = %+v", e)
	}
	// An ephemeral one: no sequence number, and not in the store.
	r.svc.PublishEphemeral("t1", "token", []byte(`{"text":"he"}`))
	e = nextKind(t, ch, "token")
	if e.Seq != 0 || e.Tier != domain.TierEphemeral {
		t.Errorf("ephemeral event = %+v", e)
	}
	after, _ := r.store.EventsSince(bg, "t1", 0, 100)
	for _, a := range after {
		if a.Kind == "token" {
			t.Error("an ephemeral event was stored")
		}
	}
	// Another task's events do not arrive.
	r.svc.PublishEphemeral("other", "other-token", nil)
	r.svc.PublishEphemeral("t1", "marker", nil)
	for {
		e, _ := next(t, ch)
		if e.TaskID != "t1" || e.Kind == "other-token" {
			t.Fatalf("an event of another task arrived: %+v", e)
		}
		if e.Kind == "marker" {
			break
		}
	}
	cancel()
	for range ch { // closes
	}
}

func TestSubscribeFromASequenceNumberSkipsWhatWasSeen(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	stored, _ := r.store.EventsSince(bg, "t1", 0, 100)
	last := stored[len(stored)-1].Seq
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	ch, err := r.svc.Subscribe(ctx, "t1", last)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-ch:
		t.Errorf("replayed %+v after the last seen", e)
	case <-time.After(50 * time.Millisecond):
	}
}

// A subscriber that does not read never blocks publishing. It loses ephemeral
// events it was too slow for, and is then caught up from the store, so it stays
// open and still gets the next durable event.
func TestASlowSubscriberIsNotWaitedFor(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	stored, _ := r.store.EventsSince(bg, "t1", 0, 100)
	ch, err := r.svc.Subscribe(ctx, "t1", stored[len(stored)-1].Seq)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		for range 3 * subBuffer { // nobody reads: this must not block
			r.svc.PublishEphemeral("t1", "token", nil)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("publishing blocked on a subscriber that does not read")
	}
	saved, err := r.store.Append(bg, domain.NewTranscriptEvent("t1", []byte(`{"after":true}`), r.clock.now))
	if err != nil {
		t.Fatal(err)
	}
	r.svc.publish(saved)
	tokens := 0
	for {
		e, ok := next(t, ch)
		if !ok {
			t.Fatal("the slow subscriber was closed instead of caught up")
		}
		if e.Kind == "token" {
			tokens++
			continue
		}
		if e.Seq != saved[0].Seq {
			t.Fatalf("got %+v, want the durable event %d", e, saved[0].Seq)
		}
		break
	}
	if tokens >= 3*subBuffer {
		t.Errorf("the slow subscriber saw all %d ephemeral events", tokens)
	}
}

// What the agent observes is stored in the transcript tier and shown live.
func TestTheAgentsObservationsAreStoredInTheTranscriptTier(t *testing.T) {
	t.Parallel()
	r := newWsRigBlocking(t, false)
	r.agent.Finish("all done")
	_, a := r.create("transcript")
	task, run, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#1"})
	if err != nil {
		t.Fatal(err)
	}
	r.svc.Wait()
	evs, err := r.store.EventsSince(bg, task, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range evs {
		if e.Kind == domain.EventTranscript && e.Tier == domain.TierTranscript {
			var ae agent.Event
			if err := json.Unmarshal(e.Payload, &ae); err == nil && ae.Kind == agent.EventMessage && ae.Text == "all done" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("run %s: no stored transcript message among %d events", run, len(evs))
	}
}

func asConflict(err error, target **domain.ConflictError) bool { return errors.As(err, target) }

func TestInboxAndWorkspaceList(t *testing.T) {
	t.Parallel()
	r := newWsRigBlocking(t, false)
	r.agent.Finish("done")
	_, a := r.create("inbox")
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#1"}); err != nil {
		t.Fatal(err)
	}
	r.svc.Wait()
	if in, err := r.svc.Inbox(bg); err != nil || len(in) != 0 {
		t.Errorf("inbox = %+v, %v", in, err)
	}
	// An approval raised for the running run shows up in the inbox, a finished
	// task's does not.
	list, err := r.svc.WorkspaceList(bg)
	if err != nil || len(list) != 1 || list[0].Workspace.Name != "inbox" || len(list[0].Agents) != 1 || list[0].Agents[0].Role != "docs" {
		t.Errorf("workspaces = %+v, %v", list, err)
	}
}

func TestLogReturnsTheStoredEventsAfterASequenceNumber(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	all, err := r.svc.Log(bg, "t1", 0, 0)
	if err != nil || len(all) < 2 {
		t.Fatalf("log = %+v, %v", all, err)
	}
	rest, err := r.svc.Log(bg, "t1", all[0].Seq, 1)
	if err != nil || len(rest) != 1 || rest[0].Seq != all[1].Seq {
		t.Errorf("after the first, limit 1 = %+v, %v", rest, err)
	}
	if _, err := r.svc.Log(bg, "nope", 0, 0); err == nil {
		t.Error("an unknown task was accepted")
	}
}

// A task with more history than the live buffer can still be followed from the
// start: the store is the buffer, at the client's pace (a `whr logs -f` on any
// real task).
func TestSubscribeReplaysALongHistory(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	const many = 3 * subBuffer
	for i := range many {
		if _, err := r.store.Append(bg, domain.NewTranscriptEvent("t1", []byte(`{"n":`+strconv.Itoa(i)+`}`), r.clock.now)); err != nil {
			t.Fatal(err)
		}
	}
	stored, _ := r.store.EventsSince(bg, "t1", 0, 5000)
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	ch, err := r.svc.Subscribe(ctx, "t1", 0)
	if err != nil {
		t.Fatalf("Subscribe of a long history: %v", err)
	}
	for i, want := range stored {
		e, ok := next(t, ch)
		if !ok || e.Seq != want.Seq {
			t.Fatalf("event %d: got seq %d (%v), want %d", i, e.Seq, ok, want.Seq)
		}
	}
}

// A subscriber that falls behind the live feed is caught up from the store,
// so it misses no durable event.
func TestASlowSubscriberMissesNoDurableEvent(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	stored, _ := r.store.EventsSince(bg, "t1", 0, 100)
	last := stored[len(stored)-1].Seq
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	ch, err := r.svc.Subscribe(ctx, "t1", last)
	if err != nil {
		t.Fatal(err)
	}
	// Flood the live feed without reading, far past both buffers.
	var want []int64
	for i := range 4 * subBuffer {
		saved, err := r.store.Append(bg, domain.NewTranscriptEvent("t1", []byte(`{"m":`+strconv.Itoa(i)+`}`), r.clock.now))
		if err != nil {
			t.Fatal(err)
		}
		r.svc.publish(saved)
		want = append(want, saved[0].Seq)
	}
	for i, seq := range want {
		e, ok := next(t, ch)
		if !ok || e.Seq != seq {
			t.Fatalf("event %d: got seq %d (%v), want %d", i, e.Seq, ok, seq)
		}
	}
}

// An ephemeral event is redacted like a stored one (T9).
func TestEphemeralEventsAreRedacted(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	stored, _ := r.store.EventsSince(bg, "t1", 0, 100)
	ch, err := r.svc.Subscribe(ctx, "t1", stored[len(stored)-1].Seq)
	if err != nil {
		t.Fatal(err)
	}
	token := "ghp_" + strings.Repeat("a1B2", 9) // the shape of a GitHub token, built here
	r.svc.PublishEphemeral("t1", "token", []byte(`{"text":"key `+token+`"}`))
	e := nextKind(t, ch, "token")
	if strings.Contains(string(e.Payload), token) {
		t.Errorf("an ephemeral event carried a token: %s", e.Payload)
	}
}
