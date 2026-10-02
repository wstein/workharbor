package github

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/redact"
)

// fakeBoard answers the GraphQL calls of the board mirror and records them.
type fakeBoard struct {
	mu        sync.Mutex
	calls     []string // "project", "issue", "add", "set:<field>=<value>"
	card      bool     // the issue is already on the board
	sessionTy string   // data type of the Session field: TEXT or SINGLE_SELECT
	errs      []string // GraphQL errors to return for every call
	owner     string   // "user" or "organization" as queried
	noProject bool     // the project is not visible: the query answers null
	pages     [][]any  // the project's items, a page each, for the queue query
}

func (b *fakeBoard) handler(t *testing.T, f *fakeGitHub) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock() // the fake has read the body and keeps it
		raw := []byte(f.bodies["POST /graphql"])
		f.mu.Unlock()
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Errorf("the GraphQL body is not JSON: %v", err)
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		reply := func(data any) {
			out := map[string]any{"data": data}
			if len(b.errs) > 0 {
				var es []map[string]string
				for _, e := range b.errs {
					es = append(es, map[string]string{"message": e, "type": "FORBIDDEN"})
				}
				out = map[string]any{"data": nil, "errors": es}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out)
		}
		switch {
		case strings.Contains(req.Query, "projectV2(number"):
			b.calls = append(b.calls, "project")
			b.owner = "user"
			if strings.Contains(req.Query, "organization(login") {
				b.owner = "organization"
			}
			session := map[string]any{"id": "F-session", "name": "Session", "dataType": b.sessionTy}
			if b.sessionTy == "SINGLE_SELECT" {
				session["options"] = []map[string]string{{"id": "O-sonnet", "name": "Claude Code Sonnet"}}
			}
			if b.noProject {
				reply(map[string]any{b.owner: map[string]any{"projectV2": nil}})
				return
			}
			reply(map[string]any{b.owner: map[string]any{"projectV2": map[string]any{
				"id": "P1",
				"fields": map[string]any{"nodes": []any{
					map[string]any{"id": "F-title", "name": "Title", "dataType": "TITLE"},
					map[string]any{"id": "F-status", "name": "Status", "dataType": "SINGLE_SELECT", "options": []map[string]string{
						{"id": "O-needs", "name": "Needs you"},
						{"id": "O-prog", "name": "In progress"},
						{"id": "O-ready", "name": "Ready to push"},
						{"id": "O-done", "name": "Done"},
					}},
					session,
					map[string]any{"id": "F-task", "name": "Task", "dataType": "TEXT"},
				}},
			}}})
		case strings.Contains(req.Query, "items(first: 100"):
			b.calls = append(b.calls, "queue")
			page := 0
			if after, ok := req.Variables["after"].(string); ok && after != "" {
				page, _ = strconv.Atoi(strings.TrimPrefix(after, "cursor"))
			}
			next := page+1 < len(b.pages)
			var nodes []any
			if page < len(b.pages) {
				nodes = b.pages[page]
			}
			reply(map[string]any{"node": map[string]any{"items": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": next, "endCursor": "cursor" + strconv.Itoa(page+1)},
				"nodes":    nodes,
			}}})
		case strings.Contains(req.Query, "projectItems"):
			b.calls = append(b.calls, "issue")
			items := []map[string]any{{"id": "I-other", "project": map[string]string{"id": "P-other"}}}
			if b.card {
				items = append(items, map[string]any{"id": "I1", "project": map[string]string{"id": "P1"}})
			}
			reply(map[string]any{"repository": map[string]any{"issue": map[string]any{"id": "ISSUE-7", "projectItems": map[string]any{"nodes": items}}}})
		case strings.Contains(req.Query, "addProjectV2ItemById"):
			b.calls = append(b.calls, "add")
			reply(map[string]any{"addProjectV2ItemById": map[string]any{"item": map[string]string{"id": "I-new"}}})
		case strings.Contains(req.Query, "updateProjectV2ItemFieldValue"):
			v, _ := json.Marshal(req.Variables["value"])
			b.calls = append(b.calls, "set:"+req.Variables["field"].(string)+"="+string(v)+"@"+req.Variables["item"].(string))
			reply(map[string]any{"updateProjectV2ItemFieldValue": map[string]any{"projectV2Item": map[string]string{"id": "x"}}})
		default:
			t.Errorf("an unexpected GraphQL query: %s", req.Query)
			http.Error(w, "unexpected", 400)
		}
	}
}

func boardRig(t *testing.T, mut func(*fakeBoard, *Config)) (*Client, *fakeBoard, *fakeGitHub) {
	t.Helper()
	now := func() time.Time { return time.Unix(1_800_000_000, 0) }
	f := newFake(t, now)
	fb := &fakeBoard{sessionTy: "TEXT"}
	f.handlers["POST /graphql"] = fb.handler(t, f)
	c := f.client(t, func(cfg *Config) {
		cfg.Board = &BoardConfig{Owner: "wstein", Number: 6}
		if mut != nil {
			mut(fb, cfg)
		}
	})
	return c, fb, f
}

var update = forge.CardUpdate{Status: forge.StatusNeedsYou, Session: "docs/runtime", Link: "https://whr.example.test/tasks/t1"}

func TestACardIsAddedAndItsFieldsAreWritten(t *testing.T) {
	c, fb, f := boardRig(t, nil)
	if err := c.UpdateCard(bg, "wstein/workharbor", 7, update); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"project", "issue", "add",
		`set:F-status={"singleSelectOptionId":"O-needs"}@I-new`,
		`set:F-session={"text":"docs/runtime"}@I-new`,
		`set:F-task={"text":"https://whr.example.test/tasks/t1"}@I-new`,
	}
	if strings.Join(fb.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(fb.calls, "\n"), strings.Join(want, "\n"))
	}
	if fb.owner != "user" {
		t.Errorf("queried %s, want a user project", fb.owner)
	}
	// the project is read once: the second update goes straight to the card
	fb.calls = nil
	fb.card = true
	if err := c.UpdateCard(bg, "wstein/workharbor", 7, forge.CardUpdate{Status: forge.StatusDone}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fb.calls, "\n"); got != "issue\nset:F-status={\"singleSelectOptionId\":\"O-done\"}@I1" {
		t.Errorf("second update:\n%s", got)
	}
	// the App's board permission is in the token request
	if body := f.bodies["POST /app/installations/7/access_tokens"]; !strings.Contains(body, `"organization_projects":"write"`) {
		t.Errorf("the installation token was asked for %s", body)
	}
}

func TestAnOrganizationsProjectAndASingleSelectSession(t *testing.T) {
	c, fb, _ := boardRig(t, func(b *fakeBoard, cfg *Config) {
		b.sessionTy, b.card = "SINGLE_SELECT", true
		cfg.Board = &BoardConfig{Owner: "acme", Organization: true, Number: 3}
	})
	// a session that is an option of the board's list is set; one that is not is left
	if err := c.UpdateCard(bg, "wstein/workharbor", 7, forge.CardUpdate{Status: forge.StatusInProgress, Session: "Claude Code Sonnet"}); err != nil {
		t.Fatal(err)
	}
	if err := c.UpdateCard(bg, "wstein/workharbor", 7, forge.CardUpdate{Status: forge.StatusInProgress, Session: "docs/runtime"}); err != nil {
		t.Fatal(err)
	}
	if fb.owner != "organization" {
		t.Errorf("queried %s, want an organization project", fb.owner)
	}
	var sets []string
	for _, c := range fb.calls {
		if strings.HasPrefix(c, "set:F-session") {
			sets = append(sets, c)
		}
	}
	if len(sets) != 1 || !strings.Contains(sets[0], `"singleSelectOptionId":"O-sonnet"`) {
		t.Errorf("session writes %v, want only the option that exists", sets)
	}
}

func TestAMissingOptionOrFieldWritesNothing(t *testing.T) {
	c, fb, _ := boardRig(t, nil)
	err := c.UpdateCard(bg, "wstein/workharbor", 7, forge.CardUpdate{Status: "Blocked"})
	if !errors.Is(err, ErrBoard) || !strings.Contains(err.Error(), `no option "Blocked"`) {
		t.Fatalf("err = %v", err)
	}
	if len(fb.calls) != 1 || fb.calls[0] != "project" {
		t.Errorf("calls %v: nothing may be written for a status the board lacks", fb.calls)
	}

	c2, _, _ := boardRig(t, func(_ *fakeBoard, cfg *Config) { cfg.Board.StatusField = "Stage" })
	if err := c2.UpdateCard(bg, "wstein/workharbor", 7, update); !errors.Is(err, ErrBoard) || !strings.Contains(err.Error(), `"Stage"`) {
		t.Errorf("a missing status field: %v", err)
	}
}

// GraphQL says most failures with a 200: they are errors, the project is read
// again afterwards, and a token never reaches the message.
func TestGraphQLErrorsAreBoardErrors(t *testing.T) {
	rd := redact.New()
	c, fb, _ := boardRig(t, func(b *fakeBoard, cfg *Config) {
		cfg.Redactor = rd
		b.errs = []string{"Resource not accessible by integration"}
	})
	err := c.UpdateCard(bg, "wstein/workharbor", 7, update)
	if !errors.Is(err, ErrBoard) || !strings.Contains(err.Error(), "Resource not accessible") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "ghs_") {
		t.Errorf("an installation token is in the error: %v", err)
	}
	fb.errs = nil
	fb.calls = nil
	if err := c.UpdateCard(bg, "wstein/workharbor", 7, update); err != nil {
		t.Fatalf("after the failure: %v", err)
	}
	if fb.calls[0] != "project" {
		t.Errorf("calls %v: the project must be read again after a failure", fb.calls)
	}
}

func TestOnlyConfiguredRepositoriesAndABoardAreUsed(t *testing.T) {
	c, fb, _ := boardRig(t, nil)
	if err := c.UpdateCard(bg, "evil/other", 7, update); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("another repository = %v, want ErrNotAllowed", err)
	}
	if err := c.UpdateCard(bg, "wstein/workharbor", 0, update); !errors.Is(err, ErrBoard) {
		t.Errorf("issue 0 = %v", err)
	}
	if len(fb.calls) != 0 {
		t.Errorf("calls %v for refused updates", fb.calls)
	}
	now := func() time.Time { return time.Unix(1_800_000_000, 0) }
	plain := newFake(t, now).client(t, nil)
	if err := plain.UpdateCard(bg, "wstein/workharbor", 7, update); err == nil {
		t.Error("a client without a board updated one")
	}
	for _, bad := range []BoardConfig{{Owner: "../x", Number: 1}, {Owner: "wstein", Number: 0}, {Owner: "wstein", Number: 1, StatusField: "a\nb"}} {
		f := newFake(t, now)
		f.handlers["POST /graphql"] = func(http.ResponseWriter, *http.Request) {}
		b := bad
		cfg := Config{AppID: 4242, Key: key(t), Repos: []string{"wstein/workharbor"}, BaseURL: f.ts.URL, Board: &b}
		if _, err := New(cfg); err == nil {
			t.Errorf("board %+v accepted", bad)
		}
	}
}

func TestWithABoardTheAppIsExpectedToHaveItsPermission(t *testing.T) {
	c, _, f := boardRig(t, nil)
	perms := AppPermissionsFor(true)
	f.handlers["GET /app"] = func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 200, map[string]any{"id": 4242, "slug": "s", "permissions": perms})
	}
	f.handlers["GET /repos/wstein/workharbor/installation"] = func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 200, map[string]any{"id": 7, "permissions": AppPermissions()}) // the installation was not updated
	}
	rep, err := c.CheckApp(bg)
	if err != nil || rep.OK() || len(rep.Repos[0].Problems) != 1 || !strings.Contains(rep.Repos[0].Problems[0], BoardPermission) {
		t.Errorf("report %+v, %v: want the missing board permission named", rep, err)
	}
	if p := AppPermissionsFor(false); len(p) != 4 || p[BoardPermission] != "" {
		t.Errorf("without a board the permissions are %v", p)
	}
}

// GitHub refusing the App the board is a typed error, which is not the same as
// a board that is merely misconfigured.
func TestARefusedBoardIsReportedAsNotWritable(t *testing.T) {
	c, fb, _ := boardRig(t, func(b *fakeBoard, _ *Config) { b.errs = []string{"Resource not accessible by integration"} })
	err := c.UpdateCard(bg, "wstein/workharbor", 7, update)
	if !errors.Is(err, ErrBoardNotWritable) || !errors.Is(err, ErrBoard) {
		t.Fatalf("err = %v, want ErrBoard and ErrBoardNotWritable", err)
	}
	fb.errs = nil

	// a project the token cannot see is not found, which looks the same
	c2, fb2, _ := boardRig(t, nil)
	fb2.noProject = true
	if _, err := c2.CheckBoard(bg); !errors.Is(err, ErrBoardNotWritable) {
		t.Errorf("a project that is not visible = %v, want ErrBoardNotWritable", err)
	}

	// a configuration error is not a refusal
	c3, _, _ := boardRig(t, nil)
	err = c3.UpdateCard(bg, "wstein/workharbor", 7, forge.CardUpdate{Status: "Blocked"})
	if errors.Is(err, ErrBoardNotWritable) {
		t.Errorf("a missing option was reported as not writable: %v", err)
	}
}

func TestCheckBoardReadsTheProjectWithoutWriting(t *testing.T) {
	c, fb, _ := boardRig(t, nil)
	rep, err := c.CheckBoard(bg)
	if err != nil || len(rep.MissingStatuses) != 0 || !rep.Session || !rep.Link {
		t.Fatalf("%+v, %v", rep, err)
	}
	if len(fb.calls) != 1 || fb.calls[0] != "project" {
		t.Errorf("calls %v: a check only reads", fb.calls)
	}
	c2, _, _ := boardRig(t, func(_ *fakeBoard, cfg *Config) { cfg.Board.SessionField, cfg.Board.LinkField = "Agent", "Url" })
	if rep, err := c2.CheckBoard(bg); err != nil || rep.Session || rep.Link {
		t.Errorf("fields the project lacks: %+v, %v", rep, err)
	}
	c3, _, _ := boardRig(t, func(_ *fakeBoard, cfg *Config) { cfg.Board.StatusField = "Stage" })
	if rep, err := c3.CheckBoard(bg); err != nil || len(rep.MissingStatuses) != 4 {
		t.Errorf("no status field: %+v, %v", rep, err)
	}
}

// The board has its own token: an installation that lacks the board's permission
// loses the board and nothing else.
func TestTheBoardHasItsOwnToken(t *testing.T) {
	c, _, f := boardRig(t, nil)
	f.handlers["GET /repos/wstein/workharbor/issues/7"] = func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 200, map[string]any{"number": 7, "title": "T", "body": "B", "author_association": "OWNER", "user": map[string]string{"login": "wstein"}})
	}
	if _, err := c.GetIssue(bg, "wstein/workharbor", 7); err != nil {
		t.Fatal(err)
	}
	if body := f.bodies["POST /app/installations/7/access_tokens"]; strings.Contains(body, BoardPermission) {
		t.Errorf("the ordinary token was asked for the board's permission: %s", body)
	}

	// an installation that has not accepted the permission refuses the board's token
	f.denyBoardMint = true
	c2 := f.client(t, func(cfg *Config) { cfg.Board = &BoardConfig{Owner: "wstein", Number: 6} })
	err := c2.UpdateCard(bg, "wstein/workharbor", 7, update)
	if !errors.Is(err, ErrBoardPermission) || !errors.Is(err, ErrBoardNotWritable) || !errors.Is(err, ErrBoard) {
		t.Fatalf("a refused board token = %v, want ErrBoardPermission", err)
	}
	if _, err := c2.GetIssue(bg, "wstein/workharbor", 7); err != nil {
		t.Errorf("a board refusal broke issues: %v", err)
	}
}

func queueItem(repo string, number int, status, session string, at string) map[string]any {
	values := []any{map[string]any{"name": status, "field": map[string]string{"name": "Status"}}}
	if session != "" {
		values = append(values, map[string]any{"text": session, "field": map[string]string{"name": "Session"}})
	}
	return map[string]any{
		"updatedAt":   at,
		"content":     map[string]any{"number": number, "repository": map[string]string{"nameWithOwner": repo}},
		"fieldValues": map[string]any{"nodes": values},
	}
}

// The cards in the queue column are read as the App, across pages, with what their
// Session says; other columns, other repositories, drafts and pull requests are left out.
func TestQueuedCardsAreReadFromTheNamedColumn(t *testing.T) {
	c, fb, _ := boardRig(t, nil)
	draft := map[string]any{"updatedAt": "2026-10-02T10:00:00Z", "content": map[string]any{}, "fieldValues": map[string]any{"nodes": []any{map[string]any{"name": "Agent queue", "field": map[string]string{"name": "Status"}}}}}
	fb.pages = [][]any{
		{queueItem("wstein/workharbor", 7, "Agent queue", "docs/code", "2026-10-02T12:00:00Z"), queueItem("wstein/workharbor", 8, "Todo", "", "2026-10-02T12:01:00Z"), draft},
		{queueItem("wstein/other", 9, "Agent queue", "", "2026-10-02T12:02:00Z"), queueItem("wstein/workharbor", 10, "Agent queue", "", "2026-10-02T12:03:00Z")},
	}
	got, err := c.QueuedCards(bg, "Agent queue")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Issue != 7 || got[0].Agent != "docs/code" || got[0].Repo != "wstein/workharbor" || got[0].Mover != "" || got[1].Issue != 10 || got[1].Agent != "" {
		t.Fatalf("cards = %+v", got)
	}
	if !got[0].UpdatedAt.Equal(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("updated at = %v", got[0].UpdatedAt)
	}
	// nothing is asked for an empty column name, and without a board it is an error
	if cards, err := c.QueuedCards(bg, ""); err != nil || cards != nil {
		t.Errorf("an empty status: %v %v", cards, err)
	}
	noBoard := newFake(t, func() time.Time { return time.Unix(1_800_000_000, 0) }).client(t, nil)
	if _, err := noBoard.QueuedCards(bg, "Agent queue"); err == nil {
		t.Error("no board configured but no error")
	}
	// a board the App cannot read is an error, not an empty queue
	c2, fb2, _ := boardRig(t, nil)
	fb2.errs = []string{"Resource not accessible by integration"}
	if _, err := c2.QueuedCards(bg, "Agent queue"); !errors.Is(err, ErrBoard) {
		t.Errorf("an unreadable board: %v", err)
	}
}
