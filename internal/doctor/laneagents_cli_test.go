package doctor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/cli"
	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/skillset"
)

func TestDoctorCLIRejectsMissingSelectedInventory(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"home", "workspaces", "tools", "skills", "secrets"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	secret := func(name, content string) string {
		filename := filepath.Join(root, "secrets", name)
		if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return filename
	}
	cfg := config.Config{
		Listen: "127.0.0.1:8787", Repositories: []config.Repository{{Name: "wstein/workharbor"}},
		Roots:  config.Roots{Workspaces: []string{filepath.Join(root, "workspaces")}, ToolStore: filepath.Join(root, "tools")},
		GitHub: config.GitHub{AppID: 1, KeyFile: secret("app.pem", "fixture")}, APITokenFile: secret("api.token", "fixture\n"),
		SkillSet: skillset.Config{Selection: "package", Store: filepath.Join(root, "skills"), Package: &skillset.Pin{Identity: "alternative", Source: "https://example.test/pack", Commit: strings.Repeat("a", 40), ManifestSHA256: strings.Repeat("b", 64), InventorySHA256: strings.Repeat("c", 64), ContractVersion: 1}},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, "config.json")
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(filename); err != nil {
		t.Fatalf("fixture configuration: %v", err)
	}
	args := []string{"--config", filename, "--json", "doctor", "--user", "fixture-user"}
	for _, check := range doctor.Checks(doctor.Deps{}) {
		if check.Name != "lane-agents" {
			args = append(args, "--skip", check.Name)
		}
	}
	var stdout, stderr bytes.Buffer
	code := cli.Execute(context.Background(), cli.Env{
		Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		Getenv: func(name string) string {
			if name == "HOME" {
				return filepath.Join(root, "home")
			}
			return ""
		},
		Setup: cli.SetupEnv{User: "fixture-user", GOOS: "linux", Executable: func() (string, error) { return filepath.Join(root, "whr"), nil }},
	}, args)
	var report struct {
		OK     bool            `json:"ok"`
		Checks []doctor.Result `json:"checks"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("code=%d stdout=%s stderr=%s: %v", code, stdout.String(), stderr.String(), err)
	}
	if code != exitcode.Error || report.OK {
		t.Fatalf("missing inventory accepted: code=%d report=%+v stderr=%s", code, report, stderr.String())
	}
	for _, result := range report.Checks {
		if result.Check == "lane-agents" {
			if result.Status != doctor.Fail || !strings.Contains(result.Detail, "selected lane package inventory") {
				t.Fatalf("wrong failure: %+v", result)
			}
			return
		}
	}
	t.Fatal("lane-agents result missing")
}
