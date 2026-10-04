package apple

import (
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/runtime"
)

func TestInteractiveCommandAttachesTheCallersTerminalAndCarriesNoFile(t *testing.T) {
	lookPath = func(string) (string, error) { return "/usr/local/bin/container", nil }
	t.Cleanup(func() { lookPath = exec.LookPath })
	bin, argv, err := InteractiveCommand("whr-abc", runtime.InteractiveRequest{
		Cmd: []string{"/bin/sh", "-c", "exec sh -i", "whr-shell", "/tools/bin"},
		Env: []string{"HOME=/home/agent", "HTTPS_PROXY=http://192.168.64.3:3128"},
		Dir: "/home/agent", User: "1000:1000",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/usr/local/bin/container", "exec", "-t", "-i", "-u", "1000:1000", "-w", "/home/agent",
		"-e", "HOME=/home/agent", "-e", "HTTPS_PROXY=http://192.168.64.3:3128", "whr-abc", "/bin/sh", "-c", "exec sh -i", "whr-shell", "/tools/bin",
	}
	if bin != want[0] || !slices.Equal(argv, want) {
		t.Errorf("argv = %q, want %q", argv, want)
	}
	if slices.Contains(argv, "--env-file") || strings.Contains(strings.Join(argv, " "), "/dev/fd") {
		t.Error("an env file or descriptor cannot survive the exec and must not be named")
	}
}

func TestInteractiveCommandRefusesWhatItCannotCarry(t *testing.T) {
	lookPath = func(string) (string, error) { return "/c", nil }
	t.Cleanup(func() { lookPath = exec.LookPath })
	for name, tc := range map[string]struct {
		id  string
		req runtime.InteractiveRequest
	}{
		"no command":       {"e", runtime.InteractiveRequest{}},
		"no id":            {"", runtime.InteractiveRequest{Cmd: []string{"sh"}}},
		"id looks a flag":  {"--detach", runtime.InteractiveRequest{Cmd: []string{"sh"}}},
		"inherit by name":  {"e", runtime.InteractiveRequest{Cmd: []string{"sh"}, Env: []string{"HOME"}}},
		"odd key":          {"e", runtime.InteractiveRequest{Cmd: []string{"sh"}, Env: []string{"A B=1"}}},
		"multi-line value": {"e", runtime.InteractiveRequest{Cmd: []string{"sh"}, Env: []string{"A=1\nB=2"}}},
	} {
		if _, _, err := InteractiveCommand(tc.id, tc.req); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if _, _, err := InteractiveCommand("e", runtime.InteractiveRequest{Cmd: []string{"sh"}}); err == nil {
		t.Error("without the container CLI: no error")
	}
}
