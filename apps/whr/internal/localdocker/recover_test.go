// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

const (
	recoverTestToolbox = "bundledock-toolbox-d3a83465d5223e7d7e2e870b48bbd996dba37cbe92d7230bdcf3ef22f46f5f75"
	recoverPulledID    = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func recoverStreams(t *testing.T) RecoverStreams {
	t.Helper()
	open := func() *os.File {
		f, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return f
	}
	return RecoverStreams{Stdin: open(), Stdout: open(), Stderr: open()}
}

func withMountedToolbox(t *testing.T, f *homeFake) {
	t.Helper()
	f.set("inspect", strings.Replace(mustRead(t, filepath.Join(f.dir, "inspect")), `{"Type":"bind"`, `{"Type":"volume","Name":"`+recoverTestToolbox+`","Destination":"/opt/bdock/toolbox","RW":false},{"Type":"bind"`, 1))
}

func prepareRecover(t *testing.T, f *homeFake, o RecoverOptions) *Recover {
	t.Helper()
	if o.Term == "" {
		o.Term = "xterm-256color"
	}
	r, err := f.client.PrepareRecover(context.Background(), socketPorts(), o)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	r.newOp = func() (string, error) { return homeTestOp, nil }
	return r
}

func TestRecoverToolboxChoices(t *testing.T) {
	for in, want := range map[string]toolboxChoice{
		"":                 {kind: "image", value: "quay.io/bundledock/toolbox:latest"},
		"latest":           {kind: "image", value: "quay.io/bundledock/toolbox:latest"},
		"2026.10.24-b1":    {kind: "image", value: "quay.io/bundledock/toolbox:2026.10.24-b1"},
		"current":          {kind: "current"},
		"d3a83465":         {kind: "volume", value: "d3a83465"},
		recoverTestToolbox: {kind: "volume", value: strings.TrimPrefix(recoverTestToolbox, "bundledock-toolbox-")},
	} {
		got, err := parseToolboxChoice(in)
		if err != nil || got != want {
			t.Errorf("%q: %+v %v want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"a b", "--privileged", "x/y", "-x", strings.Repeat("a", 129), "quay.io/evil/img:1"} {
		if _, err := parseToolboxChoice(in); exitcode.FromError(err) != exitcode.Usage {
			t.Errorf("%q admitted: %v", in, err)
		}
	}
}

func TestRecoverPreviewHasNoEffects(t *testing.T) {
	f := newHomeFake(t, "")
	withMountedToolbox(t, f)
	p := prepareRecover(t, f, RecoverOptions{Target: "ws1", Toolbox: "current"}).Preview()
	if p.Status != "preview" || p.Source.Volume != "ws1-home" || p.ToolboxVolume != recoverTestToolbox || !p.ReadOnly || p.Network || p.Image != RecoverImage() {
		t.Fatalf("preview %+v", p)
	}
	if m := f.mutations(); len(m) != 0 {
		t.Fatalf("preview mutated: %v", m)
	}
	latest := prepareRecover(t, f, RecoverOptions{Target: "ws1"}).Preview()
	if latest.Toolbox != "quay.io/bundledock/toolbox:latest" || latest.ToolboxVolume != "" {
		t.Fatalf("latest preview %+v", latest)
	}
	if _, err := f.client.PrepareRecover(context.Background(), socketPorts(), RecoverOptions{Target: "ws1", Toolbox: "current", Term: "xterm"}); err != nil {
		t.Fatalf("mounted toolbox: %v", err)
	}
}

func TestRecoverCurrentToolboxNeedsAMountedToolbox(t *testing.T) {
	f := newHomeFake(t, "")
	_, err := f.client.PrepareRecover(context.Background(), socketPorts(), RecoverOptions{Target: "ws1", Toolbox: "current", Term: "xterm"})
	if exitcode.FromError(err) != exitcode.Conflict {
		t.Fatalf("no mounted toolbox: %v", err)
	}
	f.set("volumes", "ws1-home\n")
	_, err = f.client.PrepareRecover(context.Background(), socketPorts(), RecoverOptions{Target: "ws1", Toolbox: "deadbeef", Term: "xterm"})
	if exitcode.FromError(err) != exitcode.NotFound {
		t.Fatalf("unknown local toolbox: %v", err)
	}
}

func TestRecoverMountedToolboxStartsWithoutPullOrBuild(t *testing.T) {
	f := newHomeFake(t, "")
	withMountedToolbox(t, f)
	r := prepareRecover(t, f, RecoverOptions{Target: "ws1", Toolbox: "current"})
	res, err := r.Start(context.Background(), recoverStreams(t))
	if err != nil || res.Status != "exited" || res.ExitStatus != 0 {
		t.Fatalf("start %+v %v", res, err)
	}
	if got := strings.Join(f.mutations(), ","); got != "run --rm" {
		t.Fatalf("mutations %s", got)
	}
	runs := f.runs()
	want := recoverRunArgv(recoverSpec{op: homeTestOp, home: "ws1-home", toolbox: recoverTestToolbox, image: RecoverImage(), term: "xterm-256color"})
	if !reflect.DeepEqual(runs[0], want) {
		t.Fatalf("run argv\n got %q\nwant %q", runs[0], want)
	}
	joined := strings.Join(runs[0], " ")
	for _, need := range []string{"--network none", "--read-only", "--cap-drop ALL", "ws1-home:/recover/home:ro", recoverTestToolbox + ":/opt/bdock/toolbox:ro", "--interactive --tty"} {
		if !strings.Contains(joined, need) {
			t.Errorf("argv lacks %q", need)
		}
	}
}

func TestRecoverRwAndNetworkAreExplicit(t *testing.T) {
	f := newHomeFake(t, "")
	withMountedToolbox(t, f)
	r := prepareRecover(t, f, RecoverOptions{Target: "ws1", Toolbox: "current", RW: true, Net: true})
	if _, err := r.Start(context.Background(), recoverStreams(t)); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(f.runs()[0], " ")
	if !strings.Contains(joined, "ws1-home:/recover/home ") || strings.Contains(joined, "ws1-home:/recover/home:ro") || !strings.Contains(joined, "--network bridge") {
		t.Fatalf("argv %s", joined)
	}
}

// A tag pulls the toolbox image once and fills a bundledock-toolbox-<image id> volume like bundle-volume.sh.
func TestRecoverTagPullsAndPopulatesToolboxVolume(t *testing.T) {
	f := newHomeFake(t, "")
	r := prepareRecover(t, f, RecoverOptions{Target: "ws1", Toolbox: "2026.10.24-b1"})
	res, err := r.Start(context.Background(), recoverStreams(t))
	if err != nil || res.ToolboxVolume != "bundledock-toolbox-"+recoverPulledID {
		t.Fatalf("start %+v %v", res, err)
	}
	if got := strings.Join(f.mutations(), ","); got != "image pull,volume create,run --rm,run --rm" {
		t.Fatalf("mutations %s", got)
	}
	var populate []string
	for _, c := range f.calls() {
		if c[0] == "image" && c[1] == "pull" && !reflect.DeepEqual(c, []string{"image", "pull", "--quiet", "--", "quay.io/bundledock/toolbox:2026.10.24-b1"}) {
			t.Errorf("pull argv %q", c)
		}
		if c[0] == "run" && len(c) < 20 {
			populate = c
		}
	}
	want := []string{"run", "--rm", "--pull", "never", "--network", "none", "--mount", "type=volume,src=bundledock-toolbox-" + recoverPulledID + ",dst=/bundle,volume-nocopy", recoverPulledID}
	if !reflect.DeepEqual(populate, want) {
		t.Fatalf("populate argv %q", populate)
	}
	// A second start finds the volume and does not fill it again.
	f.set("volumes", "ws1-home\nbundledock-toolbox-"+recoverPulledID+"\n")
	f.set("count", "0")
	if _, err = prepareRecover(t, f, RecoverOptions{Target: "ws1", Toolbox: "2026.10.24-b1"}).Start(context.Background(), recoverStreams(t)); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.mutations(), ","); got != "image pull,run --rm" {
		t.Fatalf("second start mutations %s", got)
	}
}

func TestRecoverPullsMissingRecoverImage(t *testing.T) {
	f := newHomeFake(t, "")
	withMountedToolbox(t, f)
	f.set("no-tools", "")
	res, err := prepareRecover(t, f, RecoverOptions{Target: "ws1", Toolbox: "current"}).Start(context.Background(), recoverStreams(t))
	if err != nil || res.Actions[0].Kind != "recover_image_pull" || !res.Actions[0].Applied {
		t.Fatalf("start %+v %v", res, err)
	}
	if got := strings.Join(f.mutations(), ","); got != "image pull,run --rm" {
		t.Fatalf("mutations %s", got)
	}
	if RecoverImage() != "docker.io/library/debian@sha256:f6b08b09a05725f9431502d3b5714f5c4576e82c1a84fa1debe0c24a7e0c3ec4" {
		t.Fatalf("recover image %s is not the pinned stock Debian", RecoverImage())
	}
}

func TestRecoverPropagatesTheShellStatus(t *testing.T) {
	f := newHomeFake(t, "")
	withMountedToolbox(t, f)
	f.set("exit-code", "3")
	res, err := prepareRecover(t, f, RecoverOptions{Target: "ws1", Toolbox: "current"}).Start(context.Background(), recoverStreams(t))
	if err != nil || res.ExitStatus != 3 {
		t.Fatalf("status %+v %v", res, err)
	}
}

func TestRecoverArgvAdmission(t *testing.T) {
	spec := recoverSpec{op: homeTestOp, home: "src", toolbox: recoverTestToolbox, image: RecoverImage(), term: "xterm"}
	if !admitRecoverRun(recoverRunArgv(spec)) {
		t.Fatal("canonical run refused")
	}
	rw, net := spec, spec
	rw.rw, net.net = true, true
	if !admitRecoverRun(recoverRunArgv(rw)) || !admitRecoverRun(recoverRunArgv(net)) {
		t.Fatal("rw or network run refused")
	}
	good := recoverRunArgv(spec)
	tamper := func(i int, v string) []string {
		c := append([]string(nil), good...)
		c[i] = v
		return c
	}
	for name, args := range map[string][]string{
		"privileged":      append([]string{"run", "--privileged"}, good[1:]...),
		"host mount":      append(append([]string(nil), good...), "-v", "/:/host"),
		"other image":     recoverRunArgv(recoverSpec{op: homeTestOp, home: "src", toolbox: recoverTestToolbox, image: "alpine:latest", term: "xterm"}),
		"bad term":        recoverRunArgv(recoverSpec{op: homeTestOp, home: "src", toolbox: recoverTestToolbox, image: RecoverImage(), term: "x --privileged"}),
		"other toolbox":   recoverRunArgv(recoverSpec{op: homeTestOp, home: "src", toolbox: "ws1-home", image: RecoverImage(), term: "xterm"}),
		"template":        tamper(len(good)-1, "rm -rf /recover"),
		"pull foreign":    {"image", "pull", "--quiet", "--", "quay.io/evil/toolbox:1"},
		"pull option":     {"image", "pull", "--quiet", "--", "--all-tags"},
		"volume rm":       {"volume", "rm", "--", recoverTestToolbox},
		"populate other":  {"run", "--rm", "--pull", "never", "--network", "none", "--mount", "type=volume,src=ws1-home,dst=/bundle,volume-nocopy", recoverPulledID},
		"populate net":    {"run", "--rm", "--pull", "never", "--network", "host", "--mount", "type=volume,src=bundledock-toolbox-" + recoverPulledID + ",dst=/bundle,volume-nocopy", recoverPulledID},
		"build other tag": {"build", "--quiet", "--tag", "evil:1", "-"},
		"build recover":   {"build", "--quiet", "--tag", RecoverImage(), "-"},
	} {
		if admitRecoverRun(args) || admitHomeMutation(args) {
			t.Errorf("%s admitted", name)
		}
	}
	for _, ok := range [][]string{
		{"image", "pull", "--quiet", "--", "quay.io/bundledock/toolbox:latest"},
		{"run", "--rm", "--pull", "never", "--network", "none", "--mount", "type=volume,src=bundledock-toolbox-" + recoverPulledID + ",dst=/bundle,volume-nocopy", recoverPulledID},
		{"volume", "create", "--label", "workharbor.recover=toolbox", "--", "bundledock-toolbox-" + recoverPulledID},
		{"image", "pull", "--quiet", "--", RecoverImage()},
	} {
		if !admitHomeMutation(ok) {
			t.Errorf("%q refused", ok)
		}
	}
}
