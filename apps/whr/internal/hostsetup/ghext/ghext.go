// SPDX-License-Identifier: EUPL-1.2
// Package ghext offers the GitHub CLI extension gh-stack as a preselected, skippable setup item.
// It reaches gh only through the GH port and contains no host or container branching.
package ghext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

const (
	// ItemID is the setup item id.
	ItemID = "gh-stack"
	// Repo is the extension repository installed by `gh extension install`.
	Repo = "github/gh-stack"
)

// ErrGHMissing means the gh command is not on PATH.
var ErrGHMissing = errors.New("gh not found")

// NetworkError marks an install failure caused by the network.
type NetworkError struct{ Detail string }

func (e *NetworkError) Error() string { return "network unavailable: " + e.Detail }

// GH is the only way this package reaches the GitHub CLI.
type GH interface {
	Available(ctx context.Context) error
	ExtensionList(ctx context.Context) (string, error)
	ExtensionInstall(ctx context.Context, repo string) error
}

// Item describes the setup item for the wizard.
type Item struct {
	ID, Title, Requires          string
	SelectedByDefault, Skippable bool
}

// Describe returns the wizard item: preselected and individually skippable.
func Describe() Item {
	return Item{ID: ItemID, Title: "GitHub CLI extension gh-stack", Requires: "gh", SelectedByDefault: true, Skippable: true}
}

// State is the outcome class of Run.
type State string

const (
	Skipped   State = "skipped"
	Present   State = "present"
	Installed State = "installed"
	Declined  State = "declined"
	Cancelled State = "cancelled"
	Blocked   State = "blocked"
	Failed    State = "failed"
)

// Options controls Run. Selected is false when the user deselected the item.
// Without a terminal (Interactive false) only Yes, the explicit flag, confirms.
type Options struct {
	Selected    bool
	Interactive bool
	Yes         bool
	Confirm     func(prompt string) (bool, error)
	Out         io.Writer
}

// Result is the outcome of Run. Reason is a stable code, Message the line shown.
type Result struct {
	State           State
	Reason, Version string
	Message         string
}

// Run detects, shows, confirms and installs the extension. It never panics on a
// missing gh or a missing network; those are results with a message.
func Run(ctx context.Context, gh GH, opt Options) Result {
	say := func(format string, a ...any) string {
		line := fmt.Sprintf(format, a...)
		if opt.Out != nil {
			fmt.Fprintln(opt.Out, line)
		}
		return line
	}
	done := func(s State, reason, version, format string, a ...any) Result {
		return Result{State: s, Reason: reason, Version: version, Message: say(format, a...)}
	}
	if !opt.Selected {
		return Result{State: Skipped, Reason: "deselected", Message: "gh-stack: skipped"}
	}
	if err := gh.Available(ctx); err != nil {
		if errors.Is(err, ErrGHMissing) {
			return done(Blocked, "gh_missing", "", "gh-stack: the GitHub CLI (gh) was not found on PATH; install gh first, then rerun setup")
		}
		return done(Failed, "detect_failed", "", "gh-stack: cannot run gh: %v", err)
	}
	list, err := gh.ExtensionList(ctx)
	if err != nil {
		return done(Failed, "detect_failed", "", "gh-stack: cannot list gh extensions: %v", err)
	}
	if v, ok := ParseVersion(list, Repo); ok {
		return done(Present, "already_installed", v, "gh-stack: already installed (%s); nothing to do", versionText(v))
	}
	say("gh-stack: not installed. Planned step: gh extension install %s (changes your gh profile)", Repo)
	switch {
	case opt.Yes:
	case !opt.Interactive:
		return done(Blocked, "confirmation_required", "", "gh-stack: confirmation required; no terminal, rerun with the explicit yes flag to install")
	default:
		if opt.Confirm == nil {
			return done(Blocked, "confirmation_required", "", "gh-stack: confirmation required")
		}
		ok, cerr := opt.Confirm("Install gh-stack with: gh extension install " + Repo + "?")
		if cerr != nil {
			return done(Cancelled, "input_closed", "", "gh-stack: not installed (input closed)")
		}
		if !ok {
			return done(Declined, "declined", "", "gh-stack: not installed (declined)")
		}
	}
	if err := gh.ExtensionInstall(ctx, Repo); err != nil {
		var ne *NetworkError
		if errors.As(err, &ne) {
			return done(Failed, "network_unavailable", "", "gh-stack: install failed, network unavailable (%s); check the connection and rerun setup", ne.Detail)
		}
		return done(Failed, "install_failed", "", "gh-stack: install failed: %v", err)
	}
	list, err = gh.ExtensionList(ctx)
	if err != nil {
		return done(Failed, "detect_failed", "", "gh-stack: cannot list gh extensions after install: %v", err)
	}
	v, ok := ParseVersion(list, Repo)
	if !ok {
		return done(Failed, "not_detected_after_install", "", "gh-stack: install finished but the extension is not listed")
	}
	return done(Installed, "installed", v, "gh-stack: installed (%s)", versionText(v))
}

func versionText(v string) string {
	if v == "" {
		return "version unknown"
	}
	return v
}

// ParseVersion returns the version column of repo in `gh extension list` output
// (empty when the column is absent) and whether the exact repository is listed.
func ParseVersion(list, repo string) (version string, installed bool) {
	for _, line := range strings.Split(list, "\n") {
		fields := strings.Fields(line)
		if strings.Contains(line, "\t") {
			fields = nil
			for _, f := range strings.Split(line, "\t") {
				if f = strings.TrimSpace(f); f != "" {
					fields = append(fields, f)
				}
			}
		}
		for i, f := range fields {
			if strings.EqualFold(f, repo) {
				if i+1 < len(fields) {
					return fields[i+1], true
				}
				return "", true
			}
		}
	}
	return "", false
}

// ExecGH runs the real gh command. LookPath and Run default to os/exec; tests inject both.
type ExecGH struct {
	LookPath func(string) (string, error)
	Run      func(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

// Available reports ErrGHMissing when gh is not on PATH.
func (g ExecGH) Available(context.Context) error {
	look := g.LookPath
	if look == nil {
		look = exec.LookPath
	}
	if _, err := look("gh"); err != nil {
		return ErrGHMissing
	}
	return nil
}

// ExtensionList returns the output of `gh extension list`.
func (g ExecGH) ExtensionList(ctx context.Context) (string, error) {
	out, errOut, err := g.run(ctx, "extension", "list")
	if err != nil {
		return "", commandError(err, errOut)
	}
	return string(out), nil
}

// ExtensionInstall runs `gh extension install <repo>`; network failures become *NetworkError.
func (g ExecGH) ExtensionInstall(ctx context.Context, repo string) error {
	_, errOut, err := g.run(ctx, "extension", "install", repo)
	if err == nil {
		return nil
	}
	if isNetwork(string(errOut)) {
		return &NetworkError{Detail: firstLine(string(errOut))}
	}
	return commandError(err, errOut)
}

func (g ExecGH) run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	if g.Run != nil {
		return g.Run(ctx, "gh", args...)
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	return so.Bytes(), se.Bytes(), err
}

func commandError(err error, stderr []byte) error {
	if s := firstLine(string(stderr)); s != "" {
		return fmt.Errorf("%w: %s", err, s)
	}
	return err
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func isNetwork(stderr string) bool {
	s := strings.ToLower(stderr)
	for _, m := range []string{"no such host", "could not resolve host", "i/o timeout", "network is unreachable", "connection refused", "tls handshake timeout", "temporary failure in name resolution", "dial tcp"} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}
