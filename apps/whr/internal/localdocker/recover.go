// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

// whr workspace recover: a throwaway container on a stock, digest-pinned Debian
// with the workspace home mounted (read-only by default), the BundleDock toolbox
// at /opt/bdock/toolbox and nixbox at /nix. No image is built and no package
// manager runs; the tools come from the two mounted bundles. Nothing is deleted.

const (
	// recoverImageRef is pulled when missing and never built (Debian trixie-slim).
	recoverImageRef   = "docker.io/library/debian@sha256:f6b08b09a05725f9431502d3b5714f5c4576e82c1a84fa1debe0c24a7e0c3ec4"
	toolboxRepo       = "quay.io/bundledock/toolbox"
	toolboxMount      = "/opt/bdock/toolbox"
	nixboxRepo        = "quay.io/bundledock/nixbox"
	nixboxMount       = "/nix"
	nixboxProfile     = nixboxMount + "/profile"
	recoverNameKey    = "workharbor.recover.operation"
	recoverPath       = nixboxProfile + "/bin:" + toolboxMount + "/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	recoverCABundle   = nixboxProfile + "/etc/ssl/certs/ca-bundle.crt"
	recoverShareDirs  = toolboxMount + "/share:" + nixboxProfile + "/share:/usr/share"
	recoverShell      = `if [ -x ` + toolboxMount + `/bin/fish ]; then exec ` + toolboxMount + `/bin/fish -l; fi; exec /bin/sh` // not sh -l: Debian's /etc/profile resets PATH
	defaultRecoverTag = "latest"
)

var (
	toolboxVolumeRE = regexp.MustCompile(`^bundledock-toolbox-[0-9a-f]{64}$`)
	nixboxVolumeRE  = regexp.MustCompile(`^bundledock-nixbox-[0-9a-f]{64}$`)
	toolboxImageRE  = regexp.MustCompile(`^quay\.io/bundledock/toolbox(:[A-Za-z0-9][A-Za-z0-9._-]{0,127}|@sha256:[0-9a-f]{64})$`)
	nixboxImageRE   = regexp.MustCompile(`^quay\.io/bundledock/nixbox(:[A-Za-z0-9][A-Za-z0-9._-]{0,127}|@sha256:[0-9a-f]{64})$`)
	toolboxTagRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	toolboxHexRE    = regexp.MustCompile(`^[0-9a-f]{8,64}$`)
	imageDigestRE   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	imageIDRE       = regexp.MustCompile(`^sha256:([0-9a-f]{64})$`)
	termNameRE      = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`)
)

// RecoverImage is the pinned stock image of the recovery container.
func RecoverImage() string { return recoverImageRef }

// RecoverOptions selects the workspace home and the bundles to mount.
type RecoverOptions struct {
	Target  string // exact container name or full ID
	Toolbox string // "", "latest", a tag, "sha256:<digest>", "current", or a local toolbox volume (id prefix)
	Nixbox  string // same choices for nixbox
	Term    string
	RW      bool // mount the home read-write
	Net     bool // allow network access
}

// RecoverStreams are the native terminal descriptors of the interactive run.
type RecoverStreams struct{ Stdin, Stdout, Stderr *os.File }

// RecoverResult describes one recover session.
type RecoverResult struct {
	Status        string           `json:"status"`
	Source        HomeCopySource   `json:"source"`
	Image         string           `json:"image"`
	Toolbox       string           `json:"toolbox"`
	ToolboxVolume string           `json:"toolbox_volume"`
	Nixbox        string           `json:"nixbox"`
	NixboxVolume  string           `json:"nixbox_volume"`
	ReadOnly      bool             `json:"read_only"`
	Network       bool             `json:"network"`
	ExitStatus    int              `json:"exit_status"`
	Actions       []HomeCopyAction `json:"actions"`
	Residuals     []string         `json:"residuals"`
}

type toolboxChoice struct{ kind, value string } // kind: image, current, volume

// bundle is one mounted BundleDock volume: its image repository and mount point.
type bundle struct {
	name, flag, repo, mount string
	volume                  *regexp.Regexp
}

var (
	toolboxBundle = bundle{"toolbox", "--toolbox", toolboxRepo, toolboxMount, toolboxVolumeRE}
	nixboxBundle  = bundle{"nixbox", "--nixbox", nixboxRepo, nixboxMount, nixboxVolumeRE}
)

func (b bundle) prefix() string { return "bundledock-" + b.name + "-" }

func (b bundle) parse(s string) (toolboxChoice, error) {
	switch {
	case s == "":
		return toolboxChoice{"image", b.repo + ":" + defaultRecoverTag}, nil
	case s == "current":
		return toolboxChoice{kind: "current"}, nil
	case b.volume.MatchString(s):
		return toolboxChoice{"volume", strings.TrimPrefix(s, b.prefix())}, nil
	case imageDigestRE.MatchString(s):
		return toolboxChoice{"image", b.repo + "@" + s}, nil
	case toolboxHexRE.MatchString(s) && (len(s) >= 12 || strings.ContainsAny(s, "abcdef")):
		// A short all-digit string such as the date tag 20261029 is a tag, not a volume id.
		return toolboxChoice{"volume", s}, nil
	case toolboxTagRE.MatchString(s):
		return toolboxChoice{"image", b.repo + ":" + s}, nil
	}
	return toolboxChoice{}, exitcode.Errorf(exitcode.PathBadArguments, "%s takes latest, a tag, a sha256 digest, current, or a local %s volume id", b.flag, b.name)
}

func parseToolboxChoice(s string) (toolboxChoice, error) { return toolboxBundle.parse(s) }
func parseNixboxChoice(s string) (toolboxChoice, error)  { return nixboxBundle.parse(s) }

// selection is the resolved choice for one bundle.
type selection struct {
	desc string // human description
	vol  string // known local volume, or empty when an image is pulled
	ref  string // image to pull, or empty for a local volume
}

type recoverPlan struct {
	source          HomeCopySource
	toolbox, nixbox selection
	pullImage       bool // the pinned recovery image is not local yet
	rw, net         bool
	term            string
}

// Recover is one prepared session; Start consumes it once.
type Recover struct {
	mu       sync.Mutex
	used     bool
	client   Client
	endpoint string
	plan     recoverPlan
	newOp    func() (string, error)
}

// PrepareRecover resolves the home and the bundle choices without any mutation.
func (c Client) PrepareRecover(parent context.Context, ports doctor.Ports, o RecoverOptions) (*Recover, error) {
	if strings.TrimSpace(o.Target) == "" {
		return nil, exitcode.Errorf(exitcode.PathBadArguments, "use workspace recover --local <exact-name|full-id>")
	}
	if !termNameRE.MatchString(o.Term) {
		o.Term = "xterm-256color"
	}
	choices := map[string]toolboxChoice{}
	for _, b := range []struct {
		b bundle
		s string
	}{{toolboxBundle, o.Toolbox}, {nixboxBundle, o.Nixbox}} {
		choice, err := b.b.parse(b.s)
		if err != nil {
			return nil, err
		}
		choices[b.b.name] = choice
	}
	ctx, cancel := context.WithTimeout(parent, doctor.Budget)
	defer cancel()
	endpoint, err := ResolveEndpoint(ctx, ports)
	if err != nil {
		return nil, err
	}
	item, source, err := c.homeSourceOf(ctx, endpoint, o.Target)
	if err != nil {
		return nil, err
	}
	plan := recoverPlan{source: source, rw: o.RW, net: o.Net, term: o.Term}
	for _, b := range []bundle{toolboxBundle, nixboxBundle} {
		sel, err := c.selectBundle(ctx, endpoint, item.Mounts, b, choices[b.name])
		if err != nil {
			return nil, err
		}
		if b.name == "toolbox" {
			plan.toolbox = sel
		} else {
			plan.nixbox = sel
		}
	}
	if _, err := c.homeRead(ctx, endpoint, "image", "inspect", "--format", "{{.Id}}", "--", RecoverImage()); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		plan.pullImage = true
	}
	return &Recover{client: c, endpoint: endpoint, plan: plan, newOp: randomOperation}, nil
}

func (c Client) selectBundle(ctx context.Context, endpoint string, mounts []mount, b bundle, choice toolboxChoice) (selection, error) {
	switch choice.kind {
	case "image":
		return selection{desc: choice.value, ref: choice.value}, nil
	case "current":
		for _, m := range mounts {
			if m.Destination == b.mount && b.volume.MatchString(m.Name) {
				return selection{desc: m.Name, vol: m.Name}, nil
			}
		}
		return selection{}, exitcode.Errorf(exitcode.PathConflict, "the workspace has no %s volume mounted", b.name).WithRemediation("Use %s latest or a tag.", b.flag)
	}
	volumes, err := c.homeVolumes(ctx, endpoint)
	if err != nil {
		return selection{}, err
	}
	var found []string
	for v := range volumes {
		if b.volume.MatchString(v) && strings.HasPrefix(v, b.prefix()+choice.value) {
			found = append(found, v)
		}
	}
	switch len(found) {
	case 0:
		return selection{}, exitcode.Errorf(exitcode.PathTargetAbsent, "no local %s volume matches %q", b.name, choice.value)
	case 1:
		return selection{desc: found[0], vol: found[0]}, nil
	}
	return selection{}, exitcode.Errorf(exitcode.PathBadArguments, "%s id %q is ambiguous; give more characters", b.name, choice.value)
}

// Preview returns what Start would do; the actions are planned, not attempted.
func (r *Recover) Preview() RecoverResult {
	p := r.plan
	res := RecoverResult{Status: "preview", Source: p.source, Image: RecoverImage(), Toolbox: p.toolbox.desc, ToolboxVolume: p.toolbox.vol,
		Nixbox: p.nixbox.desc, NixboxVolume: p.nixbox.vol, ReadOnly: !p.rw, Network: p.net, Actions: []HomeCopyAction{}, Residuals: []string{}}
	plan := func(kind, target string) {
		res.Actions = append(res.Actions, HomeCopyAction{Kind: kind, Target: target})
	}
	if p.pullImage {
		plan("recover_image_pull", RecoverImage())
	}
	for _, x := range []struct {
		b   bundle
		sel selection
	}{{toolboxBundle, p.toolbox}, {nixboxBundle, p.nixbox}} {
		if x.sel.ref != "" {
			plan(x.b.name+"_pull", x.sel.ref)
			plan(x.b.name+"_volume", x.b.prefix()+"<image id>")
		}
	}
	return res
}

// Start pulls what is missing, then hands the terminal to the recovery shell.
func (r *Recover) Start(parent context.Context, s RecoverStreams) (RecoverResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := r.Preview()
	result.Status, result.Actions = "failed", []HomeCopyAction{}
	if r.used {
		return result, exitcode.Errorf(exitcode.PathConflict, "recover session already started; prepare a new one")
	}
	r.used = true
	if err := parent.Err(); err != nil {
		return result, err
	}
	op, err := r.newOp()
	if err != nil || !homeOperation(op) {
		return result, exitcode.Errorf(exitcode.PathUnclassified, "cannot allocate a recover operation ID")
	}
	c, p := r.client, r.plan
	act := func(kind, target string) int {
		result.Actions = append(result.Actions, HomeCopyAction{Kind: kind, Target: target, Attempted: true})
		return len(result.Actions) - 1
	}
	fail := func(path exitcode.Path, remediation, format string, args ...any) (RecoverResult, error) {
		e := exitcode.Errorf(path, format, args...)
		if remediation != "" {
			e = e.WithRemediation("%s", remediation)
		}
		return result, e
	}
	read := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(parent, doctor.Budget)
		defer cancel()
		return c.homeRead(ctx, r.endpoint, args...)
	}
	pull := func(kind, ref, hint string) error {
		i := act(kind, ref)
		if reply, err := c.homeMutate(parent, r.endpoint, "image", "pull", "--quiet", "--", ref); err != nil || reply.status != 0 {
			if parent.Err() != nil {
				return parent.Err()
			}
			_, e := fail(exitcode.PathConflict, hint, "image pull failed: %s", ref)
			return e
		}
		result.Actions[i].Applied = true
		return nil
	}

	if _, err := read("image", "inspect", "--format", "{{.Id}}", "--", RecoverImage()); err != nil {
		if parent.Err() != nil {
			return result, parent.Err()
		}
		if err := pull("recover_image_pull", RecoverImage(), "The recovery image is pulled once and needs network."); err != nil {
			return result, err
		}
	}

	// ensure pulls a bundle image and fills its volume once, like bundle-volume.sh:
	// the carrier runs as root, offline, with the volume at /bundle.
	ensure := func(b bundle, sel selection) (string, error) {
		if sel.ref == "" {
			return sel.vol, nil
		}
		if err := pull(b.name+"_pull", sel.ref, "Check the tag and the network, or use "+b.flag+" current."); err != nil {
			return "", err
		}
		raw, err := read("image", "inspect", "--format", "{{.Id}}", "--", sel.ref)
		m := imageIDRE.FindStringSubmatch(strings.TrimSpace(string(raw)))
		if err != nil || m == nil {
			_, e := fail(exitcode.PathConflict, "", "the pulled %s image has no usable ID", b.name)
			return "", e
		}
		id, volume := m[1], b.prefix()+m[1]
		volumes, err := c.homeVolumes(parent, r.endpoint)
		if err != nil {
			return "", err
		}
		// The carrier writes .ready last and leaves .initializing behind when it fails or
		// is interrupted. A volume is reused only when the marker is there; otherwise the
		// idempotent carrier run is repeated and nothing is mounted unless it succeeds.
		if volumes[volume] {
			if reply, err := c.homeMutate(parent, r.endpoint, recoverReadyArgv(volume)...); err == nil && reply.status == 0 {
				return volume, nil
			} else if parent.Err() != nil {
				return "", parent.Err()
			}
		} else {
			i := act(b.name+"_volume", volume)
			if reply, err := c.homeMutate(parent, r.endpoint, "volume", "create", "--label", "workharbor.recover="+b.name, "--", volume); err != nil || reply.status != 0 {
				if parent.Err() != nil {
					return "", parent.Err()
				}
				_, e := fail(exitcode.PathConflict, "", "%s volume %s could not be created", b.name, volume)
				return "", e
			}
			result.Actions[i].Applied = true
		}
		result.Residuals = append(result.Residuals, "volume:"+volume)
		i := act(b.name+"_populate", volume)
		if reply, err := c.homeMutate(parent, r.endpoint, "run", "--rm", "--pull", "never", "--network", "none", "--mount", "type=volume,src="+volume+",dst=/bundle,volume-nocopy", id); err != nil || reply.status != 0 {
			if parent.Err() != nil {
				return "", parent.Err()
			}
			_, e := fail(exitcode.PathPartialFailure, "Remove the incomplete "+b.name+" volume yourself (docker volume rm "+volume+") and run again; whr deletes nothing automatically.", "%s volume %s is incomplete", b.name, volume)
			return "", e
		}
		result.Actions[i].Applied = true
		result.Residuals = result.Residuals[:len(result.Residuals)-1]
		return volume, nil
	}
	toolbox, err := ensure(toolboxBundle, p.toolbox)
	if err != nil {
		return result, err
	}
	result.ToolboxVolume = toolbox
	nixbox, err := ensure(nixboxBundle, p.nixbox)
	if err != nil {
		return result, err
	}
	result.NixboxVolume = nixbox

	i := act("shell", p.source.Volume)
	status, err := c.runInteractive(parent, r.endpoint, s, recoverRunArgv(recoverSpec{op: op, home: p.source.Volume, toolbox: toolbox, nixbox: nixbox, image: RecoverImage(), term: p.term, rw: p.rw, net: p.net})...)
	if err != nil {
		if parent.Err() != nil {
			return result, parent.Err()
		}
		return fail(exitcode.PathUnclassified, "", "starting the recovery container failed")
	}
	result.Actions[i].Applied = true
	result.Status, result.ExitStatus = "exited", status
	return result, nil
}

// recoverReadyArgv is the read-only completeness probe of a bundle volume: the
// carrier (BundleDock tools-init) writes .ready last and leaves .initializing
// behind when it fails. It runs in the pinned stock image, offline.
func recoverReadyArgv(volume string) []string {
	return []string{"run", "--rm", "--pull", "never", "--network", "none", "--read-only", "--mount", "type=volume,src=" + volume + ",dst=/bundle,readonly",
		"--entrypoint", "/bin/sh", RecoverImage(), "-c", "test -f /bundle/.ready && ! test -e /bundle/.initializing"}
}

type recoverSpec struct {
	op, home, toolbox, nixbox, image, term string
	rw, net                                bool
}

// recoverRunArgv is the only admitted interactive run.
func recoverRunArgv(s recoverSpec) []string {
	network, home := "none", s.home+":/recover/home"
	if s.net {
		network = "bridge"
	}
	if !s.rw {
		home += ":ro"
	}
	return []string{"run", "--rm", "--interactive", "--tty", "--detach-keys=ctrl-],ctrl-^", "--pull", "never", "--network", network, "--read-only",
		"--tmpfs", "/tmp:exec,mode=1777", "--security-opt", "no-new-privileges", "--cap-drop", "ALL",
		"--cap-add", "CHOWN", "--cap-add", "DAC_OVERRIDE", "--cap-add", "FOWNER", "--cap-add", "FSETID",
		"--user", "0:0", "--name", "whr-recover-" + s.op, "--label", recoverNameKey + "=" + s.op, "--workdir", "/recover/home",
		"--env", "HOME=/tmp", "--env", "TERM=" + s.term, "--env", "PATH=" + recoverPath, "--env", "BDOCK_TOOLBOX=" + toolboxMount, "--env", "SSL_CERT_FILE=" + recoverCABundle,
		"--env", "XDG_DATA_DIRS=" + recoverShareDirs, "--env", "WHR_RECOVER=1",
		"-v", home, "-v", s.toolbox + ":" + toolboxMount + ":ro", "-v", s.nixbox + ":" + nixboxMount + ":ro", "--entrypoint", "/bin/sh", s.image, "-c", recoverShell}
}

// admitRecoverRun accepts exactly recoverRunArgv for validated values.
func admitRecoverRun(args []string) bool {
	probe := recoverRunArgv(recoverSpec{op: "aaaaaaaaaaaaaaaa", home: "HOMEVOL", toolbox: "TBXVOL", nixbox: "NIXVOL", image: "IMGREF", term: "TERMV"})
	if len(args) != len(probe) {
		return false
	}
	at := func(want string) int {
		for i, v := range probe {
			if strings.Contains(v, want) {
				return i
			}
		}
		return -1
	}
	op, ok := strings.CutPrefix(args[at("whr-recover-")], "whr-recover-")
	home, rw := strings.CutSuffix(args[at("HOMEVOL")], ":/recover/home")
	if !rw {
		var ro bool
		if home, ro = strings.CutSuffix(args[at("HOMEVOL")], ":/recover/home:ro"); !ro {
			return false
		}
	}
	toolbox, tok := strings.CutSuffix(args[at("TBXVOL")], ":"+toolboxMount+":ro")
	nixbox, nok := strings.CutSuffix(args[at("NIXVOL")], ":"+nixboxMount+":ro")
	term, tmok := strings.CutPrefix(args[at("TERM=TERMV")], "TERM=")
	network := args[at("--network")+1]
	if !ok || !tok || !nok || !tmok || !homeOperation(op) || !validHomeVolume(home) || !toolboxVolumeRE.MatchString(toolbox) || !nixboxVolumeRE.MatchString(nixbox) || !termNameRE.MatchString(term) || (network != "none" && network != "bridge") || args[at("IMGREF")] != RecoverImage() {
		return false
	}
	return reflect.DeepEqual(args, recoverRunArgv(recoverSpec{op: op, home: home, toolbox: toolbox, nixbox: nixbox, image: RecoverImage(), term: term, rw: rw, net: network == "bridge"}))
}

// runInteractive hands the terminal to one admitted docker run and returns its status.
func (c Client) runInteractive(ctx context.Context, endpoint string, s RecoverStreams, args ...string) (int, error) {
	if !homeEndpoint(endpoint) || !admitRecoverRun(args) {
		return 0, fmt.Errorf("unsupported recover run")
	}
	exe, err := c.dockerExecutable()
	if err != nil {
		return 0, err
	}
	env, cleanEnv, err := ClientEnvironment()
	if err != nil {
		return 0, err
	}
	defer cleanEnv()
	cmd := exec.CommandContext(ctx, exe, append([]string{"--host=" + endpoint}, args...)...)
	cmd.Env, cmd.Stdin, cmd.Stdout, cmd.Stderr = env, s.Stdin, s.Stdout, s.Stderr
	err = cmd.Run()
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	return 0, err
}
