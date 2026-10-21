//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package adapters

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/detect"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/dotfiles"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/pkgmgr"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/plan"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/profile"
)

type memoryNode struct {
	info dotfiles.Info
	data []byte
}
type memoryFiles struct {
	nodes                             map[string]memoryNode
	serial                            int
	failBeforeRename, failAfterRename bool
	failDestination                   string
	failRead                          string
	failedReads                       int
}

func newMemoryFiles() *memoryFiles {
	return &memoryFiles{nodes: map[string]memoryNode{".": {info: dotfiles.Info{ID: "root", Mode: fs.ModeDir | 0700}}}}
}
func (f *memoryFiles) put(p string, b []byte, mode fs.FileMode) {
	f.serial++
	f.nodes[p] = memoryNode{info: dotfiles.Info{ID: fmt.Sprint(f.serial), Mode: mode, Size: int64(len(b))}, data: bytes.Clone(b)}
}
func (f *memoryFiles) Stat(p string) (dotfiles.Info, error) {
	n, ok := f.nodes[p]
	if !ok {
		return dotfiles.Info{}, fs.ErrNotExist
	}
	return n.info, nil
}
func (f *memoryFiles) Read(p, id string, limit int64) ([]byte, error) {
	if f.failRead == p {
		f.failRead = ""
		f.failedReads++
		return nil, fs.ErrPermission
	}
	n, ok := f.nodes[p]
	if !ok {
		return nil, fs.ErrNotExist
	}
	if n.info.ID != id || !n.info.Mode.IsRegular() || int64(len(n.data)) > limit {
		return nil, fs.ErrPermission
	}
	return bytes.Clone(n.data), nil
}
func (f *memoryFiles) Mkdir(p string, m fs.FileMode) error {
	if _, ok := f.nodes[p]; ok {
		return fs.ErrExist
	}
	f.put(p, nil, fs.ModeDir|m)
	return nil
}
func (f *memoryFiles) Create(p string, b []byte, m fs.FileMode) (dotfiles.Info, error) {
	if _, ok := f.nodes[p]; ok {
		return dotfiles.Info{}, fs.ErrExist
	}
	parent, ok := f.nodes[path.Dir(p)]
	if !ok || !parent.info.Mode.IsDir() {
		return dotfiles.Info{}, fs.ErrNotExist
	}
	f.put(p, b, m)
	return f.nodes[p].info, nil
}
func (f *memoryFiles) Rename(from, to, fromID, toID string) error {
	if f.failBeforeRename && to == f.failDestination {
		f.failBeforeRename = false
		return fs.ErrPermission
	}
	n, ok := f.nodes[from]
	if !ok || n.info.ID != fromID {
		return fs.ErrPermission
	}
	old, exists := f.nodes[to]
	if exists && old.info.ID != toID || !exists && toID != "" {
		return fs.ErrPermission
	}
	f.nodes[to] = n
	delete(f.nodes, from)
	if f.failAfterRename && to == f.failDestination {
		f.failAfterRename = false
		return fs.ErrPermission
	}
	return nil
}
func (f *memoryFiles) Remove(p, id string) error {
	n, ok := f.nodes[p]
	if !ok {
		return fs.ErrNotExist
	}
	if n.info.ID != id {
		return fs.ErrPermission
	}
	delete(f.nodes, p)
	return nil
}
func concreteFilesFixture(t *testing.T, old []byte, installed bool) (plan.ApplyEngine, plan.ApplyRequest, *memoryFiles, *fixtureExec, *dotfiles.Engine, pkgmgr.Request, string) {
	t.Helper()
	brew, request, exec, home := brewFixture(t)
	mem := newMemoryFiles()
	target := ".config/jq/config"
	if old != nil {
		mem.put(".config", nil, fs.ModeDir|0700)
		mem.put(".config/jq", nil, fs.ModeDir|0700)
		mem.put(target, old, 0600)
	}
	content := []byte("managed=true\nversion=1\n")
	config := catalogue.Config{Marker: "dotfile", Target: target, ContentFile: "jq.conf", SHA256: fmt.Sprintf("%x", sha256.Sum256(content)), Mode: "0600"}
	request.Input.Catalogue.Items[0].Config = &config
	request.Input.Catalogue.Templates = map[string][]byte{"jq.conf": content}
	files := dotfiles.New(mem, fixtureClock{})
	preview, err := files.Preview(config, request.Input.Catalogue.Templates)
	if err != nil {
		t.Fatal(err)
	}
	request.Input.Config = map[string]plan.ConfigObservation{"jq": {State: string(preview.State()), SHA256: preview.BeforeSHA256()}}
	if installed {
		request.Input.Results[0].State = "present_ok"
	}
	request.Policy.Confirmed = map[string]bool{"jq": true}
	sealed, err := plan.Build(request.Input)
	if err != nil {
		t.Fatal(err)
	}
	request.ExpectedHash = sealed.Hash
	runner, err := New(brew, request, files)
	if err != nil {
		t.Fatal(err)
	}
	store, err := profile.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	apply := plan.ApplyRequest{Input: request.Input, ExpectedHash: request.ExpectedHash, RunID: "0123456789abcdef", Mode: "setup", Answer: "yes", Confirmed: true, ConfirmedItems: map[string]bool{"jq": true}, UID: 501}
	return plan.ApplyEngine{Store: store, Clock: fixtureClock{}, Runner: runner}, apply, mem, exec, files, request, home
}

type observeConcrete struct {
	inner  plan.StepRunner
	status string
	after  func()
}

func (o *observeConcrete) ApplyStep(ctx context.Context, s plan.Step, id string, budget time.Duration, g plan.StepGuard) (plan.StepOutcome, error) {
	out, err := o.inner.ApplyStep(ctx, s, id, budget, g)
	o.status = out.Audit.Result
	if o.after != nil {
		o.after()
	}
	return out, err
}
func TestConcreteDotfileFailureRetainsActualEngineOutcome(t *testing.T) {
	for _, mode := range []string{"failed", "rolled_back"} {
		t.Run(mode, func(t *testing.T) {
			old := []byte("PRIVATE-VALUE-CANARY\n")
			engine, request, mem, exec, _, _, _ := concreteFilesFixture(t, old, true)
			mem.failDestination = ".config/jq/config"
			if mode == "failed" {
				mem.failBeforeRename = true
			} else {
				mem.failAfterRename = true
			}
			observed := &observeConcrete{inner: engine.Runner}
			engine.Runner = observed
			out, err := engine.Apply(context.Background(), request)
			if observed.status != mode {
				t.Fatalf("fault did not reach intended real T004 outcome: got=%s want=%s", observed.status, mode)
			}
			if err != nil || len(exec.calls) != 0 || len(out.Steps) != 1 || out.Steps[0].Audit.Result != mode {
				t.Fatalf("actual T004 outcome was lost: want=%s out=%+v err=%v", mode, out, err)
			}
			if !bytes.Equal(mem.nodes[".config/jq/config"].data, old) {
				t.Fatal("existing T004 failure recovery changed old content")
			}
		})
	}
}
func TestConcreteBrewThenDotfileCreate(t *testing.T) {
	engine, request, mem, exec, _, _, home := concreteFilesFixture(t, nil, false)
	out, err := engine.Apply(context.Background(), request)
	if err != nil || out.ExitCode != 0 || len(exec.calls) != 1 || len(out.Steps) != 2 || out.Steps[1].Audit.Result != "applied" {
		t.Fatal(out, err)
	}
	if !bytes.Equal(mem.nodes[".config/jq/config"].data, []byte("managed=true\nversion=1\n")) {
		t.Fatal("actual T004 Apply did not write managed content")
	}
	raw := string(readConcrete(t, filepath.Join(home, ".local/state/whr/protocols/"+request.RunID+".jsonl")))
	if !strings.Contains(raw, `"type":"diff"`) || !strings.Contains(raw, `"added_lines":2`) {
		t.Fatal("actual T004 diff not audited", raw)
	}
}
func TestConcreteDotfileReplaceCountsFullTruncatedDiff(t *testing.T) {
	old := []byte(strings.Repeat("old\n", 30000) + "PRIVATE-VALUE-CANARY\n")
	engine, request, mem, _, files, pkgRequest, home := concreteFilesFixture(t, old, true)
	preview, err := files.Preview(*pkgRequest.Input.Catalogue.Items[0].Config, pkgRequest.Input.Catalogue.Templates)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.Diff(), "truncated") {
		t.Fatal("fixture did not reach bounded displayed diff")
	}
	out, err := engine.Apply(context.Background(), request)
	if err != nil || out.ExitCode != 0 || out.Steps[0].Diff == nil || out.Steps[0].Diff.Added != 2 || out.Steps[0].Diff.Removed != 30001 || len(out.Steps[0].Backups) != 1 {
		t.Fatal(out, err)
	}
	backup := strings.TrimPrefix(out.Steps[0].Backups[0].Path, "~/")
	if !bytes.Equal(mem.nodes[backup].data, old) {
		t.Fatal("actual verified backup content changed")
	}
	raw, _ := json.Marshal(out)
	journal := string(readConcrete(t, filepath.Join(home, ".local/state/whr/protocols/"+request.RunID+".jsonl")))
	if strings.Contains(string(raw)+journal, "PRIVATE-VALUE-CANARY") {
		t.Fatal("dotfile content leaked into protocol/result")
	}
}
func readConcrete(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestConcreteInterleavesConfigBeforeDependentBrew(t *testing.T) {
	engine, request, mem, exec, files, packageRequest, _ := concreteFilesFixture(t, nil, false)
	packageRequest.Input.Catalogue.Items = append(packageRequest.Input.Catalogue.Items, catalogue.Item{ID: "bat", Kind: "tool", SelectedByDefault: true, Requires: []string{"jq"}, Tool: catalogue.Tool{Command: "bat", MinVersion: "1.0.0", Source: catalogue.Source{Manager: "homebrew", Type: "formula", Name: "bat", Tap: "homebrew/core"}}})
	packageRequest.Input.Results = append(packageRequest.Input.Results, detect.Result{Item: "bat", State: "absent"})
	sealed, err := plan.Build(packageRequest.Input)
	if err != nil {
		t.Fatal(err)
	}
	packageRequest.ExpectedHash = sealed.Hash
	runner, err := New(engine.Runner.(*Runner).brew, packageRequest, files)
	if err != nil {
		t.Fatal(err)
	}
	engine.Runner = runner
	request.Input = packageRequest.Input
	request.ExpectedHash = sealed.Hash
	out, err := engine.Apply(context.Background(), request)
	if err != nil || out.ExitCode != 0 || len(out.Steps) != 3 || len(exec.calls) != 2 || out.Steps[1].Audit.Kind != "dotfile_create" || out.Steps[2].Audit.Item != "bat" || out.Steps[2].Audit.Result != "applied" {
		t.Fatal("actual engines did not interleave through completed config dependency", out, err, exec.calls)
	}
	if string(mem.nodes[".config/jq/config"].data) != "managed=true\nversion=1\n" {
		t.Fatal("dependent tool bypassed actual config engine")
	}
}
func TestConcreteRerunLeavesBothEnginesUnchanged(t *testing.T) {
	engine, request, mem, exec, files, packageRequest, home := concreteFilesFixture(t, nil, false)
	if _, err := engine.Apply(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	before := map[string]memoryNode{}
	for p, n := range mem.nodes {
		n.data = bytes.Clone(n.data)
		before[p] = n
	}
	profilePath := filepath.Join(home, ".config/whr/host-profile.json")
	profileBefore := readConcrete(t, profilePath)
	observed, err := files.Preview(*packageRequest.Input.Catalogue.Items[0].Config, packageRequest.Input.Catalogue.Templates)
	if err != nil {
		t.Fatal(err)
	}
	packageRequest.Input.Results[0].State = "present_ok"
	packageRequest.Input.Config["jq"] = plan.ConfigObservation{State: string(observed.State()), SHA256: observed.BeforeSHA256()}
	sealed, err := plan.Build(packageRequest.Input)
	if err != nil {
		t.Fatal(err)
	}
	packageRequest.ExpectedHash = sealed.Hash
	runner, err := New(engine.Runner.(*Runner).brew, packageRequest, files)
	if err != nil {
		t.Fatal(err)
	}
	engine.Runner = runner
	request.Input = packageRequest.Input
	request.ExpectedHash = sealed.Hash
	request.RunID = "1123456789abcdef"
	out, err := engine.Apply(context.Background(), request)
	if err != nil || out.ExitCode != 0 || len(out.Steps) != 0 || out.ProfileChanged || len(exec.calls) != 1 || !reflect.DeepEqual(before, mem.nodes) || !bytes.Equal(profileBefore, readConcrete(t, profilePath)) {
		t.Fatal("actual engines changed managed state on rerun", out, err)
	}
	journal := readConcrete(t, filepath.Join(home, ".local/state/whr/protocols/"+request.RunID+".jsonl"))
	if bytes.Count(journal, []byte{'\n'}) != 3 {
		t.Fatal("no-op run omitted fresh protocol")
	}
}
func TestConcreteUnwritableProtocolBlocksBothEngines(t *testing.T) {
	engine, request, mem, exec, _, _, home := concreteFilesFixture(t, nil, false)
	dir := filepath.Join(home, ".local/state/whr/protocols")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	retained := []byte("retained audit")
	path := filepath.Join(dir, request.RunID+".jsonl")
	if err := os.WriteFile(path, retained, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := engine.Apply(context.Background(), request)
	if err == nil || out.ExitCode != 68 || len(exec.calls) != 0 || len(mem.nodes) != 1 || !bytes.Equal(retained, readConcrete(t, path)) {
		t.Fatal("unwritable protocol reached an existing action engine", out, err)
	}
}

func TestConcreteRollbackAfterBackupIsPartialOnCancellation(t *testing.T) {
	old := []byte("PRIVATE-VALUE-CANARY\n")
	engine, request, mem, _, _, _, _ := concreteFilesFixture(t, old, true)
	mem.failDestination = ".config/jq/config"
	mem.failAfterRename = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := &observeConcrete{inner: engine.Runner, after: cancel}
	engine.Runner = observed
	out, err := engine.Apply(ctx, request)
	if observed.status != "rolled_back" || err != nil || out.ExitCode != 73 || out.ProfileChanged || len(out.Steps) != 1 || out.Steps[0].Audit.Result != "rolled_back" || len(out.Steps[0].Backups) != 1 {
		t.Fatal("actual rollback/verified backup mutation was lost on cancellation", out, err)
	}
	if !bytes.Equal(mem.nodes[".config/jq/config"].data, old) {
		t.Fatal("T004 did not restore original target")
	}
}
func TestConcreteConstructorBindsActualPreviewToShownHash(t *testing.T) {
	engine, _, mem, exec, files, request, _ := concreteFilesFixture(t, []byte("original\n"), true)
	observation := request.Input.Config["jq"]
	observation.SHA256 = strings.Repeat("a", 64)
	request.Input.Config["jq"] = observation
	sealed, err := plan.Build(request.Input)
	if err != nil {
		t.Fatal(err)
	}
	request.ExpectedHash = sealed.Hash
	before := map[string]memoryNode{}
	for p, n := range mem.nodes {
		n.data = bytes.Clone(n.data)
		before[p] = n
	}
	_, err = New(engine.Runner.(*Runner).brew, request, files)
	if err == nil || len(exec.calls) != 0 || !reflect.DeepEqual(before, mem.nodes) {
		t.Fatal("constructor accepted a shown observation that differs from actual T004 preview", err)
	}
}

type interruptCauseContext struct {
	context.Context
	cause error
}

func (c interruptCauseContext) Err() error {
	if c.Context.Err() != nil {
		return c.cause
	}
	return nil
}
func TestConcreteVerifiedManifestFailureIsPartialWhenInterrupted(t *testing.T) {
	testConcreteBackupFailureInterruption(t, true)
}
func TestConcreteOwnedUnverifiedBackupIsPartialWhenInterrupted(t *testing.T) {
	testConcreteBackupFailureInterruption(t, false)
}
func testConcreteBackupFailureInterruption(t *testing.T, verified bool) {
	t.Helper()
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			old := []byte("PRIVATE-VALUE-CANARY\n")
			engine, request, mem, exec, _, _, home := concreteFilesFixture(t, old, true)
			now := fixtureClock{}.Now()
			seed, err := engine.Store.Begin(profile.Start{RunID: "1123456789abcdef", Command: "setup", Platform: "darwin-arm64", CatalogueVersion: "2026.10.1", Answer: "yes"}, now)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = seed.StoreProfile(profile.HostProfile{SchemaVersion: 1, CatalogueVersion: "2026.10.1", Updated: now.Format(time.RFC3339Nano), Items: map[string]string{"jq": "selected"}}); err != nil {
				t.Fatal(err)
			}
			if err = seed.Finish(0, nil, nil, now); err != nil {
				t.Fatal(err)
			}
			seed.Close()
			profilePath := filepath.Join(home, ".config/whr/host-profile.json")
			profileBefore := readConcrete(t, profilePath)
			dir := ".local/state/whr/backups/" + request.RunID
			backup := dir + "/" + fmt.Sprintf("%x", sha256.Sum256([]byte(".config/jq/config")))[:32] + ".bak"
			if verified {
				mem.failDestination = dir + "/manifest.json"
				mem.failBeforeRename = true
			} else {
				mem.failRead = backup
			}
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := interruptCauseContext{Context: base, cause: cause}
			observed := &observeConcrete{inner: engine.Runner, after: cancel}
			engine.Runner = observed
			out, err := engine.Apply(ctx, request)
			owned, exists := mem.nodes[backup]
			if !exists || !bytes.Equal(owned.data, old) || owned.info.Mode.Perm() != 0600 {
				t.Fatal("actual engine did not retain original-byte backup")
			}
			if err != nil || out.ExitCode != 73 || out.ProfileChanged || len(exec.calls) != 0 || len(out.Steps) != 1 || out.Steps[0].Audit.Result != "failed" || !out.Steps[0].Changed {
				t.Fatal("owned backup failure lost partial mutation/receipt", out, err)
			}
			if verified {
				if len(out.Steps[0].Backups) != 1 || out.Steps[0].Backups[0].Path != "~/"+backup || out.Steps[0].Backups[0].SHA256 != fmt.Sprintf("%x", sha256.Sum256(old)) {
					t.Fatal("receipt differs from actual verified backup", out.Steps[0].Backups)
				}
			} else if len(out.Steps[0].Backups) != 0 || mem.failedReads != 1 {
				t.Fatal("unverified receipt claimed or read fault not reached", out.Steps[0].Backups, mem.failedReads)
			}
			if !bytes.Equal(mem.nodes[".config/jq/config"].data, old) || !bytes.Equal(profileBefore, readConcrete(t, profilePath)) {
				t.Fatal("interrupted manifest failure changed target/profile")
			}
			raw := readConcrete(t, filepath.Join(home, ".local/state/whr/protocols/"+request.RunID+".jsonl"))
			encoded, _ := json.Marshal(out)
			if bytes.Contains(raw, []byte(`"type":"backup"`)) != verified || !bytes.Contains(raw, []byte(`"exit_code":73`)) || strings.Contains(string(raw)+string(encoded), "PRIVATE-VALUE-CANARY") {
				t.Fatal("backup audit/partial exit missing or private bytes leaked")
			}
		})
	}
}

func TestConcreteDotfileCheckpointFailurePreventsAllEffects(t *testing.T) {
	for _, old := range [][]byte{nil, []byte("PRIVATE-VALUE-CANARY\n")} {
		kind := "dotfile_replace"
		if old == nil {
			kind = "dotfile_create"
		}
		t.Run(kind, func(t *testing.T) {
			engine, request, mem, exec, _, _, _ := concreteFilesFixture(t, old, true)
			shown, err := plan.Build(request.Input)
			if err != nil || len(shown.Steps) != 1 || shown.Steps[0].Kind != kind {
				t.Fatal("fixture did not select the concrete dotfile step", shown, err)
			}
			before := make(map[string]memoryNode, len(mem.nodes))
			for name, node := range mem.nodes {
				node.data = bytes.Clone(node.data)
				before[name] = node
			}
			guard := &fixtureGuard{err: errors.New("PRIVATE-GUARD-CANARY")}
			out, err := engine.Runner.ApplyStep(context.Background(), shown.Steps[0], request.RunID, plan.StepBudget, guard)
			if guard.calls != 1 || !errors.Is(err, guard.err) || out.Audit.Result != "blocked" {
				t.Fatal("dotfile checkpoint error was bypassed", guard.calls, out, err)
			}
			if out.Changed || len(out.Backups) != 0 || out.Diff != nil || len(exec.calls) != 0 || !reflect.DeepEqual(before, mem.nodes) {
				t.Fatal("failed checkpoint allowed a target/backup/filesystem or process effect", out, exec.calls)
			}
		})
	}
}
