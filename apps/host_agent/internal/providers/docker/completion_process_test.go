// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOfficialCompletionProcessHelper(t *testing.T) {
	mode := os.Getenv("WORKHARBOR_COMPLETION_HELPER")
	if mode == "" {
		return
	}
	fmt.Fprintln(os.Stdout, `{"outcome":"success","result":"done"}`)
	switch mode {
	case "exit0":
		os.Exit(0)
	case "exit17":
		os.Exit(17)
	case "wait":
		for {
			time.Sleep(time.Second)
		}
	default:
		os.Exit(18)
	}
}
func TestOfficialCompletionRequiresNormalWaitedExitAfterSuccessStdout(t *testing.T) {
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	executable, e = filepath.EvalSymlinks(executable)
	if e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"exit0", "exit17", "wait"} {
		t.Run(mode, func(t *testing.T) {
			duration := 3 * time.Second
			if mode == "wait" {
				duration = 200 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), duration)
			defer cancel()
			raw, e := captureMetadata(ctx, executable, []string{"-test.run=^TestOfficialCompletionProcessHelper$"}, bridgeRoot(t), []string{"WORKHARBOR_COMPLETION_HELPER=" + mode}, 1024)
			if mode == "exit0" {
				if e != nil {
					t.Fatal(e)
				}
				if _, e := phaseResult(context.Background(), registryParser(t), raw, Hooks, ""); e != nil {
					t.Fatal(e)
				}
			} else if e == nil || len(raw) != 0 {
				t.Fatal("success stdout promoted abnormal/noncompleted child", mode, e, string(raw))
			}
		})
	}
}
