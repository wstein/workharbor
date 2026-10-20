// SPDX-License-Identifier: EUPL-1.2
package wizard_test

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/wizard"
)

func TestWizardDevcontainerSelectionPerPathConsent(t *testing.T) {
	for _, answer := range []string{"no", "yes"} {
		s := existing()
		s.Entries["chosen.json"] = scaffold.Entry{Kind: "file", Data: []byte(`{"image":"debian:bookworm"}`)}
		var diag bytes.Buffer
		p, err := wizard.Run(context.Background(), s, scaffold.Options{}, strings.NewReader("edit devcontainer\nchosen.json\nwrite\n"+answer+"\n"), &diag)
		if !strings.Contains(diag.String(), "Modify workharbor.json? [y/N]") || strings.Contains(diag.String(), "Modify chosen.json?") {
			t.Fatalf("wrong consent path: %s", diag.String())
		}
		if answer == "no" {
			if p != nil || int(exitcode.FromError(err)) != 72 {
				t.Fatalf("decline: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		want, err := scaffold.Build(s, scaffold.Options{Devcontainer: ptr("chosen.json"), Overwrite: []string{"workharbor.json"}})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(p.Files(), want.Files()) {
			t.Fatal("wizard and flags differ")
		}
	}
}
