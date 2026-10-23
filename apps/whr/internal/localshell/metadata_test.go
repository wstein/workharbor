// SPDX-License-Identifier: EUPL-1.2
package localshell

import (
	"encoding/json"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"strings"
	"testing"
)

func inspected(t *testing.T, change func(map[string]any)) []byte {
	t.Helper()
	m := map[string]any{"Id": strings.Repeat("a", 64), "Name": "/sample", "State": map[string]any{"Status": "running", "Running": true, "Paused": false, "Restarting": false}, "Config": map[string]any{"User": "", "WorkingDir": "/fallback", "Labels": map[string]string{}}, "Mounts": []any{}}
	if change != nil {
		change(m)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func selected() localdocker.Workspace {
	return localdocker.Workspace{ID: strings.Repeat("a", 64), Name: "sample", State: "running", WorkspaceFolder: "/host/project", Labels: map[string]string{"workharbor.workspace": "x"}}
}
func TestLocalShellUserPrecedence(t *testing.T) {
	for _, tc := range []struct{ label, user, want string }{{"", "", "captain"}, {"", "1000:1001", "1000:1001"}, {`[{"remoteUser":"dev"}]`, "configured", "dev"}, {`{"remoteUser":"dev"}`, "configured", "dev"}, {`[{"remoteUser":"base"},{"remoteUser":"override"}]`, "configured", "override"}, {`[{}]`, "configured", "configured"}} {
		raw := inspected(t, func(m map[string]any) {
			c := m["Config"].(map[string]any)
			c["User"] = tc.user
			if tc.label != "" {
				c["Labels"] = map[string]string{"devcontainer.metadata": tc.label}
			}
		})
		got, err := ParseMetadata(raw, selected(), "xterm-256color")
		if err != nil || got.User != tc.want {
			t.Fatalf("%+v -> %+v %v", tc, got, err)
		}
	}
}
func TestLocalShellDirectoryMountAndFallback(t *testing.T) {
	for _, match := range []bool{false, true} {
		raw := inspected(t, func(m map[string]any) {
			if match {
				m["Mounts"] = []any{map[string]any{"Source": "/host/project", "Destination": "/workspaces/sample"}}
			}
		})
		got, err := ParseMetadata(raw, selected(), "invalid TERM\x1b")
		want := "/fallback"
		if match {
			want = "/workspaces/sample"
		}
		if err != nil || got.Directory != want || (got.DirectorySource == directoryFallback) == match || got.Term != "xterm-256color" {
			t.Fatalf("match=%v: %+v %v", match, got, err)
		}
	}
	for _, n := range []int{4096, 4097} {
		raw := inspected(t, func(m map[string]any) { m["Config"].(map[string]any)["WorkingDir"] = "/" + strings.Repeat("x", n-1) })
		_, err := ParseMetadata(raw, selected(), "screen-256color")
		if (err == nil) != (n == 4096) {
			t.Fatalf("dirbytes=%d %v", n, err)
		}
	}
}
func TestLocalShellRejectsInvalidMetadata(t *testing.T) {
	cases := []func(map[string]any){
		func(m map[string]any) { m["Name"] = "/bad\x1b\u202e" },
		func(m map[string]any) { m["Name"] = "/" + strings.Repeat("n", 129) },
		func(m map[string]any) { m["Id"] = strings.Repeat("b", 64) },
		func(m map[string]any) {
			m["State"] = map[string]any{"Status": "running", "Running": true, "Paused": true}
		},
	}
	for _, value := range []string{"root:root", "BadUser", "0:bad", "captain;touch never", "a\nuser"} {
		v := value
		cases = append(cases, func(m map[string]any) { m["Config"].(map[string]any)["User"] = v })
	}
	for _, value := range []string{"relative", "", "/bad\npath", "/bad\x00path"} {
		v := value
		cases = append(cases, func(m map[string]any) { m["Config"].(map[string]any)["WorkingDir"] = v })
	}
	for _, value := range []string{`garbage`, `null`, `[{"remoteUser":""}]`, `[{"remoteUser":7}]`, `[{"remoteUser":"bad;value"}]`} {
		v := value
		cases = append(cases, func(m map[string]any) {
			m["Config"].(map[string]any)["Labels"] = map[string]string{"devcontainer.metadata": v}
		})
	}
	for i, change := range cases {
		_, err := ParseMetadata(inspected(t, change), selected(), "xterm")
		if exitcode.FromError(err) != 68 || strings.ContainsAny(err.Error(), "\x1b\u202e\n\x00") {
			t.Fatalf("case=%d unsafe/notrefused: %v", i, err)
		}
	}
}

func TestLocalShellMetadataErrorsDoNotPrintEnvironment(t *testing.T) {
	for _, label := range []string{`{"remoteEnv":{"TOKEN":"canary-secret"}}invalid`, `{"remoteUser":{"remoteEnv":{"TOKEN":"canary-secret"}}}`} {
		raw := inspected(t, func(m map[string]any) {
			m["Config"].(map[string]any)["Labels"] = map[string]string{"devcontainer.metadata": label}
		})
		_, err := ParseMetadata(raw, selected(), "xterm")
		if err == nil || strings.Contains(err.Error(), "canary-secret") {
			t.Fatalf("metadata error leaks environment: %v", err)
		}
	}
}

func TestLocalShellInspectRejectsRestartingAfterDiscovery(t *testing.T) {
	raw := inspected(t, func(m map[string]any) { m["State"].(map[string]any)["Restarting"] = true })
	_, err := ParseMetadata(raw, selected(), "xterm")
	if exitcode.FromError(err) != 68 {
		t.Fatalf("inspect-time restart accepted: %v", err)
	}
}
