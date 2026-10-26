// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func TestLocalPromotionPrivateArtifactsCannotRebindReceipt(t *testing.T) {
	for _, kind := range []string{"changed_bytes", "missing_field", "unknown_field", "noncanonical", "valid"} {
		t.Run(kind, func(t *testing.T) {
			store, err := openStageStore(filepath.Join(fixtureStageStateParent(t), "state"), "daemon", "unix:///fixture.sock")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			op := strings.Repeat("a", 32)
			if err = store.Begin(op, "sha256:"+strings.Repeat("b", 64)); err != nil {
				t.Fatal(err)
			}
			original := stageExecutionBinding{SettingsDigest: "sha256:" + strings.Repeat("c", 64), CandidateDigest: "sha256:" + strings.Repeat("d", 64), OriginalDigest: "sha256:" + strings.Repeat("e", 64), GitInputs: []stageExecutionGitInput{}}
			data, err := stageArtifactBytes(original)
			if err != nil {
				t.Fatal(err)
			}
			expected := stageDigest(original)
			reason := "promotion private artifact digest changed"
			switch kind {
			case "changed_bytes":
				data = []byte(strings.Replace(string(data), strings.Repeat("c", 64), strings.Repeat("f", 64), 1))
			case "missing_field":
				data = []byte(`{"candidate_digest":"x","git_inputs":[],"original_digest":"y"}` + "\n")
				expected = promotionBytesDigest(data)
				reason = "promotion private artifact is not canonical or has missing fields"
			case "unknown_field":
				data = []byte(strings.Replace(string(data), "{", "{\"unknown\":true,", 1))
				expected = promotionBytesDigest(data)
				reason = "promotion private artifact fields invalid"
			case "noncanonical":
				data = append([]byte(" "), data...)
				expected = promotionBytesDigest(data)
				reason = "promotion private artifact is not canonical or has missing fields"
			}
			if err = os.WriteFile(filepath.Join(store.operationPath(op), "execution-binding.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			var got stageExecutionBinding
			err = readPromotionArtifact(store, op, "execution-binding.json", expected, &got)
			if kind == "valid" {
				if err != nil || got.SettingsDigest != original.SettingsDigest {
					t.Fatalf("valid artifact: %v %+v", err, got)
				}
			} else {
				requireStageGitError(t, err, exitcode.Conflict, reason)
			}
		})
	}
}
