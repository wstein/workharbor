// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"
)

// MaxFindings is the number of findings a report lists (output schema); more
// are counted and replaced by one findings.truncated note.
const MaxFindings = 200

const (
	maxMessage = 1024 // runes, output schema maxLength
	keyEcho    = 64   // runes of an untrusted key or path that may be echoed
)

// Finding is one result of a check. Subject is a path relative to the
// validated directory (untrusted text) or empty.
type Finding struct {
	Code        string   `json:"code"`
	Severity    Severity `json:"severity"`
	Subject     string   `json:"-"`
	Message     string   `json:"message"`
	Remediation string   `json:"remediation,omitempty"`
}

// MarshalJSON writes the subject as null when empty, as the schema allows.
func (f Finding) MarshalJSON() ([]byte, error) {
	type plain Finding
	var subject *string
	if f.Subject != "" {
		subject = &f.Subject
	}
	return json.Marshal(struct {
		plain
		Subject *string `json:"subject"`
	}{plain(f), subject})
}

// Counts are the exact numbers of findings per severity, including findings
// beyond the listing bound.
type Counts struct {
	Error   int `json:"error"`
	Warning int `json:"warning"`
	Info    int `json:"info"`
}

// Report is the result of a validation: the validateResult of the output schema.
type Report struct {
	Directory         string    `json:"directory"`
	Valid             bool      `json:"valid"`
	ProviderAdmission string    `json:"provider_admission"`
	Counts            Counts    `json:"counts"`
	Findings          []Finding `json:"findings"`
	Truncated         bool      `json:"truncated"`
}

// ProviderAdmissionNotEvaluated is the only provider_admission value: validation
// never claims a configuration can be provisioned.
const ProviderAdmissionNotEvaluated = "not_evaluated"

// Errors returns the number of error findings.
func (r *Report) Errors() int { return r.Counts.Error }

// collector gathers findings during a run.
type collector struct{ list []Finding }

// add records a finding of a catalogue code. An empty remediation takes the
// default of the code. Unknown codes are a programming error.
func (c *collector) add(code, subject, message, remediation string) {
	sev, ok := severityOf[code]
	if !ok {
		panic("workspacecfg: unknown finding code " + code)
	}
	if remediation == "" {
		remediation = defaultRemediation[code]
	}
	c.list = append(c.list, Finding{
		Code: code, Severity: sev, Subject: clip(subject), Message: clip(message), Remediation: clip(remediation),
	})
}

func (c *collector) addAll(fs []Finding) { c.list = append(c.list, fs...) }

func (c *collector) hasError() bool {
	for _, f := range c.list {
		if f.Severity == SevError {
			return true
		}
	}
	return false
}

func severityRank(s Severity) int {
	switch s {
	case SevError:
		return 0
	case SevWarning:
		return 1
	}
	return 2
}

// newReport sorts findings deterministically (severity, code, subject,
// message), drops exact duplicates, counts them and bounds the listing.
func newReport(directory string, list []Finding) *Report {
	fs := append([]Finding(nil), list...)
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if ra, rb := severityRank(a.Severity), severityRank(b.Severity); ra != rb {
			return ra < rb
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		return a.Message < b.Message
	})
	r := &Report{Directory: directory, ProviderAdmission: ProviderAdmissionNotEvaluated, Findings: []Finding{}}
	var prev *Finding
	for i := range fs {
		f := fs[i]
		if prev != nil && f.Code == prev.Code && f.Subject == prev.Subject && f.Message == prev.Message {
			continue
		}
		prev = &fs[i]
		switch f.Severity {
		case SevError:
			r.Counts.Error++
		case SevWarning:
			r.Counts.Warning++
		default:
			r.Counts.Info++
		}
		r.Findings = append(r.Findings, f)
	}
	r.Valid = r.Counts.Error == 0
	if len(r.Findings) > MaxFindings {
		r.Truncated = true
		r.Findings = r.Findings[:MaxFindings-1]
		note := Finding{
			Code: "findings.truncated", Severity: SevInfo,
			Message:     "more findings than the listing bound; the counts include every finding",
			Remediation: "fix the errors listed first and run validate again",
		}
		r.Findings = append(r.Findings, note)
	}
	return r
}

// clip bounds text to the schema's maxLength and makes it valid UTF-8.
func clip(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	if utf8.RuneCountInString(s) <= maxMessage {
		return s
	}
	return string([]rune(s)[:maxMessage-1]) + "…"
}

// echo returns an untrusted key or path for a message: invalid UTF-8 replaced
// and truncated to keyEcho runes.
func echo(s string) string {
	s = strings.ToValidUTF8(s, "�")
	if r := []rune(s); len(r) > keyEcho {
		return string(r[:keyEcho]) + "…"
	}
	return s
}

// defaultRemediation gives each code that has one a fix hint.
var defaultRemediation = map[string]string{
	"manifest.missing":                     "run whr init to create workharbor.json",
	"manifest.unsafe_path":                 "make workharbor.json a regular file inside the directory",
	"manifest.too_large":                   "keep workharbor.json under 64 KiB",
	"manifest.encoding":                    "save workharbor.json as UTF-8 without a byte-order mark or NUL bytes",
	"manifest.syntax":                      "write strict JSON with at most 128 nesting levels",
	"manifest.duplicate_key":               "remove the repeated key",
	"manifest.schema":                      "match contracts/workspace/workharbor.schema.json (schema_version 1 or 2)",
	"manifest.path_invalid":                "use a relative, slash-separated path without empty, . , .. or .git components",
	"manifest.url_credentials":             "remove the credentials from the URL and authenticate through a Git credential helper",
	"manifest.bundles_deferred":            "set bundles to an empty list",
	"devcontainer.missing":                 "create the devcontainer file named by devcontainer_path",
	"devcontainer.unsafe_path":             "make every component a real directory and the file a regular file inside the directory",
	"devcontainer.too_large":               "keep the devcontainer file under 256 KiB",
	"devcontainer.encoding":                "save the devcontainer file as UTF-8 without a byte-order mark or NUL bytes",
	"devcontainer.syntax":                  "write valid JSON with comments and trailing commas only",
	"devcontainer.duplicate_key":           "remove the repeated key",
	"devcontainer.unsupported_key":         "see docs/development/devcontainer-matrix.md for the supported subset",
	"devcontainer.image_not_digest_pinned": "pin the image by digest (name@sha256:...)",
	"devcontainer.users_not_numeric":       "use explicit numeric UID:GID pairs for containerUser and remoteUser",
	"devcontainer.host_command":            "remove the host command; the provider refuses it",
	"projects.missing":                     "create the projects directory",
	"projects.unsafe_path":                 "make projects_path a real directory inside the directory",
	"projects.not_a_submodule":             "pin projects as Git submodules; keep only .gitkeep as a file",
	"git.not_repository":                   "run git init and commit if the configuration should be pinned",
	"git.unavailable":                      "install Git 2.39.5 or newer",
	"git.unsafe_path":                      "use a plain .git directory without commondir, alternates, links or special files",
	"git.head_missing":                     "create the first commit",
	"git.listing_failed":                   "check the repository with git fsck outside whr, then run validate again",
	"git.limit_exceeded":                   "reduce the number of tracked files",
	"git.tree_path_invalid":                "remove paths with .git components from the commit",
	"gitmodules.unsafe":                    "make .gitmodules a regular file",
	"gitmodules.too_large":                 "keep .gitmodules under 64 KiB and 256 entries",
	"gitmodules.syntax":                    "write .gitmodules as [submodule \"name\"] sections with path and url",
	"gitmodules.duplicate_field":           "keep one path and one url per submodule",
	"gitmodules.unsupported_key":           "keep only path and url in .gitmodules",
	"gitmodules.uncommitted_change":        "commit .gitmodules",
	"submodule.url_invalid":                "use an absolute https://, ssh:// or user@host:path URL",
	"submodule.url_credentials":            "remove the credentials from the URL and use a Git credential helper",
	"submodule.update_command":             "remove the update command from .gitmodules",
	"submodule.unlisted":                   "add a .gitmodules entry for the gitlink",
	"submodule.not_pinned":                 "run git submodule add or remove the entry",
	"submodule.outside_projects":           "place the submodule below projects_path",
	"submodule.nested":                     "do not nest or overlap submodule paths",
	"submodule.not_committed":              "commit the submodule change",
	"project.declared_not_pinned":          "add the project as a submodule at its path and commit it",
	"findings.time_budget_exhausted":       "run validate on a smaller or faster directory",
}
