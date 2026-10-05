package web

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/service"
)

func TestTaskShowsAgentMayRunAsEscapedData(t *testing.T) {
	p := taskPageOf(service.TaskView{Task: domain.Task{ID: "t1", State: domain.TaskCancelled}, AgentMayRun: []domain.AgentMayRun{{RunID: "r1", EnvID: "e1", Path: "kill-all", Error: "<script>bad()</script>"}}})
	var b bytes.Buffer
	if err := taskView(p).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Agent may still run", "r1", "e1", "kill-all", "&lt;script&gt;"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(b.String(), "<script>bad()") {
		t.Fatal("untrusted error rendered as markup")
	}
}
