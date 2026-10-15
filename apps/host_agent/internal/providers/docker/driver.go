// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// DriverPolicy is trusted native expectation data. A classified call still needs
// current approval/fence and fresh full resource inspection before any forwarding.
type DriverPolicy struct {
	Phase                                                               Phase
	OverrideCommand                                                     bool
	Scope                                                               Scope
	KnownIDs, ImageRefs                                                 map[string]bool
	DataPath, ContainerUser, RemoteUser, StartupSHA256, RuntimeMetadata string
	StartupTail                                                         []string
	ContainerEnv, RemoteEnv                                             map[string]string
	Build                                                               *BuildPolicy
}
type BuildPolicy struct {
	Context, Scratch, Tag, Target string
	Args                          map[string]string
}
type RunCall struct {
	Name, Image, ImageID, User, Mount string
	Labels, Env                       map[string]string
	Command                           []string
}
type ExecCall struct {
	ContainerID, User, Directory string
	Env                          map[string]string
	Command                      []string
}
type BuildCall struct {
	Context, Dockerfile, Tag, Target string
	Args                             map[string]string
}
type Call struct {
	Scope     Scope
	Kind      string
	IDs       []string
	Reference string
	Run       *RunCall
	Exec      *ExecCall
	Build     *BuildCall
}

func refused() (Call, error)                 { return Call{}, fail("provider_refused") }
func knownID(p DriverPolicy, id string) bool { return hexID.MatchString(id) && p.KnownIDs[id] }
func discovery(args []string, s Scope) bool {
	if len(args) < 3 || !exactArgs(args[:3], "ps", "-q", "-a") {
		return false
	}
	expected := s.LabelList()
	if len(args) != 3+2*len(expected) {
		return false
	}
	seen := map[string]bool{}
	for i := 3; i < len(args); i += 2 {
		if args[i] != "--filter" || !strings.HasPrefix(args[i+1], "label=") {
			return false
		}
		label := strings.TrimPrefix(args[i+1], "label=")
		if seen[label] {
			return false
		}
		seen[label] = true
	}
	for _, label := range expected {
		if !seen[label] {
			return false
		}
	}
	return true
}
func Classify(p DriverPolicy, args []string) (Call, error) {
	if !p.Scope.valid() || len(args) == 0 || (p.Phase != Preflight && p.Phase != Resources && p.Phase != Hooks) {
		return refused()
	}
	if p.Phase != Preflight {
		_, _, cv := parseUser(p.ContainerUser)
		_, _, rv := parseUser(p.RemoteUser)
		if !cv || !rv || (p.ContainerUser != p.RemoteUser && p.ContainerUser != "0:0") {
			return refused()
		}
	}
	for _, arg := range args {
		if !utf8.ValidString(arg) || strings.ContainsRune(arg, 0) {
			return refused()
		}
	}
	switch {
	case exactArgs(args, "-v"):
		return Call{Kind: "version"}, nil
	case exactArgs(args, "version", "--format", "{{.Server.Version}}"):
		return Call{Kind: "engine_version"}, nil
	case exactArgs(args, "buildx", "version"):
		return Call{Kind: "buildx_version"}, nil
	case discovery(args, p.Scope):
		return Call{Kind: "discovery"}, nil
	case len(args) >= 4 && exactArgs(args[:3], "inspect", "--type", "image"):
		if len(args) != 4 || !p.ImageRefs[args[3]] {
			return refused()
		}
		return Call{Kind: "image_inspect", Reference: args[3]}, nil
	case len(args) >= 4 && exactArgs(args[:3], "inspect", "--type", "container"):
		ids := []string{}
		seen := map[string]bool{}
		for _, id := range args[3:] {
			if !knownID(p, id) || seen[id] {
				return refused()
			}
			seen[id] = true
			ids = append(ids, id)
		}
		return Call{Kind: "container_inspect", IDs: ids}, nil
	}
	if p.Phase == Preflight {
		return refused()
	}
	if args[0] == "exec" {
		return classifyExec(p, args)
	}
	if p.Phase != Resources {
		return refused()
	}
	switch {
	case len(args) == 2 && args[0] == "pull" && p.ImageRefs[args[1]]:
		return Call{Kind: "pull", Reference: args[1]}, nil
	case len(args) == 2 && args[0] == "start" && knownID(p, args[1]):
		return Call{Kind: "start", IDs: []string{args[1]}}, nil
	case exactArgs(args, "events", "--format", "{{json .}}", "--filter", "event=start"):
		return Call{Kind: "events"}, nil
	case args[0] == "run":
		return classifyRun(p, args)
	case len(args) >= 2 && exactArgs(args[:2], "buildx", "build"):
		return classifyBuild(p, args)
	default:
		return refused()
	}
}
func pair(value string) (string, string, bool) {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
func equalMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if value, ok := b[k]; !ok || value != v {
			return false
		}
	}
	return true
}
func classifyExec(p DriverPolicy, args []string) (Call, error) {
	c := &ExecCall{Env: map[string]string{}}
	interactive := false
	seen := map[string]bool{}
	i := 1
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		key := args[i]
		if key == "-i" {
			if interactive {
				return refused()
			}
			interactive = true
			i++
			continue
		}
		if key != "-u" && key != "-e" && key != "-w" || i+1 >= len(args) {
			return refused()
		}
		value := args[i+1]
		i += 2
		switch key {
		case "-u":
			if seen[key] || (value != p.RemoteUser && value != p.ContainerUser && value != "root") {
				return refused()
			}
			c.User = value
			seen[key] = true
		case "-w":
			if seen[key] || value != "/workspace" {
				return refused()
			}
			c.Directory = value
			seen[key] = true
		case "-e":
			name, val, ok := pair(value)
			if !ok {
				return refused()
			}
			expected, allowed := p.RemoteEnv[name]
			if _, duplicate := c.Env[name]; duplicate || !allowed || val != expected {
				return refused()
			}
			c.Env[name] = val
		}
	}
	if !interactive || !seen["-u"] || i+1 >= len(args) || !knownID(p, args[i]) {
		return refused()
	}
	c.ContainerID = args[i]
	c.Command = append([]string{}, args[i+1:]...)
	if c.Command[0] == "" {
		return refused()
	}
	return Call{Kind: "exec", Exec: c}, nil
}
func classifyRun(p DriverPolicy, args []string) (Call, error) {
	c := &RunCall{Labels: map[string]string{}, Env: map[string]string{}}
	seen := map[string]bool{}
	attach := map[string]bool{}
	i := 1
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		key := args[i]
		if key == "--sig-proxy=false" {
			if seen[key] {
				return refused()
			}
			seen[key] = true
			i++
			continue
		}
		if i+1 >= len(args) {
			return refused()
		}
		value := args[i+1]
		i += 2
		switch key {
		case "-a":
			if (value != "STDOUT" && value != "STDERR") || attach[value] {
				return refused()
			}
			attach[value] = true
		case "--mount", "-u", "--name", "--entrypoint":
			if seen[key] {
				return refused()
			}
			seen[key] = true
			switch key {
			case "--mount":
				if value != "type=bind,source="+p.DataPath+",target=/workspace" || !validData(p.DataPath) {
					return refused()
				}
				c.Mount = value
			case "-u":
				if value != p.ContainerUser {
					return refused()
				}
				c.User = value
			case "--name":
				if value != p.Scope.Name() {
					return refused()
				}
				c.Name = value
			case "--entrypoint":
				if value != "/bin/sh" {
					return refused()
				}
			}
		case "-l":
			name, val, ok := pair(value)
			if !ok {
				return refused()
			}
			if _, duplicate := c.Labels[name]; duplicate {
				return refused()
			}
			expected, mandatory := p.Scope.Labels()[name]
			if (!mandatory && name != "devcontainer.metadata") || (mandatory && val != expected) {
				return refused()
			}
			c.Labels[name] = val
		case "-e":
			name, val, ok := pair(value)
			if !ok {
				return refused()
			}
			if _, duplicate := c.Env[name]; duplicate {
				return refused()
			}
			c.Env[name] = val
		default:
			return refused()
		}
	}
	if !seen["--sig-proxy=false"] || len(attach) != 2 || !seen["--mount"] || !seen["-u"] || !seen["--name"] || !seen["--entrypoint"] || i >= len(args) || !p.ImageRefs[args[i]] || !equalMap(c.Env, p.ContainerEnv) {
		return refused()
	}
	for name, val := range p.Scope.Labels() {
		if c.Labels[name] != val {
			return refused()
		}
	}
	c.Image = args[i]
	c.Command = append([]string{}, args[i+1:]...)
	if len(c.Command) != 3+len(p.StartupTail) || c.Command[0] != "-c" || c.Command[2] != "-" {
		return refused()
	}
	sum := sha256.Sum256([]byte(c.Command[1]))
	if hex.EncodeToString(sum[:]) != p.StartupSHA256 {
		return refused()
	}
	for i, value := range p.StartupTail {
		if c.Command[i+3] != value {
			return refused()
		}
	}
	return Call{Kind: "run", Run: c}, nil
}
func classifyBuild(p DriverPolicy, args []string) (Call, error) {
	if p.Build == nil || !filepath.IsAbs(p.Build.Context) || !filepath.IsAbs(p.Build.Scratch) {
		return refused()
	}
	b := &BuildCall{Args: map[string]string{}}
	seen := map[string]bool{}
	i := 2
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		key := args[i]
		if key == "--load" {
			if seen[key] {
				return refused()
			}
			seen[key] = true
			i++
			continue
		}
		if i+1 >= len(args) {
			return refused()
		}
		value := args[i+1]
		i += 2
		switch key {
		case "-f", "-t", "--target":
			if seen[key] {
				return refused()
			}
			seen[key] = true
			switch key {
			case "-f":
				b.Dockerfile = value
			case "-t":
				b.Tag = value
			case "--target":
				b.Target = value
			}
		case "--build-arg":
			name, val, ok := pair(value)
			if !ok {
				return refused()
			}
			if _, duplicate := b.Args[name]; duplicate {
				return refused()
			}
			if strings.HasPrefix(name, "BUILDKIT_") && (name != "BUILDKIT_INLINE_CACHE" || val != "1") {
				return refused()
			}
			if strings.HasPrefix(name, "_DEV_CONTAINERS_") && name != "_DEV_CONTAINERS_BASE_IMAGE" {
				return refused()
			}
			b.Args[name] = val
		default:
			return refused()
		}
	}
	if !seen["--load"] || i != len(args)-1 || args[i] != p.Build.Context || b.Tag != p.Build.Tag || b.Target != p.Build.Target || !equalMap(b.Args, p.Build.Args) {
		return refused()
	}
	if !filepath.IsAbs(b.Dockerfile) || filepath.Clean(b.Dockerfile) != b.Dockerfile || filepath.Base(b.Dockerfile) != "Dockerfile-with-features" {
		return refused()
	}
	rel, e := filepath.Rel(p.Build.Scratch, b.Dockerfile)
	if e != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return refused()
	}
	b.Context = args[i]
	return Call{Kind: "build", Build: b}, nil
}
