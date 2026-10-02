// Package devcontainer reads the safe subset of a repository's
// .devcontainer/devcontainer.json (design D38). The file comes from the default
// branch only, because an agent can edit every file in its topic and must not
// configure its own environment. Even reviewed files may request, never grant:
// keys that widen the environment are refused, and everything outside the
// subset is ignored with a note.
package devcontainer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/wstein/workharbor/internal/devcontainer/feature"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

// Paths are where a devcontainer.json is looked for, in the order of the
// specification.
var Paths = []string{".devcontainer/devcontainer.json", ".devcontainer.json"}

// Config is what the reader honours. It is a request: the supervisor decides
// what to build and what network access to grant.
type Config struct {
	Image string // a ready image; empty when Dockerfile is set

	// Dockerfile and Context come from build.dockerfile and build.context, both
	// relative to the devcontainer.json's directory as written in the file.
	Dockerfile string
	Context    string
	BuildArgs  map[string]string // build.args

	Env map[string]string // containerEnv

	// Features are the ids of the devcontainer features the file asks for, sorted.
	// FeatureRequests are the same with their options, in the order of the file,
	// which decides the order they install in (D38, issue #108). They are requests:
	// Resolve fetches and checks them, and the builder applies what it accepted.
	Features        []string
	FeatureRequests []feature.Request

	// ForwardPorts are the ports the file forwards, which become the preview
	// ports of D33. Only plain port numbers are read.
	ForwardPorts []int

	// PostCreate are the commands to run inside the environment after it is
	// created, in order. A string runs through a shell; an array is one command.
	PostCreate []Command

	// EgressRequests are hosts customizations.workharbor.egress asks for. Each
	// becomes a Decision for the human; none is allowed by being listed.
	EgressRequests []string

	// SourceDigest is the SHA-256 of the devcontainer.json as read, set by Resolve.
	// An egress answer remembers it, so the published preset can ask again when
	// the file that requested a host has changed (D47).
	SourceDigest string

	// Hints are the other customizations.workharbor values. They suggest, and
	// the supervisor's configuration decides.
	Hints Hints

	// Notes say which keys were ignored.
	Notes []string
}

// Hints are what customizations.workharbor may suggest besides egress hosts.
type Hints struct {
	Check        string   // the command that checks the work
	PreviewPorts []int    // ports to preview, as in D33
	Agent        string   // the name of the agent to prefer
	Tools        []string // the tools to allow the agent, by name
}

// Command is one post-create command.
type Command struct {
	Shell string   // run through `sh -c`; empty when Argv is set
	Argv  []string // run directly
}

// ErrRefused is matched by the error Parse returns for a refused key.
var ErrRefused = errors.New("devcontainer.json asks for something workharbor refuses")

// RefusedError lists every refused key with the reason.
type RefusedError struct{ Keys []string }

func (e *RefusedError) Error() string {
	return "devcontainer.json asks for something workharbor refuses: " + strings.Join(e.Keys, "; ")
}

// Is makes errors.Is(err, ErrRefused) true.
func (e *RefusedError) Is(target error) bool { return target == ErrRefused }

// refused keys and why (D38).
var refused = map[string]string{
	"initializeCommand": "it runs on the host",
	"mounts":            "mounts come only from the supervisor's configuration",
	"runArgs":           "arguments to the runtime come only from the supervisor",
	"privileged":        "an agent environment is never privileged",
	"capAdd":            "capabilities are dropped, never added",
	"securityOpt":       "security options come only from the supervisor",
	"workspaceMount":    "mounts come only from the supervisor's configuration",
}

func isReservedEnv(name string) bool { return runtime.ReservedEnv(name) }

// Parse reads devcontainer.json (JSON with comments and trailing commas).
func Parse(data []byte) (Config, error) {
	clean, err := stripJSONC(data)
	if err != nil {
		return Config{}, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(clean, &raw); err != nil {
		return Config{}, fmt.Errorf("devcontainer.json: %w", err)
	}

	var problems []string
	for key, why := range refused {
		if _, ok := raw[key]; ok {
			problems = append(problems, key+" ("+why+")")
		}
	}
	for _, key := range []string{"remoteUser", "containerUser"} {
		if v, ok := raw[key]; ok {
			var user string
			if json.Unmarshal(v, &user) != nil || isRoot(user) {
				problems = append(problems, key+" (a root user is refused)")
			}
		}
	}
	if v, ok := raw["containerEnv"]; ok {
		var env map[string]string
		if json.Unmarshal(v, &env) == nil {
			for name := range env {
				if isReservedEnv(name) {
					problems = append(problems, "containerEnv."+name+" (the supervisor sets it)")
				}
			}
		}
	}
	if v, ok := raw["build"]; ok {
		var b buildSection
		if json.Unmarshal(v, &b) == nil {
			for key, p := range map[string]string{"build.dockerfile": b.Dockerfile, "build.context": b.Context} {
				if p != "" && (path.IsAbs(p) || strings.Contains(p, "\\")) {
					problems = append(problems, key+" (an absolute path is refused)")
				}
			}
			for name := range b.Args {
				if isReservedEnv(name) {
					problems = append(problems, "build.args."+name+" (the supervisor sets it)")
				}
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return Config{}, &RefusedError{Keys: problems}
	}

	var c Config
	var notes []string
	for key, v := range raw {
		switch key {
		case "image":
			if err := json.Unmarshal(v, &c.Image); err != nil {
				return Config{}, fmt.Errorf("devcontainer.json: image: %w", err)
			}
		case "build":
			var b buildSection
			if err := json.Unmarshal(v, &b); err != nil {
				return Config{}, fmt.Errorf("devcontainer.json: build: %w", err)
			}
			c.Dockerfile, c.Context, c.BuildArgs = b.Dockerfile, b.Context, b.Args
		case "containerEnv":
			if err := json.Unmarshal(v, &c.Env); err != nil {
				return Config{}, fmt.Errorf("devcontainer.json: containerEnv: %w", err)
			}
		case "postCreateCommand":
			cmds, err := commands(v)
			if err != nil {
				if errors.Is(err, errObjectForm) {
					notes = append(notes, "postCreateCommand: the object form (parallel commands) is not supported and was ignored")
					continue
				}
				return Config{}, fmt.Errorf("devcontainer.json: postCreateCommand: %w", err)
			}
			c.PostCreate = cmds
		case "customizations":
			c.EgressRequests, c.Hints, notes = customizations(v, notes)
		case "features":
			reqs, err := featureRequests(v)
			if err != nil {
				return Config{}, fmt.Errorf("devcontainer.json: features: %w", err)
			}
			c.FeatureRequests = reqs
			for _, r := range reqs {
				c.Features = append(c.Features, r.ID)
			}
			sort.Strings(c.Features)
		case "forwardPorts":
			c.ForwardPorts, notes = ports("forwardPorts", v, notes)
		case "name", "$schema":
		default:
			notes = append(notes, key+": not in the supported subset, ignored")
		}
	}
	if c.Image == "" && c.Dockerfile == "" {
		return Config{}, errors.New("devcontainer.json: neither image nor build.dockerfile is set")
	}
	if c.Image != "" && c.Dockerfile != "" {
		return Config{}, errors.New("devcontainer.json: image and build.dockerfile are both set")
	}
	sort.Ints(c.ForwardPorts)
	sort.Strings(notes)
	c.Notes = notes
	return c, nil
}

// isRoot reports a root user: the name root, or a numeric uid of 0 in any
// spelling ("0", "00"). A group is not judged here; the runtime spec refuses
// gid 0.
func isRoot(user string) bool {
	u, _, _ := strings.Cut(strings.TrimSpace(user), ":")
	if u == "root" {
		return true
	}
	n, err := strconv.Atoi(u)
	return err == nil && n == 0
}

var errObjectForm = errors.New("object form")

func commands(v json.RawMessage) ([]Command, error) {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return []Command{{Shell: s}}, nil
	}
	var a []string
	if json.Unmarshal(v, &a) == nil {
		return []Command{{Argv: a}}, nil
	}
	var o map[string]json.RawMessage
	if json.Unmarshal(v, &o) == nil {
		return nil, errObjectForm
	}
	return nil, errors.New("want a string or an array of strings")
}

// buildSection is the build object.
type buildSection struct {
	Dockerfile string            `json:"dockerfile"`
	Context    string            `json:"context"`
	Args       map[string]string `json:"args"`
}

// ports reads a list of port numbers. A port may be a number or a string of
// digits; "host:port" forms and anything out of range are noted and dropped.
func ports(key string, v json.RawMessage, notes []string) ([]int, []string) {
	var list []json.RawMessage
	if json.Unmarshal(v, &list) != nil {
		return nil, append(notes, key+": not a list, ignored")
	}
	var out []int
	seen := map[int]bool{}
	for _, item := range list {
		var n int
		var s string
		switch {
		case json.Unmarshal(item, &n) == nil:
		case json.Unmarshal(item, &s) == nil:
			var err error
			if n, err = strconv.Atoi(s); err != nil {
				notes = append(notes, key+": "+strconv.Quote(s)+" is not a plain port number, ignored")
				continue
			}
		default:
			notes = append(notes, key+": an entry is not a port number, ignored")
			continue
		}
		if n < 1 || n > 65535 {
			notes = append(notes, key+": "+strconv.Itoa(n)+" is out of range, ignored")
			continue
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out, notes
}

// ValidHost reports whether name can be requested as an egress host.
func ValidHost(name string) bool { return domain.ValidHost(name) }

func customizations(v json.RawMessage, notes []string) ([]string, Hints, []string) {
	var c map[string]json.RawMessage
	if json.Unmarshal(v, &c) != nil {
		return nil, Hints{}, append(notes, "customizations: not an object, ignored")
	}
	var egress []string
	var hints Hints
	for tool, body := range c {
		if tool != "workharbor" {
			notes = append(notes, "customizations."+tool+": ignored")
			continue
		}
		var w struct {
			Egress       []string        `json:"egress"`
			Check        string          `json:"check"`
			PreviewPorts json.RawMessage `json:"previewPorts"`
			Agent        string          `json:"agent"`
			Tools        []string        `json:"tools"`
		}
		if err := json.Unmarshal(body, &w); err != nil {
			notes = append(notes, "customizations.workharbor: unreadable, ignored")
			continue
		}
		seen := map[string]bool{}
		for _, h := range w.Egress {
			h = strings.ToLower(strings.TrimSpace(h))
			switch {
			case strings.Contains(h, "*"):
				notes = append(notes, "customizations.workharbor.egress: "+strconv.Quote(h)+" is a wildcard, which only the supervisor's own configuration may allow; a repository asks for one exact host, ignored")
			case !ValidHost(h):
				notes = append(notes, "customizations.workharbor.egress: "+strconv.Quote(h)+" is not a host name, ignored")
			case !seen[h]:
				seen[h] = true
				egress = append(egress, h)
			}
		}
		hints.Check, hints.Agent, hints.Tools = strings.TrimSpace(w.Check), strings.TrimSpace(w.Agent), w.Tools
		if len(w.PreviewPorts) > 0 {
			hints.PreviewPorts, notes = ports("customizations.workharbor.previewPorts", w.PreviewPorts, notes)
			sort.Ints(hints.PreviewPorts)
		}
	}
	return egress, hints, notes
}

// Runner runs git on the repository, as hostgit.Repo does.
type Runner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

// ErrBadRef is returned for a ref that could be read as an option or a path.
var ErrBadRef = errors.New("devcontainer: not a usable ref")

// Read reads the devcontainer.json of ref (the default branch) from the
// repository, never from a working tree. found is false when the repository has
// none. A failure to read the repository is an error, never "not found", so a
// broken repository cannot silently drop the requested image. The file must be
// a regular blob: a symbolic link is refused. build.dockerfile and
// build.context must stay inside the repository.
func Read(ctx context.Context, r Runner, ref string) (c Config, found bool, err error) {
	c, _, found, err = read(ctx, r, ref)
	return c, found, err
}

// read is Read that also says which of Paths the file was found at.
func read(ctx context.Context, r Runner, ref string) (c Config, at string, found bool, err error) {
	if err := checkRef(ref); err != nil {
		return Config{}, "", false, err
	}
	for _, p := range Paths {
		data, err := file(ctx, r, ref, p)
		if errors.Is(err, ErrNotFound) {
			continue // not there at this path
		}
		if err != nil {
			return Config{}, "", errors.Is(err, ErrRefused), err
		}
		c, err := Parse(bytes.TrimSpace(data))
		if err != nil {
			return Config{}, p, true, err
		}
		sum := sha256.Sum256(data)
		c.SourceDigest = hex.EncodeToString(sum[:])
		if err := c.checkPaths(path.Dir(p)); err != nil {
			return Config{}, p, true, err
		}
		return c, p, true, nil
	}
	return Config{}, "", false, nil
}

// checkPaths refuses a build.dockerfile or build.context that, resolved against
// the directory of the devcontainer.json, leaves the repository.
func (c Config) checkPaths(dir string) error {
	var problems []string
	for key, p := range map[string]string{"build.dockerfile": c.Dockerfile, "build.context": c.Context} {
		if p == "" {
			continue
		}
		if j := path.Join(dir, p); j == ".." || strings.HasPrefix(j, "../") {
			problems = append(problems, key+" (it leads outside the repository)")
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return &RefusedError{Keys: problems}
	}
	return nil
}

// stripJSONC removes // and /* */ comments and trailing commas, outside strings.
func stripJSONC(in []byte) ([]byte, error) {
	var out bytes.Buffer
	inString := false
	for i := 0; i < len(in); i++ {
		ch := in[i]
		switch {
		case inString:
			out.WriteByte(ch)
			if ch == '\\' && i+1 < len(in) {
				i++
				out.WriteByte(in[i])
			} else if ch == '"' {
				inString = false
			}
		case ch == '"':
			inString = true
			out.WriteByte(ch)
		case ch == '/' && i+1 < len(in) && in[i+1] == '/':
			for i < len(in) && in[i] != '\n' {
				i++
			}
			out.WriteByte('\n')
		case ch == '/' && i+1 < len(in) && in[i+1] == '*':
			end := bytes.Index(in[i+2:], []byte("*/"))
			if end < 0 {
				return nil, errors.New("devcontainer.json: unterminated comment")
			}
			i += 2 + end + 1
			out.WriteByte(' ')
		default:
			out.WriteByte(ch)
		}
	}
	return dropTrailingCommas(out.Bytes()), nil
}

func dropTrailingCommas(in []byte) []byte {
	var out bytes.Buffer
	inString := false
	for i := 0; i < len(in); i++ {
		ch := in[i]
		if inString {
			out.WriteByte(ch)
			if ch == '\\' && i+1 < len(in) {
				i++
				out.WriteByte(in[i])
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
		}
		if ch == ',' {
			j := i + 1
			for j < len(in) && (in[j] == ' ' || in[j] == '\n' || in[j] == '\t' || in[j] == '\r') {
				j++
			}
			if j < len(in) && (in[j] == '}' || in[j] == ']') {
				continue
			}
		}
		out.WriteByte(ch)
	}
	return out.Bytes()
}

// featureRequests reads the features object in the order of the file, with each
// feature's options as the strings an install script reads: a string value is the
// "version" option, true or an empty object sets nothing, an object gives its options.
func featureRequests(raw json.RawMessage) ([]feature.Request, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("not an object")
	}
	var out []feature.Request
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		id, _ := tok.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, fmt.Errorf("%q appears twice", id)
		}
		seen[id] = true
		opts, off, err := featureOptions(val)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		if !off {
			out = append(out, feature.Request{ID: id, Options: opts})
		}
	}
	return out, nil
}

// featureOptions reads one feature's value. off is true for false: the feature is
// switched off.
func featureOptions(raw json.RawMessage) (opts map[string]string, off bool, err error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, false, err
	}
	switch t := v.(type) {
	case nil:
		return nil, false, nil
	case bool:
		return nil, !t, nil
	case string:
		return map[string]string{"version": t}, false, nil
	case map[string]any:
		opts = map[string]string{}
		for k, val := range t {
			switch x := val.(type) {
			case string:
				opts[k] = x
			case bool:
				opts[k] = fmt.Sprint(x)
			case json.Number:
				opts[k] = x.String()
			default:
				return nil, false, fmt.Errorf("option %q is not a string, boolean or number", k)
			}
		}
		return opts, false, nil
	}
	return nil, false, errors.New("not an options object, a version or true")
}
