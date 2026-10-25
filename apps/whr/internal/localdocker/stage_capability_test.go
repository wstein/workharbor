//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func TestLocalStageRejectsUnboundHostEnvironmentCopy(t *testing.T) {
	for _, args := range [][]string{{"-e", "PATH"}, {"--env", "PATH"}, {"--env=PATH"}, {"-eAWS_SECRET_ACCESS_KEY"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			c, opts, p := fixtureStagePreparation(t)
			b, _ := json.Marshal(map[string]any{"image": "example:local", "runArgs": args})
			if e := os.WriteFile(filepath.Join(opts.Source, ".devcontainer/devcontainer.json"), b, 0600); e != nil {
				t.Fatal(e)
			}
			_, e := c.prepareStage(context.Background(), opts, p)
			if exitcode.FromError(e) != exitcode.Conflict || !strings.Contains(e.Error(), "unbound host environment copy unsupported") {
				t.Fatalf("host copy admitted: %v", e)
			}
			if _, e = os.Stat(p.StateRoot); !os.IsNotExist(e) {
				t.Fatalf("capability refusal wrote state: %v", e)
			}
		})
	}
	for _, args := range [][]string{{"-e", "PUBLIC=value"}, {"--env=PUBLIC=value"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			c, opts, p := fixtureStagePreparation(t)
			b, _ := json.Marshal(map[string]any{"image": "example:local", "runArgs": args})
			if e := os.WriteFile(filepath.Join(opts.Source, ".devcontainer/devcontainer.json"), b, 0600); e != nil {
				t.Fatal(e)
			}
			s, e := c.prepareStage(context.Background(), opts, p)
			if e != nil {
				t.Fatalf("literal configured env refused: %v", e)
			}
			if e = s.Close(); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestLocalStageConfigurationAndSmokeQuotasRefuseBeforeEffects(t *testing.T) {
	for _, kind := range []string{"configuration", "smoke"} {
		t.Run(kind, func(t *testing.T) {
			c, opts, p := fixtureStagePreparation(t)
			opts.Smoke = true
			path := filepath.Join(opts.Source, ".devcontainer", "smoke-container.sh")
			body := []byte(strings.Repeat("x", (256<<10)+1))
			if kind == "configuration" {
				path = filepath.Join(opts.Source, ".devcontainer", "devcontainer.json")
				body, _ = json.Marshal(map[string]any{"image": "example:local", "padding": strings.Repeat("x", 256<<10)})
			}
			if e := os.WriteFile(path, body, 0600); e != nil {
				t.Fatal(e)
			}
			_, e := c.prepareStage(context.Background(), opts, p)
			if exitcode.FromError(e) != exitcode.Conflict || !strings.Contains(e.Error(), kind+" input exceeds 256 KiB or is unavailable") {
				t.Fatalf("oversized required input admitted: %v", e)
			}
			if _, e = os.Stat(p.StateRoot); !os.IsNotExist(e) {
				t.Fatalf("quota refusal wrote state: %v", e)
			}
		})
	}
}
