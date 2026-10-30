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

// whr workspace recover: a throwaway container with the workspace home mounted
// (read-only by default) and a BundleDock toolbox mounted at /opt/bdock/toolbox.
// The base image is built locally on first use; nothing is deleted.

const (
	toolboxRepo       = "quay.io/bundledock/toolbox"
	toolboxMount      = "/opt/bdock/toolbox"
	recoverNameKey    = "workharbor.recover.operation"
	recoverPath       = toolboxMount + "/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	recoverShell      = `if [ -x ` + toolboxMount + `/bin/fish ]; then exec ` + toolboxMount + `/bin/fish -l; fi; exec /bin/sh -l`
	defaultRecoverTag = "latest"
)

var (
	toolboxVolumeRE = regexp.MustCompile(`^bundledock-toolbox-[0-9a-f]{64}$`)
	toolboxImageRE  = regexp.MustCompile(`^quay\.io/bundledock/toolbox:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	toolboxTagRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	toolboxHexRE    = regexp.MustCompile(`^[0-9a-f]{8,64}$`)
	imageIDRE       = regexp.MustCompile(`^sha256:([0-9a-f]{64})$`)
	termNameRE      = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`)
)

// RecoverOptions selects the workspace home and the toolbox to mount.
type RecoverOptions struct {
	Target  string // exact container name or full ID
	Toolbox string // "", "latest", a tag, "current", or a local toolbox volume (id prefix)
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
	ReadOnly      bool             `json:"read_only"`
	Network       bool             `json:"network"`
	ExitStatus    int              `json:"exit_status"`
	Actions       []HomeCopyAction `json:"actions"`
	Residuals     []string         `json:"residuals"`
}

type toolboxChoice struct{ kind, value string } // kind: image, current, volume

func parseToolboxChoice(s string) (toolboxChoice, error) {
	bad := func() (toolboxChoice, error) {
		return toolboxChoice{}, exitcode.Errorf(exitcode.PathBadArguments, "--toolbox takes latest, a tag, current, or a local toolbox volume id")
	}
	switch {
	case s == "":
		return toolboxChoice{"image", toolboxRepo + ":" + defaultRecoverTag}, nil
	case s == "current":
		return toolboxChoice{kind: "current"}, nil
	case toolboxVolumeRE.MatchString(s):
		return toolboxChoice{"volume", strings.TrimPrefix(s, "bundledock-toolbox-")}, nil
	case toolboxHexRE.MatchString(s):
		return toolboxChoice{"volume", s}, nil
	case toolboxTagRE.MatchString(s):
		return toolboxChoice{"image", toolboxRepo + ":" + s}, nil
	}
	return bad()
}

type recoverPlan struct {
	source     HomeCopySource
	toolbox    string // human description
	toolboxVol string // known local volume, or empty when an image is pulled
	toolboxRef string // image to pull, or empty for a local volume
	rw, net    bool
	term       string
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

// PrepareRecover resolves the home and the toolbox choice without any mutation.
func (c Client) PrepareRecover(parent context.Context, ports doctor.Ports, o RecoverOptions) (*Recover, error) {
	if strings.TrimSpace(o.Target) == "" {
		return nil, exitcode.Errorf(exitcode.PathBadArguments, "use workspace recover --local <exact-name|full-id>")
	}
	if !termNameRE.MatchString(o.Term) {
		o.Term = "xterm-256color"
	}
	choice, err := parseToolboxChoice(o.Toolbox)
	if err != nil {
		return nil, err
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
	switch choice.kind {
	case "image":
		plan.toolboxRef, plan.toolbox = choice.value, choice.value
	case "current":
		for _, m := range item.Mounts {
			if m.Destination == toolboxMount && toolboxVolumeRE.MatchString(m.Name) {
				plan.toolboxVol = m.Name
			}
		}
		if plan.toolboxVol == "" {
			return nil, exitcode.Errorf(exitcode.PathConflict, "the workspace has no toolbox volume mounted").WithRemediation("Use --toolbox latest or a tag.")
		}
		plan.toolbox = plan.toolboxVol
	case "volume":
		volumes, err := c.homeVolumes(ctx, endpoint)
		if err != nil {
			return nil, err
		}
		var found []string
		for v := range volumes {
			if toolboxVolumeRE.MatchString(v) && strings.HasPrefix(v, "bundledock-toolbox-"+choice.value) {
				found = append(found, v)
			}
		}
		switch len(found) {
		case 0:
			return nil, exitcode.Errorf(exitcode.PathTargetAbsent, "no local toolbox volume matches %q", choice.value)
		case 1:
			plan.toolboxVol, plan.toolbox = found[0], found[0]
		default:
			return nil, exitcode.Errorf(exitcode.PathBadArguments, "toolbox id %q is ambiguous; give more characters", choice.value)
		}
	}
	return &Recover{client: c, endpoint: endpoint, plan: plan, newOp: randomOperation}, nil
}

// Preview returns what Start would do.
func (r *Recover) Preview() RecoverResult {
	p := r.plan
	return RecoverResult{Status: "preview", Source: p.source, Image: RecoverImage(), Toolbox: p.toolbox, ToolboxVolume: p.toolboxVol,
		ReadOnly: !p.rw, Network: p.net, Actions: []HomeCopyAction{}, Residuals: []string{}}
}

// Start builds what is missing, then hands the terminal to the recovery shell.
func (r *Recover) Start(parent context.Context, s RecoverStreams) (RecoverResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := r.Preview()
	result.Status = "failed"
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

	if _, err := read("image", "inspect", "--format", "{{.Id}}", "--", RecoverImage()); err != nil {
		if parent.Err() != nil {
			return result, parent.Err()
		}
		i := act("recover_image_build", RecoverImage())
		if reply, err := c.homeMutateIO(parent, r.endpoint, strings.NewReader(string(recoverDockerfile)), nil, "build", "--quiet", "--tag", RecoverImage(), "-"); err != nil || reply.status != 0 {
			if parent.Err() != nil {
				return result, parent.Err()
			}
			return fail(exitcode.PathConflict, "The recovery image needs network once to install its packages.", "recovery image build failed")
		}
		result.Actions[i].Applied = true
	}

	volume := p.toolboxVol
	if p.toolboxRef != "" {
		i := act("toolbox_pull", p.toolboxRef)
		if reply, err := c.homeMutate(parent, r.endpoint, "image", "pull", "--quiet", "--", p.toolboxRef); err != nil || reply.status != 0 {
			if parent.Err() != nil {
				return result, parent.Err()
			}
			return fail(exitcode.PathConflict, "Check the tag and the network, or use --toolbox current.", "toolbox image pull failed: %s", p.toolboxRef)
		}
		result.Actions[i].Applied = true
		raw, err := read("image", "inspect", "--format", "{{.Id}}", "--", p.toolboxRef)
		m := imageIDRE.FindStringSubmatch(strings.TrimSpace(string(raw)))
		if err != nil || m == nil {
			return fail(exitcode.PathConflict, "", "the pulled toolbox image has no usable ID")
		}
		id := m[1]
		volume = "bundledock-toolbox-" + id
		volumes, err := c.homeVolumes(parent, r.endpoint)
		if err != nil {
			return result, err
		}
		if !volumes[volume] {
			i = act("toolbox_volume", volume)
			if reply, err := c.homeMutate(parent, r.endpoint, "volume", "create", "--label", "workharbor.recover=toolbox", "--", volume); err != nil || reply.status != 0 {
				if parent.Err() != nil {
					return result, parent.Err()
				}
				return fail(exitcode.PathConflict, "", "toolbox volume %s could not be created", volume)
			}
			result.Actions[i].Applied = true
			result.Residuals = append(result.Residuals, "volume:"+volume)
			i = act("toolbox_populate", volume)
			if reply, err := c.homeMutate(parent, r.endpoint, "run", "--rm", "--pull", "never", "--network", "none", "--mount", "type=volume,src="+volume+",dst=/bundle,volume-nocopy", id); err != nil || reply.status != 0 {
				if parent.Err() != nil {
					return result, parent.Err()
				}
				return fail(exitcode.PathPartialFailure, "Remove the incomplete toolbox volume yourself (docker volume rm "+volume+"); whr deletes nothing automatically.", "toolbox volume %s is incomplete", volume)
			}
			result.Actions[i].Applied = true
			result.Residuals = nil
		}
	}
	result.ToolboxVolume = volume

	i := act("shell", p.source.Volume)
	status, err := c.runInteractive(parent, r.endpoint, s, recoverRunArgv(recoverSpec{op: op, home: p.source.Volume, toolbox: volume, image: RecoverImage(), term: p.term, rw: p.rw, net: p.net})...)
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

type recoverSpec struct {
	op, home, toolbox, image, term string
	rw, net                        bool
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
		"--env", "HOME=/tmp", "--env", "TERM=" + s.term, "--env", "PATH=" + recoverPath, "--env", "XDG_DATA_DIRS=" + toolboxMount + "/share:/usr/share", "--env", "WHR_RECOVER=1",
		"-v", home, "-v", s.toolbox + ":" + toolboxMount + ":ro", "--entrypoint", "/bin/sh", s.image, "-c", recoverShell}
}

// admitRecoverRun accepts exactly recoverRunArgv for validated values.
func admitRecoverRun(args []string) bool {
	probe := recoverRunArgv(recoverSpec{op: "aaaaaaaaaaaaaaaa", home: "HOMEVOL", toolbox: "TBXVOL", image: "IMGREF", term: "TERMV"})
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
	term, tmok := strings.CutPrefix(args[at("TERM=TERMV")], "TERM=")
	network := args[at("--network")+1]
	if !ok || !tok || !tmok || !homeOperation(op) || !validHomeVolume(home) || !toolboxVolumeRE.MatchString(toolbox) || !termNameRE.MatchString(term) || (network != "none" && network != "bridge") || args[at("IMGREF")] != RecoverImage() {
		return false
	}
	return reflect.DeepEqual(args, recoverRunArgv(recoverSpec{op: op, home: home, toolbox: toolbox, image: RecoverImage(), term: term, rw: rw, net: network == "bridge"}))
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
