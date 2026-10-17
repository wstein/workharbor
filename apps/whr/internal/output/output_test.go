// SPDX-License-Identifier: EUPL-1.2

package output_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/output"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/version"
)

func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "cli", "output.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	const url = "https://workharbor.invalid/contracts/cli/output.schema.json"
	if err := c.AddResource(url, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(url)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func validate(t *testing.T, s *jsonschema.Schema, doc []byte) error {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		t.Fatalf("not JSON: %v: %q", err, doc)
	}
	return s.Validate(v)
}

func jsonWriter() (*output.Writer, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return &output.Writer{Out: &out, Err: &errOut, JSON: true}, &out, &errOut
}

func TestEnvelopeMatchesOutputSchema(t *testing.T) {
	schema := compileSchema(t)

	t.Run("success", func(t *testing.T) {
		w, out, errOut := jsonWriter()
		if err := w.Success("version", version.Current(), "ignored in JSON mode"); err != nil {
			t.Fatal(err)
		}
		if err := validate(t, schema, out.Bytes()); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if errOut.Len() != 0 {
			t.Fatalf("stderr = %q, want empty on success", errOut)
		}
	})

	t.Run("success without result still carries an object", func(t *testing.T) {
		w, out, _ := jsonWriter()
		if err := w.Success("host.doctor", nil, ""); err == nil {
			// host.doctor requires a doctorResult; an empty object must fail the schema, not be omitted.
			if validate(t, schema, out.Bytes()) == nil {
				t.Fatal("empty result for host.doctor unexpectedly validates")
			}
		}
		var env map[string]any
		if err := json.Unmarshal(out.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		if _, ok := env["result"].(map[string]any); !ok {
			t.Fatalf("ok envelope lacks a result object: %s", out)
		}
	})

	t.Run("every error code", func(t *testing.T) {
		for _, e := range exitcode.Table() {
			if e.Code == 0 {
				continue
			}
			for _, command := range []string{"workspace.ls", "unknown", "host.service.install"} {
				w, out, errOut := jsonWriter()
				path := pathFor(e.Code)
				got := w.Failure(command, exitcode.Errorf(path, "problem %d", e.Code).WithRemediation("try again"))
				if int(got) != e.Code {
					t.Fatalf("Failure returned %d, want %d", got, e.Code)
				}
				if err := validate(t, schema, out.Bytes()); err != nil {
					t.Fatalf("code %d: %v\n%s", e.Code, err, out)
				}
				if !strings.HasPrefix(errOut.String(), "whr: problem") {
					t.Fatalf("stderr = %q, want a whr: line", errOut)
				}
			}
		}
	})

	t.Run("unsupported json is a structured usage error", func(t *testing.T) {
		w, out, errOut := jsonWriter()
		w.Failure("help", exitcode.Errorf(exitcode.PathUnsupportedJSON, "help does not support --json"))
		if err := validate(t, schema, out.Bytes()); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if !strings.Contains(out.String(), `"code":"usage"`) || !strings.Contains(errOut.String(), "whr: help does not support") {
			t.Fatalf("stdout=%q stderr=%q", out, errOut)
		}
	})

	t.Run("failure keeps command data", func(t *testing.T) {
		w, out, _ := jsonWriter()
		failure := exitcode.Errorf(exitcode.PathWrongState, "configuration is invalid")
		failure.Result = map[string]any{
			"directory": ".", "valid": false, "provider_admission": "not_evaluated", "truncated": false,
			"counts":   map[string]int{"error": 1, "warning": 0, "info": 0},
			"findings": []any{map[string]any{"code": "manifest.missing", "severity": "error", "message": "no manifest"}},
		}
		w.Failure("workspace.validate", failure)
		if err := validate(t, schema, out.Bytes()); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	})

	t.Run("the schema really rejects wrong envelopes", func(t *testing.T) {
		for name, doc := range map[string]string{
			"error code does not match exit code": `{"schema_version":1,"command":"x.y","ok":false,"exit_code":64,"error":{"code":"conflict","message":"m"}}`,
			"ok with nonzero exit":                `{"schema_version":1,"command":"version","ok":true,"exit_code":64,"result":{}}`,
			"missing result":                      `{"schema_version":1,"command":"version","ok":true,"exit_code":0}`,
			"unknown exit code":                   `{"schema_version":1,"command":"x","ok":false,"exit_code":75,"error":{"code":"usage","message":"m"}}`,
			"non-canonical command":               `{"schema_version":1,"command":"Workspace Ls","ok":true,"exit_code":0,"result":{}}`,
		} {
			if validate(t, schema, []byte(doc)) == nil {
				t.Errorf("%s: schema accepted %s", name, doc)
			}
		}
	})
}

func pathFor(code int) exitcode.Path {
	for _, p := range exitcode.Paths() {
		if int(p.Code()) == code {
			return p
		}
	}
	panic("no path for code")
}

func TestJSONStdoutPurity(t *testing.T) {
	cases := map[string]func(w *output.Writer){
		"success": func(w *output.Writer) { _ = w.Success("version", version.Current(), "human text") },
		"failure": func(w *output.Writer) {
			w.Failure("workspace.ls", exitcode.Errorf(exitcode.PathUnavailableCommand, "not yet"))
		},
		"notice": func(w *output.Writer) { w.Notice("progress %d", 1); _ = w.Success("version", version.Current(), "") },
		"two uses": func(w *output.Writer) {
			_ = w.Success("version", version.Current(), "")
			_ = w.Success("version", version.Current(), "")
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			w, out, errOut := jsonWriter()
			run(w)
			dec := json.NewDecoder(out)
			var first map[string]any
			if err := dec.Decode(&first); err != nil {
				t.Fatalf("stdout is not one JSON document: %v: %q", err, out)
			}
			if _, err := dec.Token(); err != io.EOF {
				t.Fatalf("stdout carries more than one envelope or trailing data")
			}
			if strings.Contains(errOut.String(), `"schema_version"`) {
				t.Fatalf("envelope leaked to stderr: %q", errOut)
			}
		})
	}
	t.Run("human mode keeps data on stdout and diagnostics on stderr", func(t *testing.T) {
		var out, errOut bytes.Buffer
		w := &output.Writer{Out: &out, Err: &errOut}
		w.Notice("working")
		if err := w.Success("version", nil, "data line\n"); err != nil {
			t.Fatal(err)
		}
		w.Failure("version", exitcode.Errorf(exitcode.PathUnclassified, "bad"))
		if out.String() != "data line\n" {
			t.Fatalf("stdout = %q, want only the data", out.String())
		}
		if errOut.String() != "working\nwhr: bad\n" {
			t.Fatalf("stderr = %q", errOut.String())
		}
	})
}

func TestFailureLines(t *testing.T) {
	var out, errOut bytes.Buffer
	w := &output.Writer{Out: &out, Err: &errOut}
	code := w.Failure("workspace.ls", exitcode.Errorf(exitcode.PathUnavailableCommand, "line one\nforged: whr: ok\x1b[2J").WithRemediation("do this\x1b]0;x\a"))
	if code != 64 {
		t.Fatalf("code = %d", code)
	}
	lines := strings.Split(strings.TrimSuffix(errOut.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want a whr: line plus a remediation line, got %q", errOut.String())
	}
	if !strings.HasPrefix(lines[0], "whr: line one<U+000A>forged: whr: ok<U+001B>[2J") {
		t.Errorf("first line = %q", lines[0])
	}
	if lines[1] != "  hint: do this<U+001B>]0;x<U+0007>" {
		t.Errorf("remediation line = %q", lines[1])
	}
	if strings.ContainsAny(errOut.String(), "\x1b\a") {
		t.Errorf("raw control bytes reached stderr: %q", errOut.String())
	}
}

func TestEnvelopeEscapesAndBoundsText(t *testing.T) {
	w, out, _ := jsonWriter()
	long := "\u202e\u009b\x7f" + strings.Repeat("\u00e4", 2000)
	w.Failure("workspace.ls", exitcode.Errorf(exitcode.PathBadArguments, "%s", long))
	for _, r := range out.String() {
		if r == 0x202e || r == 0x9b || r == 0x7f {
			t.Fatalf("raw hostile rune U+%04X in JSON: %q", r, out)
		}
	}
	var env struct {
		Error struct{ Message string }
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(env.Error.Message)); n > 1024 {
		t.Fatalf("message has %d runes, schema allows 1024", n)
	}
}

func TestColorMatrix(t *testing.T) {
	type in struct {
		mode    output.ColorMode
		noColor bool // --no-color flag
		env     map[string]string
		tty     bool
	}
	cases := []struct {
		name string
		in   in
		want bool
	}{
		{"auto tty", in{output.ColorAuto, false, nil, true}, true},
		{"auto not a tty", in{output.ColorAuto, false, nil, false}, false},
		{"auto tty NO_COLOR set", in{output.ColorAuto, false, map[string]string{"NO_COLOR": "1"}, true}, false},
		{"auto tty NO_COLOR empty is ignored", in{output.ColorAuto, false, map[string]string{"NO_COLOR": ""}, true}, true},
		{"auto tty TERM=dumb", in{output.ColorAuto, false, map[string]string{"TERM": "dumb"}, true}, false},
		{"always not a tty", in{output.ColorAlways, false, nil, false}, true},
		{"always overrides NO_COLOR", in{output.ColorAlways, false, map[string]string{"NO_COLOR": "1"}, false}, true},
		{"never tty", in{output.ColorNever, false, nil, true}, false},
		{"--no-color beats auto", in{output.ColorAuto, true, nil, true}, false},
		{"--no-color beats always", in{output.ColorAlways, true, nil, true}, false},
		{"empty mode is auto", in{"", false, nil, true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := output.UseColor(tc.in.mode, tc.in.noColor, func(k string) string { return tc.in.env[k] }, tc.in.tty)
			if got != tc.want {
				t.Fatalf("UseColor = %v, want %v", got, tc.want)
			}
		})
	}
	for _, s := range []string{"auto", "always", "never"} {
		if _, err := output.ParseColorMode(s); err != nil {
			t.Errorf("ParseColorMode(%q): %v", s, err)
		}
	}
	for _, s := range []string{"", "yes", "ALWAYS", "on"} {
		if _, err := output.ParseColorMode(s); err == nil {
			t.Errorf("ParseColorMode(%q) accepted an invalid value", s)
		}
	}
	t.Run("color only decorates the prefix on stderr and never touches stdout or JSON", func(t *testing.T) {
		var out, errOut bytes.Buffer
		w := &output.Writer{Out: &out, Err: &errOut, Color: true}
		_ = w.Success("version", nil, "plain\n")
		w.Failure("version", exitcode.Errorf(exitcode.PathUnclassified, "bad"))
		if out.String() != "plain\n" {
			t.Errorf("stdout = %q", out.String())
		}
		if !strings.HasPrefix(errOut.String(), "\x1b[1;31mwhr:\x1b[0m bad") {
			t.Errorf("stderr = %q", errOut.String())
		}
		var jo, je bytes.Buffer
		jw := &output.Writer{Out: &jo, Err: &je, JSON: true, Color: true}
		jw.Failure("version", exitcode.Errorf(exitcode.PathUnclassified, "bad"))
		if strings.Contains(jo.String(), "\x1b") {
			t.Errorf("JSON stdout carries color: %q", jo.String())
		}
	})
}
