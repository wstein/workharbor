// SPDX-License-Identifier: EUPL-1.2

package commands_test

import (
	"context"
	"io/fs"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
)

// fakePorts is a machine with a Docker socket, a good Git and plenty of disk;
// nothing here touches the real host.
func fakePorts() doctor.Ports {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	return doctor.Ports{
		OS: "linux", Arch: "amd64",
		Getenv: func(string) string { return "" },
		Home:   func() (string, error) { return "/home/alice", nil },
		User:   func() string { return "alice" },
		Host:   func() string { return "devbox" },
		Getwd:  func() (string, error) { return "/work", nil },
		Now:    func() time.Time { return now },
		Lstat: func(p string) (fs.FileInfo, error) {
			return nil, &fs.PathError{Op: "lstat", Path: p, Err: syscall.ENOENT}
		},
		DialUnix: func(_ context.Context, p string) error {
			if p == "/var/run/docker.sock" {
				return nil
			}
			return &fs.PathError{Op: "connect", Path: p, Err: syscall.ENOENT}
		},
		ReadFile: func(p string, _ int64) ([]byte, error) {
			return nil, &fs.PathError{Op: "open", Path: p, Err: syscall.ENOENT}
		},
		CanRead:    func(string) error { return nil },
		FindGit:    func() (string, error) { return "/usr/bin/git", nil },
		GitVersion: func(context.Context, string) (string, error) { return "git version 2.43.0", nil },
		FreeBytes:  func(string) (uint64, error) { return 10 << 30, nil },
	}
}

// brokenSocket is fakePorts without any socket.
func brokenSocket() doctor.Ports {
	p := fakePorts()
	p.DialUnix = func(_ context.Context, path string) error {
		return &fs.PathError{Op: "connect", Path: path, Err: syscall.ENOENT}
	}
	return p
}

func baseHandlers() map[string]commands.Handler {
	h := commands.DefaultHandlers()
	h["host.doctor"] = commands.DoctorHandler(fakePorts())
	return h
}

func withDoctor(p doctor.Ports) map[string]commands.Handler {
	h := baseHandlers()
	h["host.doctor"] = commands.DoctorHandler(p)
	return h
}

func TestDoctorExitCodes(t *testing.T) {
	// incomplete (skipped checks remain) exits 0 and writes the report to stdout only.
	for _, sp := range [][]string{{"doctor"}, {"host", "doctor"}} {
		got := run(t, withDoctor(fakePorts()), sp...)
		if got.code != 0 || got.err != "" || !strings.Contains(got.stdout, "verdict: incomplete") {
			t.Errorf("%v: %+v", sp, got)
		}
	}
	// a failed check exits 68, keeps the report on stdout and announces on stderr.
	got := run(t, withDoctor(brokenSocket()), "doctor")
	if got.code != 68 || !strings.Contains(got.stdout, "verdict: failed") || !strings.Contains(got.stdout, "container.socket") ||
		!strings.HasPrefix(got.err, "whr: ") || !strings.Contains(got.err, "container.socket") {
		t.Errorf("failed: %+v", got)
	}
	// JSON: one envelope, the report as the result of a conflict failure.
	got = run(t, withDoctor(brokenSocket()), "doctor", "--json")
	env := decodeEnvelope(t, got.stdout)
	res, _ := env["result"].(map[string]any)
	errObj, _ := env["error"].(map[string]any)
	if got.code != 68 || env["ok"] != false || env["command"] != "host.doctor" || res["verdict"] != "failed" || errObj["code"] != "conflict" {
		t.Errorf("failed json: %+v", got)
	}
	got = run(t, withDoctor(fakePorts()), "doctor", "--json")
	env = decodeEnvelope(t, got.stdout)
	if got.code != 0 || env["ok"] != true || env["result"].(map[string]any)["verdict"] != "incomplete" {
		t.Errorf("incomplete json: %+v", got)
	}
	// doctor takes no arguments
	if got := run(t, withDoctor(fakePorts()), "doctor", "extra"); got.code != 64 || got.stdout != "" {
		t.Errorf("arguments: %+v", got)
	}
}

func TestDoctorJSONMatchesSchema(t *testing.T) {
	schema := compileSchema(t)
	for name, p := range map[string]doctor.Ports{"incomplete": fakePorts(), "failed": brokenSocket()} {
		got := run(t, withDoctor(p), "doctor", "--json")
		doc, err := jsonUnmarshal(got.stdout)
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(doc); err != nil {
			t.Errorf("%s: %v\n%s", name, err, got.stdout)
		}
	}
}

// TestMutatingHostCommandsRefuseUnsupportedPlatform is table-level: the
// mutating host commands are unavailable until T015, so they refuse before any I/O.
func TestMutatingHostCommandsRefuseUnsupportedPlatform(t *testing.T) {
	n := 0
	for _, e := range commands.Table() {
		if e.Writes != "host" {
			continue
		}
		n++
		got := run(t, withDoctor(fakePorts()), e.Path...)
		if got.code != 64 || got.stdout != "" || !strings.Contains(got.err, e.ID+" is not available") {
			t.Errorf("%s: %+v", e.ID, got)
		}
	}
	if n == 0 {
		t.Fatal("no mutating host command in the table")
	}
}
