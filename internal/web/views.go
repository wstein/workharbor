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

type changeRow struct {
	ID, Repo, From, To string
}

type changesPage struct {
	nav
	Flash string
	// StepUp says a passkey is enrolled, so a change can be confirmed here.
	StepUp  bool
	Changes []changeRow
	// Revoke says the supervisor holds forge tokens it could revoke.
	Revoke bool
}

type deviceRow struct {
	ID, Label, Since, LastSeen string
	Current                    bool
}

type devicesPage struct {
	nav
	Devices []deviceRow
	// Revocable is false when the sign-in cannot list its sessions.
	Revocable bool
	Flash     string
}

type taskRow struct {
	ID, Repo, Issue, State, Agent string
	NeedsYou                      bool
}

type agentChoice struct{ Ref, Repo string }

// usageRowView is a row of the usage card, already written as text.
type usageRowView struct {
	Key, Auth, Turns, Runs, In, Out, CacheRead, CacheWrite, Cache, Cost, Label, APITime, WallTime string
	Link                                                                                          string // the agent's tasks, for a by-agent row
}

// usageCard is the Harbor page's usage card for a period (issue #111, §9.3, §5.7).
type usageCard struct {
	Period  string
	Periods []usagePeriodLink
	// Subscription says the usage windows lead and the cost is API-equivalent.
	Subscription bool
	Windows      []windowRow
	Balance      string
	// Code is the line for the commits approved in the period, or empty.
	Code      string
	Total     []usageRowView
	ByAgent   []usageRowView
	ByModel   []usageRowView
	Sort      string
	SortLinks map[string]string
}

// limitView is one provider of the limits panel, already written as text.
type limitView struct {
	Provider, Source, Balance, Budget string
	Unknown, Low                      bool
	Windows                           []limitWindowView
}

// limitWindowView is one reported window: used and left, reset and age.
type limitWindowView struct {
	ID, Name, Used, Left, Resets, Age string
	Percent                           int
	Low                               bool
}

type usagePeriodLink struct {
	Label, Href string
	Current     bool
}

type harborPage struct {
	AgentRef string // the agent whose tasks are shown, or empty
	nav
	NeedsYou []taskRow
	Others   []taskRow
	Agents   []agentChoice
	Key      string // the idempotency key of the start form
	Flash    string
	// Usage is the account's usage, which leads the page (§5.7); nil when there
	// is nothing to show.
	Usage *usageCard
	// Limits is the limits panel: one entry per provider; never empty when the
	// backend reports limits.
	Limits []limitView
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

// previewRow is an open preview of a task's dev server, or a declared port that
// could be opened (ID empty).
type previewRow struct {
	ID, TaskID, Key string
	Port            int
}

type inboxPage struct {
	nav
	Decisions []decisionRow
	Previews  []previewRow
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
	AgentMayRun                                  []domain.AgentMayRun
	Runs                                         []string
	Decisions                                    []decisionRow
	Events                                       []eventRow
	Earlier                                      int
	Since                                        int64
	SayKey                                       string
	Flash                                        string
	Cancelable                                   bool
	CanPause, CanResume                          bool // a run is running, or is paused
	ActKey                                       string
	StepUp                                       bool
	// Previews are the ports the task's environment declares for previews, each
	// with its open preview if there is one (D33).
	Previews []previewRow
	// Usage is the task's usage line (turns, tokens, cost), or empty.
	Usage string
	// PreviewsOn says the UI has the preview proxy, so the section is shown.
	PreviewsOn bool
	// Panes is the task list beside the task on a wide screen in landscape (§9.6
	// T2); a phone does not show it.
	Panes []paneRow
}

// paneRow is a task in the list beside the task page.
type paneRow struct {
	ID, Repo, Issue, State string
	Current, NeedsYou      bool
}

type purgePage struct {
	nav
	ID, Repo, Issue string
	Events          int
	Bytes           int64
	Key             string
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

// runVerb is the path of the form that pauses a running run or resumes a paused one.
func runVerb(p taskPage) string {
	if p.CanPause {
		return "/pause"
	}
	return "/resume"
}

func taskPageOf(v service.TaskView) taskPage {
	p := taskPage{
		ID: string(v.Task.ID), Repo: v.Task.Repo, Issue: v.Task.Issue, State: string(v.Task.State), Agent: v.Agent,
		Cancelable: !v.Task.State.Terminal(), AgentMayRun: v.AgentMayRun,
	}
	for _, r := range v.Runs {
		line := string(r.ID) + " " + string(r.State)
		if r.State.Terminal() {
			reason := r.TerminalReason
			if reason == "" {
				reason = "unknown"
			}
			line += "; reason " + reason
		}
		p.Runs = append(p.Runs, line)
		if r.State == domain.RunRunning || r.State == domain.RunStarting {
			p.Live = true
		}
		p.CanPause = p.CanPause || r.State == domain.RunRunning
		p.CanResume = p.CanResume || r.State == domain.RunPaused
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

func optionLabel(option string) string {
	if option == domain.AnswerSeen {
		return "Seen"
	}
	return option
}
