// Package version exposes build metadata, set via -ldflags (design §13).
package version

import (
	"encoding/json"
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"
)

// These are set at build time by the Makefile:
//
//	-X .../internal/version.Version=v0.1.0 -X .../Commit=abc1234 -X .../Date=2026-10-01T09:00:00Z -X .../Dirty=true
//
// An unstamped build (plain `go build`) leaves them empty and Get reads the
// Go build info instead.
var (
	Version = ""
	Commit  = ""
	Date    = ""
	Dirty   = ""
)

// SchemaVersion is the version of the JSON form (design §9.2).
const SchemaVersion = 1

// Info is what `whr version` reports.
type Info struct {
	SchemaVersion int    `json:"schema_version"`
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	Dirty         bool   `json:"dirty"`
	Date          string `json:"date,omitempty"`
}

var tagged = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// Get returns the build's version information. The version is never empty:
// without a stamp it is v0.0.0-0-g<commit> (or v0.0.0-0-gunknown), and a stamp
// that is not a version, such as a bare commit from `git describe --always`,
// is turned into v0.0.0-0-g<it>.
func Get() Info {
	info := Info{SchemaVersion: SchemaVersion, Version: Version, Commit: Commit, Date: Date, Dirty: Dirty == "true"}
	if info.Commit == "" || Version == "" {
		if bi, ok := debug.ReadBuildInfo(); ok {
			for _, s := range bi.Settings {
				switch s.Key {
				case "vcs.revision":
					if info.Commit == "" && len(s.Value) >= 7 {
						info.Commit = s.Value[:7]
					}
				case "vcs.modified":
					if Dirty == "" {
						info.Dirty = s.Value == "true"
					}
				case "vcs.time":
					if info.Date == "" {
						info.Date = s.Value
					}
				}
			}
		}
	}
	info.Version = Normalize(info.Version, info.Commit)
	if info.Commit == "" {
		info.Commit = "unknown"
	}
	return info
}

// Normalize makes a version: a tag-based version is kept, and anything else
// (empty, "dev", a bare commit) becomes v0.0.0-0-g<commit>.
func Normalize(v, commit string) string {
	v = strings.TrimSuffix(v, "-dirty")
	if tagged.MatchString(v) {
		return v
	}
	sha := commit
	if v != "" && v != "dev" {
		sha = v
	}
	if sha == "" {
		sha = "unknown"
	}
	return "v0.0.0-0-g" + sha
}

// Text is the human form: the version, the commit and whether the tree was
// dirty.
func (i Info) Text() string {
	state := "clean"
	if i.Dirty {
		state = "dirty"
	}
	return fmt.Sprintf("%s (commit %s, %s)", i.Version, i.Commit, state)
}

// JSON is the data form, one line.
func (i Info) JSON() ([]byte, error) { return json.Marshal(i) }
