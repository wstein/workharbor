//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func stageFactsFixture() []byte {
	return []byte("HOST_NAME='captain'\nHOST_IP='127.0.0.1'\nHOST_OS='linux'\nHOST_ARCH='x86_64'\nHOST_ENGINE='Docker'\nHOST_ENGINE_ARCH='amd64'\nHOST_CONTEXT='owner'\\''s context'\n")
}

func TestLocalStageHostFactsLiteralAdmission(t *testing.T) {
	valid := stageFactsFixture()
	if err := validateStageHostFacts(valid); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{
		bytes.Replace(valid, []byte("HOST_IP="), []byte("HOST_NAME="), 1),
		bytes.Replace(valid, []byte("'linux'"), []byte("$(touch sentinel)"), 1),
		append(append([]byte{}, valid...), []byte("echo unsafe\n")...),
		bytes.Replace(valid, []byte("'linux'"), []byte("'lin\x1bux'"), 1),
		bytes.Replace(valid, []byte("'linux'"), []byte("'"+strings.Repeat("x", 4097)+"'"), 1),
		bytes.Replace(valid, []byte("HOST_OS='linux'\nHOST_ARCH='x86_64'"), []byte("HOST_ARCH='x86_64'\nHOST_OS='linux'"), 1),
	} {
		if err := validateStageHostFacts(bad); exitcode.FromError(err) != exitcode.Conflict || !strings.Contains(err.Error(), "host facts literal data invalid") {
			t.Fatalf("wrong literal refusal: %v", err)
		}
	}
}

func TestLocalStageHostFactsPublicationNeverOverwritesCandidateWork(t *testing.T) {
	source := filepath.Join(t.TempDir(), "host.env")
	if err := os.WriteFile(source, stageFactsFixture(), 0600); err != nil {
		t.Fatal(err)
	}
	data, digest, err := readStageHostFacts(source)
	if err != nil || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("admission: %s %v", digest, err)
	}
	clone := t.TempDir()
	if err = os.Mkdir(filepath.Join(clone, ".devcontainer"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = publishStageHostFacts(clone, data); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(clone, ".devcontainer", "host.env")
	st, err := os.Stat(target)
	if err != nil || st.Mode().Perm() != 0644 {
		t.Fatalf("facts mode: %v %v", st, err)
	}
	b, got, err := readStageHostFacts(target)
	if err != nil || got != digest || !bytes.Equal(b, data) {
		t.Fatalf("published bytes: %s %v", got, err)
	}
	if err = publishStageHostFacts(clone, []byte("replacement")); exitcode.FromError(err) != exitcode.Conflict || !strings.Contains(err.Error(), "candidate host facts target must be absent") {
		t.Fatalf("overwrite admitted: %v", err)
	}
	b, _ = os.ReadFile(target)
	if !bytes.Equal(b, data) {
		t.Fatal("candidate work overwritten")
	}
}
