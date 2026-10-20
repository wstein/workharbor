// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func fixtureAgentsBlock(v int, body string) []byte {
	return []byte(fmt.Sprintf("<!-- whr:agents-block begin v=%d sha256=%s -->\n%s<!-- whr:agents-block end -->\n", v, digest([]byte(body)), body))
}
func TestAgentsBlockClassificationAndActions(t *testing.T) {
	current := fixtureAgentsBlock(agentsBlockVersion, "body\n")
	cases := []struct {
		name          string
		data          []byte
		class, edited string
		refused       bool
	}{
		{"missing", []byte("owner\n"), "missing", "append_block", false},
		{"current", current, "current", "skip_existing", false},
		{"older", fixtureAgentsBlock(agentsBlockVersion-1, "body\n"), "older", "replace_block", false},
		{"modified", bytes.Replace(current, []byte("body\n"), []byte("edit\n"), 1), "modified", "replace_block", false},
		{"newer", bytes.Replace(fixtureAgentsBlock(agentsBlockVersion+1, "body\n"), []byte("body\n"), []byte("edit\n"), 1), "newer", "left_unchanged", false},
		{"malformed", []byte("<!-- whr:agents-block end -->\n"), "malformed", "", true},
		{"size precedence", bytes.Repeat([]byte("<!-- whr:agents-block end -->\n"), 3000), "too_large", "", true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			for _, edit := range []bool{false, true} {
				got, err := planAgentsBlock(tt.data, edit)
				if (err != nil) != (edit && tt.refused) {
					t.Fatalf("edit=%v error=%v", edit, err)
				}
				if err != nil {
					continue
				}
				action := "left_unchanged"
				if tt.class == "current" {
					action = "skip_existing"
				}
				if edit {
					action = tt.edited
				}
				warning := "agents_block." + tt.class
				if tt.class == "current" || (edit && tt.class != "newer") {
					warning = ""
				}
				if got.action != action || got.warning != warning {
					t.Fatalf("edit=%v got %+v want %s %s", edit, got, action, warning)
				}
			}
		})
	}
}
func TestAgentsBlockRejectsMarkerLikeLines(t *testing.T) {
	good := fixtureAgentsBlock(agentsBlockVersion, "body\n")
	begin := strings.Split(string(good), "\n")[0] + "\n"
	cases := []string{"\xef\xbb\xbf  <!-- whr:agents-block unknown -->\n", begin, "<!-- whr:agents-block end -->\n", string(good) + string(good), begin + string(good), strings.ReplaceAll(string(good), "\n", "\r\n"), "  " + string(good), "\xef\xbb\xbf" + string(good), "\xef\xbb\xbf  " + string(good), "```\n\t<!-- whr:agents-block unknown -->\n```\n", strings.TrimSuffix(string(good), "\n")}
	for i, data := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			got, err := planAgentsBlock([]byte(data), false)
			if err != nil || got.warning != "agents_block.malformed" {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
}
func TestAgentsBlockPreservesOutsideBytesAndBoundsResult(t *testing.T) {
	prefix := []byte("\xef\xbb\xbfowner\r\n\r\n")
	suffix := []byte("tail\r\n")
	old := fixtureAgentsBlock(agentsBlockVersion-1, "old\n")
	input := append(append(append([]byte{}, prefix...), old...), suffix...)
	got, err := planAgentsBlock(input, true)
	want := append(append(append([]byte{}, prefix...), renderAgentsBlock()...), suffix...)
	if err != nil || !bytes.Equal(got.data, want) {
		t.Fatalf("replacement changed outside bytes: %v", err)
	}
	got, err = planAgentsBlock(prefix, true)
	want = append(append(append([]byte{}, prefix...), '\n'), renderAgentsBlock()...)
	if err != nil || !bytes.Equal(got.data, want) {
		t.Fatalf("append separator: %v", err)
	}
	large := bytes.Repeat([]byte("a"), agentsBlockMaxBytes-1)
	got, err = planAgentsBlock(large, false)
	if err != nil || got.warning != "agents_block.too_large" {
		t.Fatalf("result bound: %+v %v", got, err)
	}
	if _, err = planAgentsBlock(large, true); err == nil {
		t.Fatal("oversize result accepted")
	}
}
func TestAgentsBlockTemplateIntegrity(t *testing.T) {
	for _, line := range bytes.Split([]byte(agentsBlockBody), []byte("\n")) {
		if markerLike(line) {
			t.Fatal("marker-like template line")
		}
	}
	got, err := planAgentsBlock(renderAgentsBlock(), false)
	if err != nil || got.action != "skip_existing" {
		t.Fatalf("render not current: %+v %v", got, err)
	}
}

func TestAgentsBlockBoundariesAndHashPrecedence(t *testing.T) {
	for _, prefix := range []string{"", "owner", "owner\n", "owner\r\n", "\xef\xbb\xbfowner\r\n"} {
		t.Run(fmt.Sprintf("prefix=%q", prefix), func(t *testing.T) {
			got, err := planAgentsBlock([]byte(prefix), true)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(got.data, []byte(prefix)) {
				t.Fatal("outside bytes changed")
			}
			rerun, err := planAgentsBlock(got.data, false)
			if err != nil || rerun.action != "skip_existing" {
				t.Fatalf("rerun: %+v %v", rerun, err)
			}
		})
	}
	// An older block with edited bytes is modified, ahead of the older class.
	old := fixtureAgentsBlock(agentsBlockVersion-1, "body\n")
	modified := bytes.Replace(old, []byte("body\n"), []byte("edited\n"), 1)
	got, err := planAgentsBlock(modified, false)
	if err != nil || got.warning != "agents_block.modified" {
		t.Fatalf("modified precedence: %+v %v", got, err)
	}
	for _, class := range []string{"missing", "older", "modified"} {
		t.Run(class, func(t *testing.T) {
			base := []byte{}
			switch class {
			case "older":
				base = old
			case "modified":
				base = modified
			}
			// Fill exactly to the maximum permitted output length, then exceed by one.
			replaced := len(base)
			if class == "missing" {
				replaced = -2
			} // appended text needs two LF bytes
			padding := agentsBlockMaxBytes - len(renderAgentsBlock()) + replaced - len(base)
			data := append(bytes.Repeat([]byte("x"), padding), base...)
			if class != "missing" { // markers must start at column zero
				data[padding-1] = '\n'
			}
			got, err := planAgentsBlock(data, true)
			if err != nil || len(got.data) != agentsBlockMaxBytes {
				t.Fatalf("exact bound: size=%d error=%v", len(got.data), err)
			}
			data = append([]byte("x"), data...)
			got, err = planAgentsBlock(data, false)
			if err != nil || got.warning != "agents_block.too_large" {
				t.Fatalf("exceeded result bound: %+v %v", got, err)
			}
		})
	}
}
