package web

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/store"
)

// The view models are what the templates show. Everything that came from an
// agent, an issue or the forge is text in them, and the templates escape it:
// nothing here is HTML.

// nav is what every page needs: the CSRF token for its forms, the number of
// Decisions waiting and which tab is current.
type nav struct {
	CSRF   string
	Inbox  int
	Active string
}

type taskRow struct {
	ID, Repo, Issue, State, Agent string
	NeedsYou                      bool
}

type agentChoice struct{ Ref, Repo string }

type harborPage struct {
	nav
	NeedsYou []taskRow
	Others   []taskRow
	Agents   []agentChoice
	Key      string // the idempotency key of the start form
	Flash    string
}

type decisionRow struct {
	ID, TaskID, Kind, Subject, Input, Deadline string
	Options                                    []string
	// Review is a Decision that needs the passkey step-up of D45 (a "Ready to push?"
	// or an egress host), so the page offers no plain answer. For says what the
	// approval is for: "this commit" or "this host".
	Review bool
	For    string
	// Plan is an approval of a plan, which reads better as text than as a tool.
	Key string
}

type inboxPage struct {
	nav
	Decisions []decisionRow
	Flash     string
	// StepUp says a passkey is enrolled, so a review can be answered here.
	StepUp bool
}

type eventRow struct {
	Seq    int64
	At     string
	Label  string
	Text   string
	Detail string
}

type taskPage struct {
	nav
	ID, Repo, Issue, State, Agent, Branch, PRURL string
	Live                                         bool
	Runs                                         []string
	Decisions                                    []decisionRow
	Events                                       []eventRow
	Earlier                                      int
	Since                                        int64
	SayKey                                       string
	Flash                                        string
	Cancelable                                   bool
	StepUp                                       bool
}

type cancelPage struct {
	nav
	ID, Repo, Issue string
	Key             string
}

func taskRows(in []store.TaskSummary) (needs, others []taskRow) {
	for _, t := range in {
		r := taskRow{ID: string(t.ID), Repo: t.Repo, Issue: t.Issue, State: string(t.State), Agent: t.Agent, NeedsYou: t.State == domain.TaskAwaitingGuidance}
		if r.NeedsYou {
			needs = append(needs, r)
		} else {
			others = append(others, r)
		}
	}
	return needs, others
}

func decisionRows(in []domain.Decision, key func() string) []decisionRow {
	out := make([]decisionRow, 0, len(in))
	for _, d := range in {
		row := decisionRow{
			ID: string(d.ID), TaskID: string(d.TaskID), Kind: kindLabel(d), Subject: d.Subject, Input: d.Input,
			Options: d.Options, Review: sensitive(&d), For: stepUpFor(d), Key: key(),
		}
		if !d.Deadline.IsZero() {
			row.Deadline = d.Deadline.UTC().Format("15:04 UTC")
		}
		out = append(out, row)
	}
	return out
}

func kindLabel(d domain.Decision) string {
	switch {
	case d.Cause != "":
		return strings.ReplaceAll(string(d.Cause), "_", " ")
	case d.Kind == domain.DecisionApproval:
		return "tool approval"
	case d.Kind == domain.DecisionReview:
		return "ready to push?"
	}
	return string(d.Kind)
}

func taskPageOf(v service.TaskView) taskPage {
	p := taskPage{
		ID: string(v.Task.ID), Repo: v.Task.Repo, Issue: v.Task.Issue, State: string(v.Task.State), Agent: v.Agent,
		Cancelable: !v.Task.State.Terminal(),
	}
	for _, r := range v.Runs {
		p.Runs = append(p.Runs, string(r.ID)+" "+string(r.State))
		if r.State == domain.RunRunning || r.State == domain.RunStarting {
			p.Live = true
		}
	}
	if c := v.Candidate; c != nil {
		p.Branch, p.PRURL = c.Branch, c.PRURL
	}
	return p
}

// maxText bounds what one event shows: a transcript line is untrusted and may
// be huge.
const maxText = 4000

// eventRowOf turns a stored or live event into a row. A transcript event is the
// agent's; any other is the supervisor's own record, shown as its kind and a
// short summary of its data.
func eventRowOf(e domain.Event) eventRow {
	row := eventRow{Seq: e.Seq, At: e.At.UTC().Format("15:04:05")}
	if e.Kind == domain.EventTranscript {
		var a agent.Event
		if json.Unmarshal(e.Payload, &a) == nil && a.Kind != "" {
			row.Label = string(a.Kind)
			switch a.Kind {
			case agent.EventToolCall:
				row.Text, row.Detail = a.Tool, a.Input
			case agent.EventApproval:
				if a.Approval != nil {
					verdict := "denied"
					if a.Approval.Allow {
						verdict = "allowed"
					}
					row.Text, row.Detail = a.Tool+" "+verdict, a.Approval.Reason
				}
			default:
				row.Text = a.Text
			}
			row.Text, row.Detail = clip(row.Text), clip(row.Detail)
			return row
		}
	}
	row.Label = string(e.Kind)
	if e.Tier == domain.TierEphemeral {
		row.Label = "…" + row.Label
	}
	row.Text = clip(summary(e.Payload))
	return row
}

// summary is the data of a supervisor event on one line.
func summary(payload []byte) string {
	var m map[string]any
	if json.Unmarshal(payload, &m) != nil {
		return ""
	}
	parts := make([]string, 0, len(m))
	for _, k := range []string{"object", "id", "from", "to", "state", "subject", "option", "cause", "kind"} {
		if v, ok := m[k]; ok {
			parts = append(parts, k+"="+toString(v))
		}
	}
	return strings.Join(parts, " ")
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// clip keeps at most maxText characters of untrusted text and drops control
// characters other than newline and tab, which a template could not make safe
// to read.
func clip(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return '?'
	}, s)
	if utf8.RuneCountInString(s) <= maxText {
		return s
	}
	return string([]rune(s)[:maxText]) + "…"
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func (t taskPage) sinceURL() string { return "/tasks/" + t.ID + "/events?since=" + itoa(t.Since) }

type openPage struct {
	nav
	ID, Repo, Issue, Path string
	Warnings              []string // files in the copy an editor may run by itself: untrusted names
}
