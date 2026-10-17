// SPDX-License-Identifier: EUPL-1.2

// Package output renders command results: the --json envelope of
// contracts/cli/output.schema.json, the human "whr:" error line (FR-017),
// color (FR-016) and the split between data on stdout and diagnostics on
// stderr (FR-014).
package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

// SchemaVersion is the envelope version this build writes.
const SchemaVersion = 1

// maxText is the schema's maxLength for messages and remediation, in runes.
const maxText = 1024

// ErrorBody is the "error" object of the envelope.
type ErrorBody struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

// Envelope is the single JSON document a command writes in --json mode.
type Envelope struct {
	SchemaVersion int        `json:"schema_version"`
	Command       string     `json:"command"`
	OK            bool       `json:"ok"`
	ExitCode      int        `json:"exit_code"`
	Result        any        `json:"result,omitempty"`
	Error         *ErrorBody `json:"error,omitempty"`
	OperationID   string     `json:"operation_id,omitempty"`
}

// Writer sends data to Out and diagnostics to Err. In JSON mode Out receives
// exactly one envelope and nothing else, whatever happens.
type Writer struct {
	Out, Err io.Writer
	JSON     bool
	// Color decorates the "whr:" prefix on Err; it never affects Out.
	Color bool

	wrote bool
}

// Success writes the result: the envelope in JSON mode, otherwise human text
// (already safe for a terminal; escape untrusted text with terminal.Escape).
func (w *Writer) Success(command string, result any, human string) error {
	if !w.JSON {
		_, err := io.WriteString(w.Out, human)
		return err
	}
	if result == nil {
		result = struct{}{}
	}
	return w.envelope(Envelope{SchemaVersion: SchemaVersion, Command: command, OK: true, ExitCode: 0, Result: result})
}

// Failure announces err on stderr as a "whr:" line plus an optional remediation
// line, writes the failure envelope in JSON mode and returns the exit code.
func (w *Writer) Failure(command string, err *exitcode.Error) exitcode.Code {
	code := err.Code()
	if w.JSON {
		name, _ := exitcode.Name(int(code))
		_ = w.envelope(Envelope{
			SchemaVersion: SchemaVersion, Command: command, OK: false, ExitCode: int(code), Result: err.Result,
			Error: &ErrorBody{Code: name, Message: clip(err.Message), Remediation: clip(err.Remediation)},
		})
	}
	prefix := "whr:"
	if w.Color {
		prefix = "\x1b[1;31mwhr:\x1b[0m"
	}
	fmt.Fprintf(w.Err, "%s %s\n", prefix, terminal.Escape(err.Message))
	if err.Remediation != "" {
		fmt.Fprintf(w.Err, "  hint: %s\n", terminal.Escape(err.Remediation))
	}
	return code
}

// Notice writes one progress or diagnostic line to stderr (FR-014).
func (w *Writer) Notice(format string, args ...any) {
	fmt.Fprintln(w.Err, terminal.Escape(fmt.Sprintf(format, args...)))
}

var errSecondEnvelope = errors.New("a command writes one envelope only")

func (w *Writer) envelope(e Envelope) error {
	if w.wrote {
		return errSecondEnvelope
	}
	w.wrote = true
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(e); err != nil {
		return err
	}
	// The encoder's trailing newline is the only structural control character;
	// escape the document and add the newline back afterwards.
	doc := append(terminal.EscapeJSON(bytes.TrimSuffix(buf.Bytes(), []byte("\n"))), '\n')
	_, err := w.Out.Write(doc)
	return err
}

// clip bounds text to the schema's maxLength, marking the cut.
func clip(s string) string {
	if r := []rune(s); len(r) > maxText {
		return string(r[:maxText-1]) + "…"
	}
	return s
}

// ColorMode is the value of --color.
type ColorMode string

// The accepted --color values.
const (
	ColorAuto   ColorMode = "auto"
	ColorAlways ColorMode = "always"
	ColorNever  ColorMode = "never"
)

// ParseColorMode accepts exactly auto, always and never.
func ParseColorMode(s string) (ColorMode, error) {
	switch m := ColorMode(s); m {
	case ColorAuto, ColorAlways, ColorNever:
		return m, nil
	}
	return "", fmt.Errorf("invalid --color value %q (use auto, always or never)", s)
}

// UseColor decides whether a stream gets color: --no-color and "never" turn it
// off, "always" turns it on regardless of the environment, and auto (also the
// empty mode) needs a terminal, no non-empty NO_COLOR and TERM other than dumb.
func UseColor(mode ColorMode, noColorFlag bool, getenv func(string) string, isTerminal bool) bool {
	switch {
	case noColorFlag, mode == ColorNever:
		return false
	case mode == ColorAlways:
		return true
	}
	return isTerminal && getenv("NO_COLOR") == "" && strings.TrimSpace(getenv("TERM")) != "dumb"
}
