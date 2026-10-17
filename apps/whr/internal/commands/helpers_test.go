// SPDX-License-Identifier: EUPL-1.2

package commands_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
)

type contractEntry struct {
	ID        string   `json:"id"`
	Path      []string `json:"path"`
	Short     []string `json:"short"`
	Increment string   `json:"increment"`
	Needs     string   `json:"needs"`
	Writes    string   `json:"writes"`
	JSON      string   `json:"json"`
	BlockedOn string   `json:"blocked_on"`
	Flags     []string `json:"flags"`
}

type contract struct {
	GroupAliases map[string][]string `json:"group_aliases"`
	Commands     []contractEntry     `json:"commands"`
	Freeze       struct {
		TestFile string   `json:"test_file"`
		TestIDs  []string `json:"test_ids"`
	} `json:"freeze"`
}

func repoFile(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", "..", "..", "contracts", "cli"}, parts...)...)
}

func loadContract(t *testing.T) contract {
	t.Helper()
	raw, err := os.ReadFile(repoFile("commands.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c contract
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile(repoFile("output.schema.json"))
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

type result struct {
	code        int
	stdout, err string
}

// run executes whr in process with a fixed environment, no terminal and the
// given handlers (nil selects the production handlers).
func run(t *testing.T, handlers map[string]commands.Handler, args ...string) result {
	t.Helper()
	var out, errOut bytes.Buffer
	env := commands.Env{
		Stdout:     &out,
		Stderr:     &errOut,
		Getenv:     func(string) string { return "" },
		IsTerminal: func(io.Writer) bool { return false },
	}
	if handlers == nil {
		handlers = baseHandlers()
	}
	code := commands.Execute(context.Background(), args, env, handlers)
	return result{code, out.String(), errOut.String()}
}

// stubHandlers makes every command of the table available with a handler that
// only reports what it was called with.
func stubHandlers() map[string]commands.Handler {
	h := baseHandlers()
	delete(h, "host.doctor") // the stub only reports arguments; doctor has its own tests
	for _, e := range commands.Table() {
		id := e.ID
		if id == "version" { // the one default handler that is not replaced by a stub
			continue
		}
		h[id] = func(_ context.Context, call commands.Call) (commands.Result, error) {
			return commands.Result{
				Value: map[string]any{"id": call.Entry.ID, "args": call.Args},
				Human: "ran " + call.Entry.ID + "\n",
			}, nil
		}
	}
	return h
}

func decodeEnvelope(t *testing.T, stdout string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader([]byte(stdout)))
	var env map[string]any
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("stdout is not JSON: %v: %q", err, stdout)
	}
	if dec.More() {
		t.Fatalf("stdout holds more than one document: %q", stdout)
	}
	return env
}

func jsonUnmarshal(s string) (any, error) {
	return jsonschema.UnmarshalJSON(bytes.NewReader([]byte(s)))
}

// runWithContext is run with a caller context and the production handlers.
func runWithContext(ctx context.Context, out, errOut io.Writer, args ...string) int {
	env := commands.Env{
		Stdout: out, Stderr: errOut,
		Getenv:     func(string) string { return "" },
		IsTerminal: func(io.Writer) bool { return false },
	}
	return commands.Execute(ctx, args, env, commands.DefaultHandlers())
}
