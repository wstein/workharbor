package version

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestNormalizeNeverReturnsEmpty(t *testing.T) {
	tests := []struct{ version, commit, want string }{
		{"v0.1.0", "abc1234", "v0.1.0"},
		{"v0.1.0-3-gabc1234", "abc1234", "v0.1.0-3-gabc1234"},
		{"v0.1.0-rc.1", "abc1234", "v0.1.0-rc.1"},
		{"v0.1.0-dirty", "abc1234", "v0.1.0"},       // dirty is a flag, not part of the version
		{"", "abc1234", "v0.0.0-0-gabc1234"},        // unstamped
		{"dev", "abc1234", "v0.0.0-0-gabc1234"},     // the old default
		{"abc1234", "abc1234", "v0.0.0-0-gabc1234"}, // git describe --always without a tag
		{"abc1234-dirty", "abc1234", "v0.0.0-0-gabc1234"},
		{"", "", "v0.0.0-0-gunknown"},
		{"1.2.3", "abc1234", "v0.0.0-0-g1.2.3"}, // no leading v: not a tag version
	}
	for _, tc := range tests {
		got := Normalize(tc.version, tc.commit)
		if got != tc.want || got == "" {
			t.Errorf("Normalize(%q, %q) = %q, want %q", tc.version, tc.commit, got, tc.want)
		}
	}
}

func TestInfoFormats(t *testing.T) {
	info := Info{SchemaVersion: SchemaVersion, Version: "v0.1.0", Commit: "abc1234", Dirty: true, Date: "2026-10-01T09:00:00Z"}
	if got, want := info.Text(), "v0.1.0 (commit abc1234, dirty)"; got != want {
		t.Errorf("Text = %q, want %q", got, want)
	}
	info.Dirty = false
	if !strings.HasSuffix(info.Text(), ", clean)") {
		t.Errorf("a clean tree reads %q", info.Text())
	}
	raw, err := info.JSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema_version":1,"version":"v0.1.0","commit":"abc1234","dirty":false,"date":"2026-10-01T09:00:00Z"}`
	if string(raw) != want {
		t.Errorf("JSON = %s, want %s", raw, want)
	}
	var back Info
	if err := json.Unmarshal(raw, &back); err != nil || back != info {
		t.Errorf("round trip: %+v, %v", back, err)
	}
}

// A build that was not stamped still has a version that is not empty.
func TestAnUnstampedBuildHasAVersion(t *testing.T) {
	old := [4]string{Version, Commit, Date, Dirty}
	t.Cleanup(func() { Version, Commit, Date, Dirty = old[0], old[1], old[2], old[3] })
	Version, Commit, Date, Dirty = "", "", "", ""
	got := Get()
	if !regexp.MustCompile(`^v0\.0\.0-0-g\S+$`).MatchString(got.Version) || got.Commit == "" {
		t.Errorf("Get() = %+v, want v0.0.0-0-g<commit> and a commit", got)
	}

	Version, Commit, Dirty = "v1.2.3", "abc1234", "true"
	if got := Get(); got.Version != "v1.2.3" || got.Commit != "abc1234" || !got.Dirty {
		t.Errorf("a stamped build = %+v", got)
	}
}
