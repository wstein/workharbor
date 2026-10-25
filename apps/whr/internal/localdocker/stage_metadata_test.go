//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func TestLocalStageMetadataUsesPinnedClosedRunnerAndMetadataQuota(t *testing.T) {
	t.Setenv("AWS_SECRET_ACCESS_KEY", "metadata-secret-sentinel")
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 64)
	record := map[string]any{"Id": id, "Name": "/primary", "Image": "image:tag", "State": "running", "Labels": map[string]string{"anchorage-role": "primary", "large": strings.Repeat("x", (1<<20)+1)}, "Mounts": []any{}}
	b, _ := json.Marshal(record)
	data := filepath.Join(t.TempDir(), "inspect.json")
	if err := os.WriteFile(data, append(b, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(t.TempDir(), "docker")
	script := "#!/bin/sh\ntest \"$1\" = --host && test \"$2\" = unix:///tmp/fixed.sock || exit 4\ntest -z \"${AWS_SECRET_ACCESS_KEY+x}\" || exit 5\nshift 2\ncase \"$1:$2\" in\ninfo:--format) printf '%s\\n' '{\"ID\":\"daemon\",\"OSType\":\"linux\",\"OperatingSystem\":\"Debian\",\"KernelVersion\":\"kernel\",\"Name\":\"host\",\"Architecture\":\"x86_64\"}' ;;\ncontainer:ls) printf '%s\\n' '{\"ID\":\"" + id + "\",\"Status\":\"Up\"}' ;;\ncontainer:inspect) cat " + stageShellLiteral(data) + " ;;\n*) exit 6 ;;\nesac\n"
	if err := os.WriteFile(tool, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	r, e := newStageRunner(context.Background(), root, "unix:///tmp/fixed.sock", map[string]string{"docker": tool})
	if e != nil {
		t.Fatal(e)
	}
	s, e := readStageMetadata(context.Background(), r)
	if e != nil || s.Daemon.ID != "daemon" || len(s.Containers) != 1 || s.Containers[0].ID != id {
		t.Fatalf("metadata: %+v %v", s, e)
	}
	if len(s.Containers[0].Labels["large"]) <= 1<<20 {
		t.Fatal("metadata incorrectly used phase 1-MiB quota")
	}
}

func TestLocalStagePrimarySelectionReasons(t *testing.T) {
	id := strings.Repeat("a", 64)
	primary := inspection{ID: id, Name: "/primary", State: "running", Labels: map[string]string{"anchorage-role": "primary"}}
	for _, tc := range []struct {
		name             string
		all              []inspection
		selector, reason string
		code             exitcode.Code
	}{
		{"absent", nil, "primary", "primary target absent", exitcode.NotFound},
		{"ambiguous", []inspection{primary, primary}, "primary", "primary target ambiguous", exitcode.Usage},
		{"foreign", []inspection{{ID: id, Name: "/primary", State: "running", Labels: map[string]string{}}}, "primary", "primary target absent", exitcode.NotFound},
		{"stopped", []inspection{{ID: id, Name: "/primary", State: "exited", Labels: primary.Labels}}, "primary", "primary must be running", exitcode.Conflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, e := selectStagePrimary(tc.all, tc.selector)
			if exitcode.FromError(e) != tc.code || !strings.Contains(e.Error(), tc.reason) {
				t.Fatalf("wrong selection refusal: %v", e)
			}
		})
	}
	if got, e := selectStagePrimary([]inspection{primary}, id); e != nil || got.ID != id {
		t.Fatalf("exact ID: %+v %v", got, e)
	}
}
