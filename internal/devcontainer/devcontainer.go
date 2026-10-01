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
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
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

	Env map[string]string // containerEnv

	// PostCreate are the commands to run inside the environment after it is
	// created, in order. A string runs through a shell; an array is one command.
	PostCreate []Command

	// EgressRequests are hosts customizations.workharbor.egress asks for. Each
	// becomes a Decision for the human; none is allowed by being listed.
	EgressRequests []string

	// Notes say which keys were ignored.
	Notes []string
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

// ignoredLater are keys D38 names as honoured but that this reader does not
// read yet (the full support is issue #76).
var ignoredLater = map[string]bool{"features": true, "forwardPorts": true}

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
			var b struct {
				Dockerfile string `json:"dockerfile"`
				Context    string `json:"context"`
			}
			if err := json.Unmarshal(v, &b); err != nil {
				return Config{}, fmt.Errorf("devcontainer.json: build: %w", err)
			}
			c.Dockerfile, c.Context = b.Dockerfile, b.Context
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
			c.EgressRequests, notes = customizations(v, notes)
		case "name", "$schema":
		default:
			if ignoredLater[key] {
				notes = append(notes, key+": honoured by the full support (#76), ignored for now")
			} else {
				notes = append(notes, key+": not in the supported subset, ignored")
			}
		}
	}
	if c.Image == "" && c.Dockerfile == "" {
		return Config{}, errors.New("devcontainer.json: neither image nor build.dockerfile is set")
	}
	if c.Image != "" && c.Dockerfile != "" {
		return Config{}, errors.New("devcontainer.json: image and build.dockerfile are both set")
	}
	sort.Strings(notes)
	c.Notes = notes
	return c, nil
}

func isRoot(user string) bool {
	u, _, _ := strings.Cut(user, ":")
	return u == "root" || u == "0"
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

func customizations(v json.RawMessage, notes []string) ([]string, []string) {
	var c map[string]json.RawMessage
	if json.Unmarshal(v, &c) != nil {
		return nil, append(notes, "customizations: not an object, ignored")
	}
	var egress []string
	for tool, body := range c {
		if tool != "workharbor" {
			notes = append(notes, "customizations."+tool+": ignored")
			continue
		}
		var w struct {
			Egress []string `json:"egress"`
		}
		if err := json.Unmarshal(body, &w); err != nil {
			notes = append(notes, "customizations.workharbor: unreadable, ignored")
			continue
		}
		egress = w.Egress
	}
	return egress, notes
}

// Runner runs git on the repository, as hostgit.Repo does.
type Runner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

// Read reads the devcontainer.json of ref (the default branch) from the
// repository, never from a working tree. found is false when the repository has
// none.
func Read(ctx context.Context, r Runner, ref string) (c Config, found bool, err error) {
	for _, p := range Paths {
		data, err := r.Run(ctx, "show", ref+":"+p)
		if err != nil {
			continue // not there at this path
		}
		c, err := Parse(bytes.TrimSpace(data))
		return c, true, err
	}
	return Config{}, false, nil
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
