//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func stageSocketIdentity(endpoint string) (stageEndpointIdentity, error) {
	var result stageEndpointIdentity
	if !strings.HasPrefix(endpoint, "unix:///") {
		return result, stageConflict("local Unix socket capability required")
	}
	physical, e := filepath.EvalSymlinks(strings.TrimPrefix(endpoint, "unix://"))
	if e != nil {
		return result, stageConflict("physical socket unavailable")
	}
	st, e := os.Lstat(physical)
	if e != nil || st.Mode()&os.ModeSocket == 0 {
		return result, stageConflict("physical socket identity unavailable")
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return result, stageConflict("physical socket identity unavailable")
	}
	return stageEndpointIdentity{Endpoint: "unix://" + physical, SocketDevice: uint64(stat.Dev), SocketInode: uint64(stat.Ino)}, nil
}

func admitStageNativeMount(ctx context.Context, daemon stageDaemonInfo, primary inspection, source string) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	// macOS sharing requires a separately approved read-only sharing-root proof;
	// no runtime probe or sharing-configuration mutation substitutes for it.
	if runtime.GOOS != "linux" {
		return stageConflict("native state-root mount-sharing capability unavailable")
	}
	host, e := os.Hostname()
	if e != nil || host == "" || daemon.Name != host || daemon.OSType != "linux" {
		return stageConflict("native daemon filesystem correspondence unavailable")
	}
	kernel, e := os.ReadFile("/proc/sys/kernel/osrelease")
	if e != nil || strings.TrimSpace(string(kernel)) != daemon.KernelVersion {
		return stageConflict("native daemon filesystem correspondence unavailable")
	}
	osName := strings.ToLower(daemon.OperatingSystem)
	for _, vm := range []string{"docker desktop", "orbstack", "linuxkit", "colima", "rancher desktop"} {
		if strings.Contains(osName, vm) {
			return stageConflict("native daemon filesystem correspondence unavailable")
		}
	}
	// Matching native facts plus the already matched primary bind are a
	// cooperative capability sample, not proof of identical mount namespaces.
	_, _, _, e = bindStagePrimary(primary, source)
	return e
}

func bindStagePrimary(primary inspection, source string) (workspace, home, destination string, err error) {
	label := primary.Labels["devcontainer.local_folder"]
	physical, e := filepath.EvalSymlinks(label)
	if e != nil || physical != source {
		return "", "", "", stageConflict("primary physical source binding mismatch")
	}
	binds, homes := 0, 0
	for _, m := range primary.Mounts {
		if m.Type == "bind" {
			p, e := filepath.EvalSymlinks(m.Source)
			if e == nil && p == source {
				if !filepath.IsAbs(m.Destination) || !m.RW {
					return "", "", "", stageConflict("primary workspace mount capability unavailable")
				}
				workspace = m.Destination
				binds++
			}
		}
		if m.Type == "volume" && (m.Destination == "/home/captain" || m.Destination == "/home/vscode") {
			if !volumeName(m.Name) || !m.RW || m.readOnlyUnknown {
				return "", "", "", stageConflict("primary home mount capability unavailable")
			}
			home = m.Name
			destination = m.Destination
			homes++
		}
	}
	if binds != 1 || homes != 1 {
		return "", "", "", stageConflict("primary workspace or home binding ambiguous")
	}
	return workspace, home, destination, nil
}

func admitStageToolVersions(ctx context.Context, r *stageRunner) error {
	for _, name := range []string{"docker", "node", "devcontainer", "git", "bash"} {
		out, e := r.Run(ctx, "metadata", name, []string{"--version"}, nil, 5*time.Second)
		if e != nil {
			return stageConflict("installed " + name + " version capability unavailable")
		}
		version := strings.TrimSpace(string(out.Stdout))
		if i := strings.IndexByte(version, '\n'); i >= 0 {
			version = version[:i]
		}
		if version == "" || len(version) > 4096 || strings.ContainsAny(version, "\x00\r\x1b") {
			return stageConflict("installed " + name + " version observation invalid")
		}
		if name == "devcontainer" && version != "0.89.0" {
			return stageConflict("official Dev Container CLI 0.89.0 required")
		}
		if name == "git" && !stageGitVersionAdmitted(version) {
			return stageConflict("Git 2.39.5 or later required")
		}
		id := r.Tools[name]
		id.Version = version
		r.Tools[name] = id
	}
	// POSIX sh has no standardized version interface (dash reports none). Keep
	// that absence explicit while binding its exact content and fixed capability.
	out, e := r.Run(ctx, "metadata", "sh", []string{"-c", "printf '%s' whr-posix-sh-capability"}, nil, 5*time.Second)
	if e != nil || string(out.Stdout) != "whr-posix-sh-capability" {
		return stageConflict("installed POSIX sh capability unavailable")
	}
	id := r.Tools["sh"]
	id.Version = "unreported"
	if banner, e := r.Run(ctx, "metadata", "sh", []string{"--version"}, nil, 5*time.Second); e == nil {
		line := strings.TrimSpace(string(banner.Stdout))
		if i := strings.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}
		if line != "" && len(line) <= 4096 && !strings.ContainsAny(line, "\x00\r\x1b") {
			id.Version = line
		}
	}
	r.Tools["sh"] = id
	return nil
}
func stageGitVersionAdmitted(version string) bool {
	version = strings.TrimPrefix(version, "git version ")
	parts := strings.Split(version, ".")
	if len(parts) < 3 {
		return false
	}
	values := make([]int, 3)
	for i := 0; i < 3; i++ {
		s := parts[i]
		for j, c := range s {
			if c < '0' || c > '9' {
				s = s[:j]
				break
			}
		}
		n, e := strconv.Atoi(s)
		if e != nil {
			return false
		}
		values[i] = n
	}
	return values[0] > 2 || values[0] == 2 && (values[1] > 39 || values[1] == 39 && values[2] >= 5)
}

func admitStageConfig(config map[string]any, omitted []string) error {
	for _, key := range []string{"dockerComposeFile", "service"} {
		if _, ok := config[key]; ok {
			return stageConflict("Docker Compose local stage capability unavailable")
		}
	}
	if args, ok := config["runArgs"].([]any); ok {
		for i, arg := range args {
			value, _ := arg.(string)
			if value == "--env-file" || strings.HasPrefix(value, "--env-file=") {
				return stageConflict("omitted credential env-file executable input required")
			}
			env := ""
			if value == "-e" || value == "--env" {
				if i+1 < len(args) {
					env, _ = args[i+1].(string)
				}
				if !stageLiteralEnv(env) {
					return stageConflict("unbound host environment copy unsupported")
				}
			}
			if strings.HasPrefix(value, "--env=") {
				if !stageLiteralEnv(strings.TrimPrefix(value, "--env=")) {
					return stageConflict("unbound host environment copy unsupported")
				}
			}
			if strings.HasPrefix(value, "-e") && !strings.HasPrefix(value, "--") && value != "-e" {
				if !stageLiteralEnv(value[2:]) {
					return stageConflict("unbound host environment copy unsupported")
				}
			}
		}
	}
	// Required config paths must survive capture; missing Dockerfile/context or
	// hook dependencies are checked from the actual captured execution view.
	for _, key := range []string{"envFile", "remoteEnvFile"} {
		if _, ok := config[key]; ok {
			return stageConflict("omitted credential env-file executable input required")
		}
	}
	_ = omitted
	return nil
}
func stageLiteralEnv(value string) bool {
	name, _, ok := strings.Cut(value, "=")
	return ok && name != "" && !strings.ContainsRune(value, '\x00')
}
func stageAnchorageHostInitializer(config map[string]any) bool {
	b, _ := json.Marshal(config["initializeCommand"])
	return bytes.Contains(b, []byte("/.devcontainer/host-info.sh"))
}

func (s *LocalStage) prepareCLIConfiguration(ctx context.Context) error {
	policy := stageDriverPolicy{PrimaryID: s.primary.ID, PrimaryName: strings.TrimPrefix(s.primary.Name, "/"), PrimaryHome: s.home, OperationID: s.operationID, WorkspaceSource: s.clonePath, WorkspaceDestination: s.workspace, Home: s.homeIntent.Plan().Mount.Name, HomeDestination: s.homeDestination, AllowedMounts: []string{}, PullReferences: []string{}, InitializerVolumes: map[string]string{}}
	if image, ok := s.config["image"].(string); ok {
		policy.PullReferences = append(policy.PullReferences, image)
	}
	full := stageCopyConfig(s.config)
	full["workspaceFolder"] = s.workspace
	full["workspaceMount"] = renderStageMount("type=bind", "source="+s.clonePath, "target="+s.workspace)
	mounts := []any{}
	if sourceMounts, ok := full["mounts"].([]any); ok {
		for _, value := range sourceMounts {
			raw, ok := value.(string)
			if !ok {
				return stageConflict("configured object mount capability unavailable")
			}
			m, e := stageNativeMount(raw)
			if e != nil {
				return e
			}
			if m["target"] == s.homeDestination {
				continue
			}
			if strings.Contains(raw, "${") {
				return stageConflict("unresolved additional mount capability unavailable")
			}
			policy.AllowedMounts = append(policy.AllowedMounts, raw)
			mounts = append(mounts, raw)
		}
	}
	mounts = append(mounts, renderStageMount("type=volume", "source="+policy.Home, "target="+s.homeDestination))
	full["mounts"] = mounts
	args := []any{}
	if sourceArgs, ok := full["runArgs"].([]any); ok {
		for i := 0; i < len(sourceArgs); i++ {
			raw, ok := sourceArgs[i].(string)
			if !ok {
				return stageConflict("native run argument type unavailable")
			}
			if raw == "--name" || raw == "--hostname" {
				if i+1 >= len(sourceArgs) {
					return stageConflict("native run identity argument invalid")
				}
				i++
				continue
			}
			if strings.HasPrefix(raw, "--name=") || strings.HasPrefix(raw, "--hostname=") {
				continue
			}
			args = append(args, raw)
		}
	}
	full["runArgs"] = append(args, "--name", "whr-anchorage-staging", "--hostname", "anchorage-staging")
	if init, ok := full["initializeCommand"].(map[string]any); ok {
		for _, value := range init {
			a, ok := value.([]any)
			if !ok || len(a) != 5 {
				continue
			}
			script, _ := a[1].(string)
			ref, _ := a[2].(string)
			volume, _ := a[3].(string)
			if strings.HasSuffix(script, "/.devcontainer/initialize.sh") && volumeName(volume) && strings.Contains(ref, "@sha256:") {
				policy.InitializerVolumes[volume] = ref
				policy.PullReferences = append(policy.PullReferences, ref)
			}
		}
	}
	s.runner.Policy = &policy
	driver, e := s.runner.DockerDriver("preview", "")
	if e != nil {
		return e
	}
	configRoot := filepath.Join(s.root, "configuration")
	if e = os.Mkdir(configRoot, 0700); e != nil {
		return stageConflict("private configuration storage unavailable")
	}
	override := filepath.Join(configRoot, "devcontainer.json")
	s.sourceMetadata = stageSourceMetadata(full)
	data, _ := json.Marshal(full)
	if e = os.WriteFile(override, append(data, '\n'), 0600); e != nil {
		return stageConflict("private configuration publication unavailable")
	}
	s.invocation = stageCLIInvocation{Workspace: s.capture.Path, Config: filepath.Join(s.capture.Path, s.configRelative), Override: override, Driver: driver, UserData: filepath.Join(s.runner.Root, "user-data"), Labels: []string{"devcontainer.local_folder=" + s.capture.Source, "anchorage-staging=1", "workharbor.stage.operation=" + s.operationID}}
	argv, e := s.invocation.Arguments("read-configuration")
	if e != nil {
		return e
	}
	out, e := s.runner.Run(ctx, "metadata", "devcontainer", argv, nil, 5*time.Second)
	if e != nil {
		return e
	}
	var response struct{ Configuration, MergedConfiguration map[string]any }
	d := json.NewDecoder(bytes.NewReader(out.Stdout))
	d.UseNumber()
	if e = decodeDockerRecord(d, &response); e != nil || d.Decode(new(any)) != io.EOF || response.Configuration == nil || response.MergedConfiguration == nil {
		return stageConflict("official merged configuration capability unavailable")
	}
	return nil
}

func stageTrustTools(tools map[string]stageToolIdentity) map[string]stageToolIdentity {
	out := map[string]stageToolIdentity{}
	for k, v := range tools {
		if k != "docker_driver" {
			out[k] = v
		}
	}
	return out
}
