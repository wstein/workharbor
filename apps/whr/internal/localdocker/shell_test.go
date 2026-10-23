// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLocalShellInspectArgvIsExact(t *testing.T) {
	id := strings.Repeat("a", 64)
	c := Client{Executable: fakeDocker(t, `printf '%s\n' "$@"`)}
	raw, err := c.PreflightInspect(context.Background(), "unix:///test/socket", id)
	want := strings.Join([]string{"--host=unix:///test/socket", "container", "inspect", "--format", "{{json .}}", id, ""}, "\n")
	if err != nil || string(raw) != want {
		t.Fatalf("%q %v", raw, err)
	}
	if _, err := c.PreflightInspect(context.Background(), "unix:///test/socket", "--help"); err == nil {
		t.Fatal("non-full ID admitted")
	}
}
func TestLocalShellLivenessDistinguishesDaemonReply(t *testing.T) {
	id := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, body string
		reachable  bool
	}{
		{"running", `printf 'true\n'`, true}, {"stopped", `printf 'false\n'`, true},
		{"gone", `printf 'Error response from daemon: No such container: ` + id + `\n' >&2;exit 1`, true},
		{"legacygone", `printf 'Error: No such object: ` + id + `\n' >&2;exit 1`, true},
		{"daemonerror", `printf 'Error response from daemon: Internal Server Error\n' >&2;exit 1`, true},
		{"transport", `printf 'Cannot connect to the Docker daemon\n' >&2;exit 1`, false},
		{"unknown", `printf 'unclassified failure\n' >&2;exit 1`, false},
		{"invalidresponse", `printf 'garbage\n'`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `test "$#" -eq 6 && test "$1" = '--host=unix:///test/socket' && test "$2" = container && test "$3" = inspect && test "$4" = --format && test "$5" = '{{.State.Running}}' && test "$6" = '` + id + `' || exit 99
` + tc.body
			got := (Client{Executable: fakeDocker(t, body)}).Reachable(context.Background(), "unix:///test/socket", id)
			if got != tc.reachable {
				t.Fatalf("got %v", got)
			}
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if (Client{Executable: fakeDocker(t, `/bin/sleep 60`)}).Reachable(ctx, "unix:///test/socket", id) {
		t.Fatal("timeout reported reachable")
	}
}
func TestLocalSnapshotPinsOneDoctorEndpoint(t *testing.T) {
	p := socketPorts()
	var dials []string
	p.DialUnix = func(_ context.Context, s string) error { dials = append(dials, s); return nil }
	c := fixtureDocker(t, `{"Id":"`+strings.Repeat("a", 64)+`","Name":"/sample","State":"running","Labels":{"workharbor.workspace":"x"}}`)
	snap, err := c.Snapshot(context.Background(), p)
	if err != nil || snap.Endpoint != "unix:///var/run/docker.sock" || len(snap.Workspaces) != 1 || !reflect.DeepEqual(dials, []string{"/var/run/docker.sock"}) {
		t.Fatalf("%+v %v dials=%v", snap, err, dials)
	}
}
