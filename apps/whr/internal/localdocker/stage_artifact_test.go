//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"golang.org/x/sys/unix"
)

func TestLocalStageFIFOAdmissionNeverWaitsForWriter(t *testing.T) {
	s, err := openStageStore(filepath.Join(fixtureStageStateParent(t), "state"), "daemon", "unix:///tmp/docker.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	op := strings.Repeat("1", 32)
	if err = s.Begin(op, "sha256:"+strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(s.operationPath(op), "fifo")
	if err = unix.Mkfifo(artifact, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(artifact, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, reason string
		call         func() error
	}{
		{"artifact_read", "private artifact ownership, mode or quota", func() error { _, e := s.readArtifact(op, "fifo", 1024); return e }},
		{"artifact_replace", "private artifact replacement identity", func() error { return s.writeArtifact(op, "fifo", []byte("x"), false) }},
		{"tool_hash", "tool executable type or mode", func() error { _, e := stageFileDigest(context.Background(), artifact); return e }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan error, 1)
			go func() { done <- tc.call() }()
			select {
			case e := <-done:
				if exitcode.FromError(e) != exitcode.Conflict || !strings.Contains(e.Error(), tc.reason) {
					t.Fatalf("wrong FIFO refusal: %v", e)
				}
			case <-time.After(time.Second):
				t.Fatal("FIFO admission waited for a writer")
			}
		})
	}
}
