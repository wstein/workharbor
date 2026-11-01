// SPDX-License-Identifier: EUPL-1.2

package sandbox_test

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/sandbox"
)

var idShape = regexp.MustCompile(`^sbx-[a-z2-7]{16}$`)

func TestIDFormatAndUniqueness(t *testing.T) {
	// Default source is the CSPRNG: shape and uniqueness over many draws.
	seen := map[sandbox.ID]bool{}
	for i := 0; i < 2000; i++ {
		id, err := sandbox.NewID(nil)
		if err != nil {
			t.Fatal(err)
		}
		if !idShape.MatchString(string(id)) {
			t.Fatalf("id %q has the wrong shape", id)
		}
		if seen[id] {
			t.Fatalf("id %q repeated", id)
		}
		seen[id] = true
	}

	// The source is injectable and the encoding is lowercase base32: ten
	// bytes of entropy make sixteen characters.
	fixed, err := sandbox.NewID(bytes.NewReader([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}))
	if err != nil || fixed != "sbx-aaaaaaaaaaaaaaaa" {
		t.Fatalf("zero entropy: %q, %v", fixed, err)
	}
	ones, err := sandbox.NewID(bytes.NewReader(bytes.Repeat([]byte{0xff}, 10)))
	if err != nil || ones != "sbx-7777777777777777" {
		t.Fatalf("full entropy: %q, %v", ones, err)
	}
	if _, err := sandbox.NewID(bytes.NewReader([]byte{1, 2, 3})); err == nil {
		t.Error("a short entropy source must fail, not produce a weak id")
	}
	if _, err := sandbox.NewID(failingReader{}); err == nil {
		t.Error("a failing entropy source must fail")
	}
	_ = rand.Reader // the production default

	// Never reused: a collision with a known id is redrawn, a bounded number of times.
	stream := bytes.NewReader(append(bytes.Repeat([]byte{0}, 20), bytes.Repeat([]byte{0xff}, 10)...))
	taken := map[sandbox.ID]bool{"sbx-aaaaaaaaaaaaaaaa": true}
	id, err := sandbox.NewUniqueID(stream, func(c sandbox.ID) bool { return taken[c] })
	if err != nil || id != "sbx-7777777777777777" {
		t.Fatalf("collision not redrawn: %q, %v", id, err)
	}
	always := func(sandbox.ID) bool { return true }
	if _, err := sandbox.NewUniqueID(bytes.NewReader(make([]byte, 1000)), always); !errors.Is(err, sandbox.ErrIDExhausted) {
		t.Errorf("endless collisions must fail with ErrIDExhausted, got %v", err)
	}

	// ParseID accepts exactly the format.
	if got, err := sandbox.ParseID("sbx-abcdefg234567abc"); err != nil || got != "sbx-abcdefg234567abc" {
		t.Errorf("valid id rejected: %v", err)
	}
	for _, bad := range []string{"", "sbx-", "sbx-abc", "sbx-ABCDEFGHIJKLMNOP", "sbx-abcdefghijklmnop1", "sbx-abcdefghijklmno0", "sbx-abcdefghijklmno1",
		"sbx-abcdefghijklmno8", "sbx-abcdefghijklmno9", "sbx_abcdefghijklmnop", " sbx-abcdefghijklmnop", "sbx-abcdefghijklmnop\n", "sbx-abcdefghijklmnop/..", "SBX-abcdefghijklmnop"} {
		if _, err := sandbox.ParseID(bad); !errors.Is(err, sandbox.ErrInvalidID) {
			t.Errorf("ParseID(%q) = %v, want ErrInvalidID", bad, err)
		}
	}

	// Names: validated, and unique among non-discarded instances of one workspace.
	for _, ok := range []string{"", "review", "a", "feature_1.x-2", strings.Repeat("a", 63)} {
		if err := sandbox.ValidName(ok); err != nil {
			t.Errorf("ValidName(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"-x", ".x", "a b", "a/b", "a\nb", "sbx-abcdefghijklmnop", strings.Repeat("a", 64), "ä"} {
		if err := sandbox.ValidName(bad); !errors.Is(err, sandbox.ErrInvalidName) {
			t.Errorf("ValidName(%q) = %v, want ErrInvalidName", bad, err)
		}
	}
	records := []sandbox.Record{
		{ID: "sbx-aaaaaaaaaaaaaaaa", Workspace: "w1", Name: "review", State: sandbox.StateReady},
		{ID: "sbx-bbbbbbbbbbbbbbbb", Workspace: "w1", Name: "old", State: sandbox.StateDiscarded},
		{ID: "sbx-cccccccccccccccc", Workspace: "w2", Name: "other", State: sandbox.StateReady},
	}
	if err := sandbox.NameAvailable(records, "w1", "review"); !errors.Is(err, sandbox.ErrNameTaken) {
		t.Errorf("a live name must be taken: %v", err)
	}
	if err := sandbox.NameAvailable(records, "w1", "old"); err != nil {
		t.Errorf("a discarded instance releases its name: %v", err)
	}
	if err := sandbox.NameAvailable(records, "w1", "other"); err != nil {
		t.Errorf("names are per workspace: %v", err)
	}
	if err := sandbox.NameAvailable(records, "w1", ""); err != nil {
		t.Errorf("the name is optional: %v", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrClosedPipe }
