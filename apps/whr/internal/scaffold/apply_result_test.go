// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/output"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestInitPartialResultMatchesPublicSchema(t *testing.T) {
	dir := fixture(t)
	p := applyProposal(t, dir, Options{NonInteractive: true, Yes: true})
	result, err := p.apply(context.Background(), func(at string) error {
		if at == "after:workharbor.json" {
			if e := os.Rename(filepath.Join(dir, "workharbor.json"), filepath.Join(dir, "retained")); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(filepath.Join(dir, "workharbor.json"), []byte("foreign"), 0644); e != nil {
				t.Fatal(e)
			}
			return syscall.ENOSPC
		}
		return nil
	})
	var coded *exitcode.Error
	if !errors.As(err, &coded) || coded.Code() != 73 {
		t.Fatalf("missing partial failure: %v", err)
	}
	var out, diagnostics bytes.Buffer
	w := output.Writer{Out: &out, Err: &diagnostics, JSON: true}
	w.Failure("workspace.init", coded)
	raw, e := os.ReadFile("../../../../contracts/cli/output.schema.json")
	if e != nil {
		t.Fatal(e)
	}
	schemaDoc, e := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	c := jsonschema.NewCompiler()
	const uri = "https://workharbor.invalid/output.schema.json"
	if e = c.AddResource(uri, schemaDoc); e != nil {
		t.Fatal(e)
	}
	schema, e := c.Compile(uri)
	if e != nil {
		t.Fatal(e)
	}
	doc, e := jsonschema.UnmarshalJSON(bytes.NewReader(out.Bytes()))
	if e != nil {
		t.Fatal(e)
	}
	if e = schema.Validate(doc); e != nil {
		t.Errorf("partial failure violates public schema: %v", e)
	}
	data, _ := json.Marshal(result)
	var view map[string]any
	_ = json.Unmarshal(data, &view)
	remaining, ok := view["remaining"].([]any)
	if !ok || len(remaining) < 2 {
		t.Fatalf("remaining recovery paths not typed: %s", data)
	}
	joined := fmt.Sprint(remaining)
	if !strings.Contains(joined, "workharbor.json") || !strings.Contains(joined, ".whr-init-tmp-journal") {
		t.Fatal("missing every remaining path")
	}
}
