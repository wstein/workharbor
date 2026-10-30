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
	// Published nixbox index (tag 2026.10.30-b2, also latest and stable) as read on 2026-10-30; fixture only.
	recoverNixboxDigest = "c20d3778c2843f66de3739a024c6bafa758afa252e54646321b0558639cdbc9f"
	recoverTestNixbox   = "bundledock-nixbox-" + recoverNixboxDigest
	recoverNixboxTag    = "2026.10.30-b2"
	debianPin           = "docker.io/library/debian@sha256:f6b08b09a05725f9431502d3b5714f5c4576e82c1a84fa1debe0c24a7e0c3ec4"
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

func withMountedBundles(t *testing.T, f *homeFake) {
	t.Helper()
	withMountedToolbox(t, f)
	f.set("inspect", strings.Replace(mustRead(t, filepath.Join(f.dir, "inspect")), `{"Type":"bind"`, `{"Type":"volume","Name":"`+recoverTestNixbox+`","Destination":"/nix","RW":false},{"Type":"bind"`, 1))
}

// bundled prepares a recover with both bundles taken from the workspace's own mounts.
func bundled(t *testing.T, f *homeFake, o RecoverOptions) *Recover {
	t.Helper()
	o.Toolbox, o.Nixbox = "current", "current"
	return prepareRecover(t, f, o)
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
		"":                                  {kind: "image", value: "quay.io/bundledock/toolbox:latest"},
		"latest":                            {kind: "image", value: "quay.io/bundledock/toolbox:latest"},
		"2026.10.24-b1":                     {kind: "image", value: "quay.io/bundledock/toolbox:2026.10.24-b1"},
		"current":                           {kind: "current"},
		"sha256:" + strings.Repeat("c", 64): {kind: "image", value: "quay.io/bundledock/toolbox@sha256:" + strings.Repeat("c", 64)},
		"d3a83465":                          {kind: "volume", value: "d3a83465"},
		recoverTestToolbox:                  {kind: "volume", value: strings.TrimPrefix(recoverTestToolbox, "bundledock-toolbox-")},
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

func TestRecoverNixboxChoices(t *testing.T) {
	digest := "sha256:" + recoverNixboxDigest
	for in, want := range map[string]toolboxChoice{
		"":                {kind: "image", value: "quay.io/bundledock/nixbox:latest"},
		"latest":          {kind: "image", value: "quay.io/bundledock/nixbox:latest"},
		recoverNixboxTag:  {kind: "image", value: "quay.io/bundledock/nixbox:" + recoverNixboxTag},
		"current":         {kind: "current"},
		digest:            {kind: "image", value: "quay.io/bundledock/nixbox@" + digest},
		"c20d3778":        {kind: "volume", value: "c20d3778"},
		recoverTestNixbox: {kind: "volume", value: recoverNixboxDigest},
	} {
		got, err := parseNixboxChoice(in)
		if err != nil || got != want {
			t.Errorf("%q: %+v %v want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"a b", "--privileged", "x/y", "-x", "sha256:abc", "quay.io/evil/img:1"} {
		if _, err := parseNixboxChoice(in); exitcode.FromError(err) != exitcode.Usage {
			t.Errorf("%q admitted: %v", in, err)
		}
	}
}

func TestRecoverPreviewHasNoEffects(t *testing.T) {
	f := newHomeFake(t, "")
	withMountedBundles(t, f)
	p := bundled(t, f, RecoverOptions{Target: "ws1"}).Preview()
	if p.Status != "preview" || p.Source.Volume != "ws1-home" || p.ToolboxVolume != recoverTestToolbox || p.NixboxVolume != recoverTestNixbox || !p.ReadOnly || p.Network || p.Image != debianPin {
		t.Fatalf("preview %+v", p)
	}
	if len(p.Actions) != 0 {
		t.Fatalf("current bundles and a present image need no action: %+v", p.Actions)
	}
	if m := f.mutations(); len(m) != 0 {
		t.Fatalf("preview mutated: %v", m)
	}
}

// The dry run lists the pulls and volumes a default start would do, as not attempted.
func TestRecoverDryRunPlansPullsAndVolumes(t *testing.T) {
	f := newHomeFake(t, "")
	f.set("no-tools", "")
	p := prepareRecover(t, f, RecoverOptions{Target: "ws1"}).Preview()
	if p.Toolbox != "quay.io/bundledock/toolbox:latest" || p.Nixbox != "quay.io/bundledock/nixbox:latest" || p.ToolboxVolume != "" || p.NixboxVolume != "" {
		t.Fatalf("latest preview %+v", p)
	}
	var got []string
	for _, a := range p.Actions {
		if a.Attempted || a.Applied {
			t.Errorf("preview action ran: %+v", a)
		}
		got = append(got, a.Kind+" "+a.Target)
	}
	want := []string{"recover_image_pull " + debianPin, "toolbox_pull quay.io/bundledock/toolbox:latest", "toolbox_volume bundledock-toolbox-<image id>",
		"nixbox_pull quay.io/bundledock/nixbox:latest", "nixbox_volume bundledock-nixbox-<image id>"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("plan %q\nwant %q", got, want)
	}
	if m := f.mutations(); len(m) != 0 {
		t.Fatalf("dry run mutated: %v", m)
	}
}

func TestRecoverCurrentBundlesNeedMountedVolumes(t *testing.T) {
	f := newHomeFake(t, "")
	for _, o := range []RecoverOptions{{Toolbox: "current"}, {Toolbox: "latest", Nixbox: "current"}} {
		o.Target, o.Term = "ws1", "xterm"
		if _, err := f.client.PrepareRecover(context.Background(), socketPorts(), o); exitcode.FromError(err) != exitcode.Conflict {
			t.Fatalf("%+v: no mounted volume: %v", o, err)
		}
	}
	f.set("volumes", "ws1-home\n")
	for _, o := range []RecoverOptions{{Toolbox: "deadbeef"}, {Nixbox: "deadbeef"}} {
		o.Target, o.Term = "ws1", "xterm"
		if _, err := f.client.PrepareRecover(context.Background(), socketPorts(), o); exitcode.FromError(err) != exitcode.NotFound {
			t.Fatalf("%+v: unknown local volume: %v", o, err)
		}
	}
	if _, err := f.client.PrepareRecover(context.Background(), socketPorts(), RecoverOptions{Target: "ws1", Nixbox: "--rm", Term: "xterm"}); exitcode.FromError(err) != exitcode.Usage {
		t.Fatalf("bad nixbox: %v", err)
	}
}

func TestRecoverLocalVolumesStartWithoutPull(t *testing.T) {
	f := newHomeFake(t, "", recoverTestToolbox, recoverTestNixbox)
	res, err := prepareRecover(t, f, RecoverOptions{Target: "ws1", Toolbox: recoverTestToolbox[len("bundledock-toolbox-"):][:8], Nixbox: recoverNixboxDigest[:8]}).Start(context.Background(), recoverStreams(t))
	if err != nil || res.ToolboxVolume != recoverTestToolbox || res.NixboxVolume != recoverTestNixbox {
		t.Fatalf("start %+v %v", res, err)
	}
	if got := strings.Join(f.mutations(), ","); got != "run --rm" {
		t.Fatalf("mutations %s", got)
	}
}

func TestRecoverMountedBundlesStartWithoutPullOrBuild(t *testing.T) {
	f := newHomeFake(t, "")
	withMountedBundles(t, f)
	res, err := bundled(t, f, RecoverOptions{Target: "ws1"}).Start(context.Background(), recoverStreams(t))
	if err != nil || res.Status != "exited" || res.ExitStatus != 0 {
		t.Fatalf("start %+v %v", res, err)
	}
	if got := strings.Join(f.mutations(), ","); got != "run --rm" {
		t.Fatalf("mutations %s", got)
	}
	runs := f.runs()
	want := recoverRunArgv(recoverSpec{op: homeTestOp, home: "ws1-home", toolbox: recoverTestToolbox, nixbox: recoverTestNixbox, image: RecoverImage(), term: "xterm-256color"})
	if !reflect.DeepEqual(runs[0], want) {
		t.Fatalf("run argv\n got %q\nwant %q", runs[0], want)
	}
	joined := strings.Join(runs[0], " ")
	for _, need := range []string{"--network none", "--read-only", "--cap-drop ALL", "ws1-home:/recover/home:ro", recoverTestToolbox + ":/opt/bdock/toolbox:ro", recoverTestNixbox + ":/nix:ro", "--interactive --tty",
		"--env PATH=/nix/profile/bin:/opt/bdock/toolbox/bin:", "--env BDOCK_TOOLBOX=/opt/bdock/toolbox", "--env SSL_CERT_FILE=/nix/profile/etc/ssl/certs/ca-bundle.crt", debianPin + " -c"} {
		if !strings.Contains(joined, need) {
			t.Errorf("argv lacks %q", need)
		}
	}
	if strings.Contains(joined, "apt") || strings.Contains(joined, "/bin/sh -l") || strings.Contains(joined, "whr-recover:") {
		t.Errorf("argv has a package manager, login sh, or built image: %s", joined)
	}
}

func TestRecoverRwAndNetworkAreExplicit(t *testing.T) {
	f := newHomeFake(t, "")
	withMountedBundles(t, f)
	if _, err := bundled(t, f, RecoverOptions{Target: "ws1", RW: true, Net: true}).Start(context.Background(), recoverStreams(t)); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(f.runs()[0], " ")
	if !strings.Contains(joined, "ws1-home:/recover/home ") || strings.Contains(joined, "ws1-home:/recover/home:ro") || !strings.Contains(joined, "--network bridge") {
		t.Fatalf("argv %s", joined)
	}
}

// Tags pull each bundle once and fill bundledock-<name>-<image id> volumes like bundle-volume.sh, as the root carrier.
func TestRecoverTagsPullAndPopulateBothVolumes(t *testing.T) {
	f := newHomeFake(t, "")
	r := prepareRecover(t, f, RecoverOptions{Target: "ws1", Toolbox: "2026.10.24-b1", Nixbox: recoverNixboxTag})
	res, err := r.Start(context.Background(), recoverStreams(t))
	if err != nil || res.ToolboxVolume != "bundledock-toolbox-"+recoverPulledID || res.NixboxVolume != "bundledock-nixbox-"+recoverPulledID {
		t.Fatalf("start %+v %v", res, err)
	}
	if got := strings.Join(f.mutations(), ","); got != "image pull,volume create,run --rm,image pull,volume create,run --rm,run --rm" {
		t.Fatalf("mutations %s", got)
	}
	var pulls, creates, populates [][]string
	for _, c := range f.calls() {
		switch {
		case c[0] == "image" && c[1] == "pull":
			pulls = append(pulls, c)
		case c[0] == "volume" && c[1] == "create":
			creates = append(creates, c)
		case c[0] == "run" && len(c) < 20:
			populates = append(populates, c)
		}
	}
	populate := func(kind string) []string {
		return []string{"run", "--rm", "--pull", "never", "--network", "none", "--mount", "type=volume,src=bundledock-" + kind + "-" + recoverPulledID + ",dst=/bundle,volume-nocopy", recoverPulledID}
	}
	if !reflect.DeepEqual(pulls, [][]string{{"image", "pull", "--quiet", "--", "quay.io/bundledock/toolbox:2026.10.24-b1"}, {"image", "pull", "--quiet", "--", "quay.io/bundledock/nixbox:" + recoverNixboxTag}}) {
		t.Fatalf("pulls %q", pulls)
	}
	if !reflect.DeepEqual(creates, [][]string{{"volume", "create", "--label", "workharbor.recover=toolbox", "--", "bundledock-toolbox-" + recoverPulledID}, {"volume", "create", "--label", "workharbor.recover=nixbox", "--", "bundledock-nixbox-" + recoverPulledID}}) {
		t.Fatalf("creates %q", creates)
	}
	if !reflect.DeepEqual(populates, [][]string{populate("toolbox"), populate("nixbox")}) {
		t.Fatalf("populate argv %q", populates)
	}
	// A second start finds both volumes and does not fill them again.
	f.set("volumes", "ws1-home\nbundledock-toolbox-"+recoverPulledID+"\nbundledock-nixbox-"+recoverPulledID+"\n")
	f.set("count", "0")
	if _, err = prepareRecover(t, f, RecoverOptions{Target: "ws1", Toolbox: "2026.10.24-b1", Nixbox: "latest"}).Start(context.Background(), recoverStreams(t)); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.mutations(), ","); got != "image pull,image pull,run --rm" {
		t.Fatalf("second start mutations %s", got)
	}
}

func TestRecoverNixboxPullFailureStopsBeforeTheShell(t *testing.T) {
	f := newHomeFake(t, "")
	withMountedToolbox(t, f)
	f.set("pull-fails", "")
	_, err := prepareRecover(t, f, RecoverOptions{Target: "ws1", Toolbox: "current"}).Start(context.Background(), recoverStreams(t))
	if exitcode.FromError(err) != exitcode.Conflict || strings.Contains(strings.Join(f.mutations(), ","), "run") {
		t.Fatalf("failed pull: %v %v", err, f.mutations())
	}
}

// The recovery image is the pinned stock Debian: pulled when missing, never built.
func TestRecoverPullsMissingRecoverImageAndNeverBuilds(t *testing.T) {
	f := newHomeFake(t, "")
	withMountedBundles(t, f)
	f.set("no-tools", "")
	res, err := bundled(t, f, RecoverOptions{Target: "ws1"}).Start(context.Background(), recoverStreams(t))
	if err != nil || res.Actions[0].Kind != "recover_image_pull" || res.Actions[0].Target != debianPin || !res.Actions[0].Applied {
		t.Fatalf("start %+v %v", res, err)
	}
	if got := strings.Join(f.mutations(), ","); got != "image pull,run --rm" {
		t.Fatalf("mutations %s", got)
	}
	pulled := false
	for _, c := range f.calls() {
		if c[0] == "image" && c[1] == "pull" {
			pulled = reflect.DeepEqual(c, []string{"image", "pull", "--quiet", "--", debianPin})
		}
	}
	if !pulled {
		t.Fatalf("calls %q", f.calls())
	}
	f = newHomeFake(t, "")
	withMountedBundles(t, f)
	f.set("no-tools", "")
	f.set("pull-fails", "")
	_, err = bundled(t, f, RecoverOptions{Target: "ws1"}).Start(context.Background(), recoverStreams(t))
	if exitcode.FromError(err) != exitcode.Conflict || strings.Contains(strings.Join(f.mutations(), ","), "run") {
		t.Fatalf("failed pull: %v %v", err, f.mutations())
	}
}

func TestRecoverHasNoBuildPath(t *testing.T) {
	if _, err := os.Stat("recover.Dockerfile"); err == nil {
		t.Fatal("recover.Dockerfile still exists")
	}
	if admitHomeMutation([]string{"build", "--quiet", "--tag", "whr-recover:abcdef123456", "-"}) || admitHomeMutation([]string{"build", "--quiet", "--tag", debianPin, "-"}) {
		t.Fatal("a recover build is admitted")
	}
	if !admitHomeMutation([]string{"build", "--quiet", "--tag", ToolsImage(), "-"}) {
		t.Fatal("tools image build refused")
	}
	for _, name := range []string{"RecoverImage"} {
		if RecoverImage() != debianPin {
			t.Fatalf("%s is not the pinned Debian: %s", name, RecoverImage())
		}
	}
}

func TestRecoverPropagatesTheShellStatus(t *testing.T) {
	f := newHomeFake(t, "")
	withMountedBundles(t, f)
	f.set("exit-code", "3")
	res, err := bundled(t, f, RecoverOptions{Target: "ws1"}).Start(context.Background(), recoverStreams(t))
	if err != nil || res.ExitStatus != 3 {
		t.Fatalf("status %+v %v", res, err)
	}
}

func TestRecoverArgvAdmission(t *testing.T) {
	spec := recoverSpec{op: homeTestOp, home: "src", toolbox: recoverTestToolbox, nixbox: recoverTestNixbox, image: RecoverImage(), term: "xterm"}
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
	with := func(f func(*recoverSpec)) []string {
		s := spec
		f(&s)
		return recoverRunArgv(s)
	}
	nixPopulate := func(vol, network, id string) []string {
		return []string{"run", "--rm", "--pull", "never", "--network", network, "--mount", "type=volume,src=" + vol + ",dst=/bundle,volume-nocopy", id}
	}
	for name, args := range map[string][]string{
		"privileged":        append([]string{"run", "--privileged"}, good[1:]...),
		"host mount":        append(append([]string(nil), good...), "-v", "/:/host"),
		"other image":       with(func(s *recoverSpec) { s.image = "alpine:latest" }),
		"old built image":   with(func(s *recoverSpec) { s.image = "whr-recover:abcdef123456" }),
		"bad term":          with(func(s *recoverSpec) { s.term = "x --privileged" }),
		"other toolbox":     with(func(s *recoverSpec) { s.toolbox = "ws1-home" }),
		"toolbox as nixbox": with(func(s *recoverSpec) { s.nixbox = recoverTestToolbox }),
		"nixbox as toolbox": with(func(s *recoverSpec) { s.toolbox = recoverTestNixbox }),
		"other nixbox":      with(func(s *recoverSpec) { s.nixbox = "ws1-home" }),
		"nixbox rw":         tamper(indexOf(good, recoverTestNixbox+":/nix:ro"), recoverTestNixbox+":/nix"),
		"nixbox elsewhere":  tamper(indexOf(good, recoverTestNixbox+":/nix:ro"), recoverTestNixbox+":/usr:ro"),
		"path":              tamper(indexOf(good, "PATH="+recoverPath), "PATH=/tmp/evil"),
		"template":          tamper(len(good)-1, "rm -rf /recover"),
		"pull foreign":      {"image", "pull", "--quiet", "--", "quay.io/evil/toolbox:1"},
		"pull foreign nix":  {"image", "pull", "--quiet", "--", "quay.io/evil/nixbox:1"},
		"pull option":       {"image", "pull", "--quiet", "--", "--all-tags"},
		"pull debian tag":   {"image", "pull", "--quiet", "--", "debian:latest"},
		"pull platform":     {"image", "pull", "--platform", "linux/amd64", "--", "quay.io/bundledock/nixbox:latest"},
		"volume rm":         {"volume", "rm", "--", recoverTestToolbox},
		"volume rm nixbox":  {"volume", "rm", "--", recoverTestNixbox},
		"populate other":    nixPopulate("ws1-home", "none", recoverPulledID),
		"populate net":      nixPopulate("bundledock-nixbox-"+recoverPulledID, "host", recoverPulledID),
		"populate mismatch": nixPopulate("bundledock-nixbox-"+recoverPulledID, "none", strings.Repeat("c", 64)),
		"nixbox create lbl": {"volume", "create", "--label", "workharbor.recover=evil", "--", recoverTestNixbox},
		"nixbox create bad": {"volume", "create", "--label", "workharbor.recover=nixbox", "--", "ws1-home"},
		"toolbox lbl nix":   {"volume", "create", "--label", "workharbor.recover=toolbox", "--", recoverTestNixbox},
		"build other tag":   {"build", "--quiet", "--tag", "evil:1", "-"},
	} {
		if admitRecoverRun(args) || admitHomeMutation(args) {
			t.Errorf("%s admitted", name)
		}
	}
	for _, ok := range [][]string{
		{"image", "pull", "--quiet", "--", "quay.io/bundledock/toolbox:latest"},
		{"image", "pull", "--quiet", "--", "quay.io/bundledock/nixbox:latest"},
		{"image", "pull", "--quiet", "--", "quay.io/bundledock/nixbox:" + recoverNixboxTag},
		{"image", "pull", "--quiet", "--", "quay.io/bundledock/nixbox@sha256:" + recoverNixboxDigest},
		{"image", "pull", "--quiet", "--", debianPin},
		nixPopulate("bundledock-toolbox-"+recoverPulledID, "none", recoverPulledID),
		nixPopulate("bundledock-nixbox-"+recoverPulledID, "none", recoverPulledID),
		{"volume", "create", "--label", "workharbor.recover=toolbox", "--", "bundledock-toolbox-" + recoverPulledID},
		{"volume", "create", "--label", "workharbor.recover=nixbox", "--", "bundledock-nixbox-" + recoverPulledID},
	} {
		if !admitHomeMutation(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, ok := range [][]string{
		{"image", "inspect", "--format", "{{.Id}}", "--", debianPin},
		{"image", "inspect", "--format", "{{.Id}}", "--", "quay.io/bundledock/nixbox:latest"},
		{"image", "inspect", "--format", "{{.Id}}", "--", "quay.io/bundledock/nixbox@sha256:" + recoverNixboxDigest},
	} {
		if !admitHomeRead(ok) {
			t.Errorf("read %q refused", ok)
		}
	}
	if admitHomeRead([]string{"image", "inspect", "--format", "{{.Id}}", "--", "quay.io/evil/nixbox:1"}) {
		t.Error("foreign inspect admitted")
	}
}

func indexOf(args []string, v string) int {
	for i, a := range args {
		if a == v {
			return i
		}
	}
	return -1
}
