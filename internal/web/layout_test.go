package web

import (
	"regexp"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/store"
)

// The two layouts of design §9.6 come from one server-rendered page: the page
// carries the task list beside the task, and the stylesheet shows it only on a
// wide screen in landscape.
func TestTheTaskPageCarriesTheTaskListForTheTabletsTwoPanes(t *testing.T) {
	r := newRig(t)
	r.be.tasks = []store.TaskSummary{
		{ID: "t1", Repo: "o/a", Issue: "#1", State: domain.TaskRunning},
		{ID: "t2", Repo: "o/b", Issue: "#2", State: domain.TaskAwaitingGuidance},
	}
	r.be.show = func(id domain.ID) (service.TaskView, error) {
		return service.TaskView{Task: domain.Task{ID: id, Repo: "o/a", Issue: "#1", State: domain.TaskRunning}, Agent: "w/a"}, nil
	}
	b := r.browser()
	b.signIn()
	_, page := b.do("GET", "/tasks/t1", nil)
	for _, want := range []string{
		`class="panes"`, `<aside class="tasklist" aria-label="Tasks">`,
		`href="/tasks/t2"`, `href="/tasks/t1" aria-current="page"`, // both tasks, this one marked
		`<h1>o/a#1</h1>`, `id="transcript"`, `id="message"`, // the task itself stays in the same page
		`name="viewport" content="width=device-width, initial-scale=1"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the task page lacks %q", want)
		}
	}
	// the one that needs you comes first in the list
	if strings.Index(page, `href="/tasks/t2"`) > strings.Index(page, `href="/tasks/t1" aria-current`) {
		t.Error("the task that needs you is not first in the list")
	}
	// with one task there is nothing to switch to
	r.be.tasks = r.be.tasks[:1]
	if _, page = b.do("GET", "/tasks/t1", nil); strings.Contains(page, `class="tasklist"`) {
		t.Error("a list of one task is shown")
	}
}

func (r *rig) css() string {
	r.t.Helper()
	b := r.browser()
	resp, body := b.do("GET", "/static/app.css", nil)
	if resp.StatusCode != 200 {
		r.t.Fatalf("app.css: %d", resp.StatusCode)
	}
	return body
}

// block returns the declarations of the first rule whose selector is exactly sel.
func block(t *testing.T, css, sel string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(sel) + `\s*\{([^}]*)\}`)
	m := re.FindStringSubmatch(css)
	if m == nil {
		t.Fatalf("no rule for %q", sel)
	}
	return m[1]
}

// Touch targets are at least 44 points and nothing is hover-only (D35, §9.6), and
// the phone's single column and the tablet's two panes are in the one stylesheet.
func TestTheStylesheetKeepsTouchTargetsAndBothLayouts(t *testing.T) {
	css := newRig(t).css()
	for _, sel := range []string{"input, select, textarea, button, .button", ".tab", ".card a"} {
		if !strings.Contains(block(t, css, sel), "min-height: 44px") {
			t.Errorf("%q has no min-height of 44px", sel)
		}
	}
	// the phone's layout is the default: the list beside the task is hidden
	if !strings.Contains(block(t, css, ".tasklist"), "display: none") {
		t.Error("the task list is not hidden by default (the phone's single column)")
	}
	// the tablet's two panes need a wide screen and landscape, and place the list beside the task
	m := regexp.MustCompile(`@media \(min-width: 56rem\) and \(orientation: landscape\) \{([\s\S]*?)\n\}`).FindStringSubmatch(css)
	if m == nil {
		t.Fatal("no media query for a wide screen in landscape")
	}
	if !strings.Contains(m[1], "main > .panes") || !strings.Contains(m[1], "grid-template-columns: minmax(14rem, 20rem) minmax(0, 1fr)") || !strings.Contains(m[1], ".tasklist { display: block") {
		t.Errorf("the two panes are not laid out in the media query:\n%s", m[1])
	}
	// no control exists only on hover: a :hover rule may restyle, never show or hide
	for _, rule := range regexp.MustCompile(`[^{}]*:hover[^{]*\{[^}]*\}`).FindAllString(css, -1) {
		if strings.Contains(rule, "display") || strings.Contains(rule, "visibility") || strings.Contains(rule, "opacity") {
			t.Errorf("a hover-only reveal: %s", strings.TrimSpace(rule))
		}
	}
}

// The keyboard shortcuts are in the script served from the app's own origin, and
// every page that has a session lists them; they are written for the tablet's
// keyboard and never answer anything.
func TestTheKeyboardShortcutsAreServedAndListed(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	b.signIn()
	_, js := b.do("GET", "/static/app.js", nil)
	for _, want := range []string{`case "j"`, `case "k"`, `case "m"`, `case "?"`, `case "g"`, `a[data-shortcut=`, `e.ctrlKey || e.metaKey`, "requestSubmit"} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
	for _, bad := range []string{"eval(", "innerHTML", "new Function", "fetch(", "XMLHttpRequest"} {
		if strings.Contains(js, bad) {
			t.Errorf("app.js uses %q: shortcuts only move focus and follow links", bad)
		}
	}
	for _, path := range []string{"/", "/inbox", "/devices"} {
		_, page := b.do("GET", path, nil)
		for _, want := range []string{`id="shortcuts"`, `data-shortcut="h"`, `data-shortcut="i"`, `data-shortcut="d"`, `data-shortcut="c"`, "Keyboard shortcuts"} {
			if !strings.Contains(page, want) {
				t.Errorf("%s lacks %q", path, want)
			}
		}
	}
	// the sign-in page has no session and no shortcut list
	if _, page := r.browser().do("GET", "/login", nil); strings.Contains(page, `id="shortcuts"`) {
		t.Error("the sign-in page lists shortcuts")
	}
}
