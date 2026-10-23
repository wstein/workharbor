// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
)

func fixtureDocker(t *testing.T, records string) Client {
	t.Helper()
	exe := fakeDocker(t, `case "$2 $3" in
 'container ls') printf '%s\n' '`+`{"ID":"`+strings.Repeat("a", 64)+`","Status":"Up 1 hour"}`+`';;
 'container inspect')
 test "$#" -eq 6 && test "$1" = '--host=unix:///var/run/docker.sock' && test "$4" = '--format' && test "$5" = '`+expectedInspectFormat+`' && test "$6" = '`+strings.Repeat("a", 64)+`' || exit 98
 cat <<'JSON'
`+records+`
JSON
 ;;
 *) exit 99;;
esac`)
	return Client{Executable: exe}
}
func socketPorts() doctor.Ports {
	return doctor.Ports{DialUnix: func(context.Context, string) error { return nil }}
}
func TestLocalLsListsLabelledContainersAndVolumes(t *testing.T) {
	records := fmt.Sprintf(`{"Id":%q,"Name":"/sample","Image":"ubuntu:test","State":"running","Labels":{"devcontainer.local_folder":"/host/project"},"Mounts":[{"Type":"volume","Name":"home","Destination":"/home/captain","RW":true},{"Type":"volume","Name":"ro","Destination":"/read-only","RW":false},{"Type":"volume","Name":"tools","Destination":"/opt/bdock/go","RW":true},{"Type":"volume","Name":"bundledock-extra","Destination":"/tools","RW":true}]}`, strings.Repeat("a", 64))
	got, err := fixtureDocker(t, records).Discover(context.Background(), socketPorts())
	if err != nil || len(got) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	w := got[0]
	if w.ID != strings.Repeat("a", 64) || w.Name != "sample" || w.Role != "primary" || w.Image != "ubuntu:test" || w.State != "running" || w.Status != "Up 1 hour" || w.WorkspaceFolder != "/host/project" || w.HomeVolume == nil || *w.HomeVolume != "home" || strings.Join(w.ProtectedVolumes, ",") != "bundledock-extra,ro,tools" {
		t.Fatalf("%+v", w)
	}
}
func TestLocalLsIncludesStoppedAndExcludesForeign(t *testing.T) {
	for _, label := range []string{"anchorage-staging", "anchorage-temp", "devcontainer.local_folder", "devcontainer.config_file", "workharbor.workspace", "foreign"} {
		t.Run(label, func(t *testing.T) {
			records := fmt.Sprintf(`{"Id":%q,"Name":"/stopped","State":"exited","Labels":{%q:"1"},"Mounts":[]}`, strings.Repeat("a", 64), label)
			got, err := fixtureDocker(t, records).Discover(context.Background(), socketPorts())
			if err != nil {
				t.Fatal(err)
			}
			if label == "foreign" {
				if len(got) != 0 {
					t.Fatal("foreign returned")
				}
				return
			}
			if len(got) != 1 || got[0].State != "exited" || got[0].HomeVolume != nil {
				t.Fatalf("%+v", got)
			}
			role := "primary"
			if label == "anchorage-staging" {
				role = "staging"
			}
			if label == "anchorage-temp" {
				role = "temp"
			}
			if got[0].Role != role {
				t.Fatalf("role=%s", got[0].Role)
			}
		})
	}
}
func TestLocalLsEscapesHostileMetadata(t *testing.T) {
	record := map[string]any{"Id": strings.Repeat("a", 64), "Name": "/bad\x1b[31m\u202e", "Image": "$(touch never)", "State": "exited", "Labels": map[string]string{"workharbor.workspace": "x\nlabel"}}
	raw, _ := json.Marshal(record)
	got, err := fixtureDocker(t, string(raw)).Discover(context.Background(), socketPorts())
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
	human := Human(got)
	rows := strings.Split(strings.TrimSpace(human), "\n")
	if len(rows) != 2 || strings.Split(rows[1], "\t")[0] != strings.Repeat("a", 12) {
		t.Fatalf("human ID must be exactly 12 characters: %q", human)
	}
	if strings.ContainsAny(human, "\x1b\u202e") || !strings.Contains(human, "<U+001B>") || !strings.Contains(human, "<U+202E>") || !strings.Contains(human, strings.Repeat("a", 12)) {
		t.Fatalf("unsafe/missing human fields: %q", human)
	}
}
func TestLocalDiscoveryTotalDeadlineIncludesSocketAndCommands(t *testing.T) {
	p := socketPorts()
	p.DialUnix = func(ctx context.Context, _ string) error {
		d, _ := ctx.Deadline()
		if time.Until(d) > 501*time.Millisecond {
			t.Fatal("dial cap missing")
		}
		time.Sleep(30 * time.Millisecond)
		return nil
	}
	// The inherited earlier deadline must include socket selection, not restart
	// at the Docker boundary. A child deliberately never produces a response.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := (Client{Executable: fakeDocker(t, "/bin/sleep 60")}).Discover(ctx, p)
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("shared deadline lost: %v", err)
	}
}
func TestLocalLsRejectsMalformedDockerRecords(t *testing.T) {
	for _, raw := range []string{`garbage`, `{"Id":"short","Labels":{"workharbor.workspace":"x"}}`, fmt.Sprintf(`{"Id":%q,"Labels":{"workharbor.workspace":"x"}}`, strings.Repeat("b", 64))} {
		if _, err := fixtureDocker(t, raw).Discover(context.Background(), socketPorts()); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestLocalLsBindAndLegacyVolumeMetadata(t *testing.T) {
	raw := fmt.Sprintf(`{"Id":%q,"Name":"/legacy","State":"exited","Labels":{"devcontainer.config_file":"/host/project/.devcontainer/devcontainer.json"},"Mounts":[{"Type":"bind","Source":"/host/project","Destination":"/workspace","RW":true},{"Type":"bind","Source":"/toolbox","Destination":"/opt/bdock/go","RW":false},{"Type":"volume","Name":"legacy-home","Destination":"/home/vscode","RW":true}]}`, strings.Repeat("a", 64))
	got, err := fixtureDocker(t, raw).Discover(context.Background(), socketPorts())
	if err != nil || len(got) != 1 || got[0].WorkspaceFolder != "/host/project" || got[0].HomeVolume == nil || *got[0].HomeVolume != "legacy-home" {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestLocalLsEmptyDaemonAvoidsInspect(t *testing.T) {
	client := Client{Executable: fakeDocker(t, `test "$2 $3" = 'container ls' || exit 99`)}
	got, err := client.Discover(context.Background(), socketPorts())
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestLocalDiscoveryOwnBudgetStopsDocker(t *testing.T) {
	client := Client{Executable: fakeDocker(t, `/bin/sleep 60 & wait`)}
	start := time.Now()
	_, err := client.Discover(context.Background(), socketPorts())
	if err == nil || time.Since(start) > 5*time.Second+200*time.Millisecond {
		t.Fatalf("discovery exceeded total budget: %v (%v)", err, time.Since(start))
	}
}
func TestLocalDockerRefusesMutationArgv(t *testing.T) {
	client := Client{Executable: fakeDocker(t, `echo should-not-run`)}
	for _, args := range [][]string{{"container", "rm", "--force", strings.Repeat("a", 64)}, {"volume", "ls"}, {"container", "inspect", "--format", inspectFormat, ";touch bad"}} {
		if raw, err := client.read(context.Background(), "unix:///test/socket", args...); err == nil || len(raw) != 0 {
			t.Fatalf("non-discovery vector ran: %v", args)
		}
	}
}
