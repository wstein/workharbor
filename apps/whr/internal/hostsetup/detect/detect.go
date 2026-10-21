// SPDX-License-Identifier: EUPL-1.2

// Package detect probes host catalogue items through injected read-only ports.
// It supplies no real process, filesystem or network adapter.
package detect

import (
	"context"
	"errors"
	"io/fs"
	"math/big"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
)

const FixedPATH = "/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/local/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
const probeBudget = 5 * time.Second

// File is just the read-only metadata detection requires. Kind is regular,
// directory, symlink or special. No file contents or configuration are read.
type File struct {
	Kind       string
	Executable bool
}
type FS interface {
	Lstat(string) (File, error)
	Readlink(string) (string, error)
}

// Probe binds an executable, separate arguments, complete environment and timeout.
// A real Exec adapter must honor context/timeout and bound stdout to 64KiB.
// Detection never supplies stdin, a shell command string, or inherited env.
type Probe struct {
	Executable string
	Args       []string
	Env        map[string]string
	Timeout    time.Duration
}
type Output struct {
	Stdout string
	Exit   int
}
type Exec interface {
	Run(context.Context, Probe) (Output, error)
}
type Clock interface{ Now() time.Time }

const OrbStackInfoPlist = "/Applications/OrbStack.app/Contents/Info.plist"

// Metadata exposes only the approved fixed application-version observation.
// Callers cannot select a file, key or command through this port.
type Metadata interface {
	OrbStackAppVersion(context.Context, time.Duration) (string, error)
}

// Result retains measured internal evidence; command-facing redaction is T008.
type Result struct {
	Item    string
	State   string
	Path    string
	Version string
	Managed bool
	Reason  string
}
type Detector struct {
	Metadata Metadata
	FS       FS
	Exec     Exec
	Clock    Clock
}

func (d Detector) Detect(ctx context.Context, item catalogue.Item) Result {
	result := Result{Item: item.ID, State: "unknown"}
	if d.FS == nil || d.Clock == nil || ctx.Err() != nil {
		result.Reason = "probe_unavailable"
		return result
	}
	if item.Tool.VersionMetadata != "" {
		return d.orbStackMetadata(ctx, item)
	}
	if d.Exec == nil {
		result.Reason = "probe_unavailable"
		return result
	}
	// Even a future catalogue entry cannot make detection invoke Homebrew.
	if item.Tool.Command == "brew" || !regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`).MatchString(item.Tool.Command) {
		result.Reason = "probe_forbidden"
		return result
	}
	start := d.Clock.Now()
	path, err := d.find(ctx, item.Tool.Command, start)
	if _, boundErr := d.remaining(ctx, start); boundErr != nil {
		result.Reason = "probe_budget_exhausted"
		return result
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			result.State = "absent"
			result.Reason = "not_found"
		} else {
			result.Reason = "path_unverifiable"
		}
		return result
	}
	result.Path = path
	remaining, boundErr := d.remaining(ctx, start)
	if boundErr != nil {
		result.Reason = "probe_budget_exhausted"
		return result
	}
	pattern, err := regexp.Compile(item.Tool.VersionRegex)
	if err != nil {
		result.Reason = "invalid_version_pattern"
		return result
	}
	remaining, boundErr = d.remaining(ctx, start)
	if boundErr != nil {
		result.Reason = "probe_budget_exhausted"
		return result
	}
	output, err := d.Exec.Run(ctx, Probe{Executable: path, Args: append([]string(nil), item.Tool.VersionArgs...), Env: map[string]string{"PATH": FixedPATH, "LC_ALL": "C"}, Timeout: remaining})
	result.State = "present_unverifiable"
	_, boundErr = d.remaining(ctx, start)
	if err != nil || output.Exit != 0 || boundErr != nil || len(output.Stdout) > 64*1024 {
		result.Reason = "probe_failed"
		return result
	}
	match := pattern.FindStringSubmatch(output.Stdout)
	if len(match) == 0 {
		result.Reason = "version_unparsed"
		return result
	}
	version := match[0]
	if len(match) > 1 {
		version = match[1]
	}
	compared, ok := compareVersions(version, item.Tool.MinVersion)
	if !ok {
		result.Reason = "version_unparsed"
		return result
	}
	result.Version = version
	managed, err := d.managed(ctx, path, item.Tool.Source, start)
	if err != nil {
		result.Reason = "source_unverifiable"
		return result
	}
	if _, boundErr = d.remaining(ctx, start); boundErr != nil {
		result.Reason = "probe_budget_exhausted"
		return result
	}
	result.Managed = managed
	if !managed {
		result.State = "present_other_source"
		return result
	}
	result.State = "present_ok"
	if compared < 0 {
		result.State = "present_outdated"
	}
	return result
}

func (d Detector) find(ctx context.Context, command string, start time.Time) (string, error) {
	for _, dir := range strings.Split(FixedPATH, ":") {
		path := filepath.Join(dir, command)
		// Never execute /usr/bin/git. If an installed real Git exists, use its
		// absolute path instead of asking the CLT shim to discover/install it.
		if path == "/usr/bin/git" {
			resolved, err := d.realGit(ctx, start)
			if err == nil {
				return resolved, nil
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return "", err
			}
			continue
		}
		resolved, err := d.resolve(ctx, path, start)
		if err == nil && filepath.Base(resolved) == "brew" {
			return "", fs.ErrInvalid
		}
		if err == nil && resolved == "/usr/bin/git" {
			if command != "git" {
				return "", fs.ErrInvalid
			}
			resolved, err = d.realGit(ctx, start)
		}
		if err == nil {
			return resolved, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	return "", fs.ErrNotExist
}
func (d Detector) realGit(ctx context.Context, start time.Time) (string, error) {
	for _, path := range []string{"/Library/Developer/CommandLineTools/usr/bin/git", "/Applications/Xcode.app/Contents/Developer/usr/bin/git"} {
		resolved, err := d.resolve(ctx, path, start)
		if err == nil && resolved != "/usr/bin/git" && filepath.Base(resolved) != "brew" {
			return resolved, nil
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	return "", fs.ErrNotExist
}

// remaining checks both deadline and cancellation after terminal observations as
// well as before dispatch. A late missing entry is not evidence of absence.
func (d Detector) remaining(ctx context.Context, start time.Time) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	elapsed := d.Clock.Now().Sub(start)
	if elapsed < 0 || elapsed >= probeBudget {
		return 0, context.DeadlineExceeded
	}
	return probeBudget - elapsed, nil
}
func (d Detector) stat(ctx context.Context, path string, start time.Time) (File, error) {
	if _, err := d.remaining(ctx, start); err != nil {
		return File{}, err
	}
	info, err := d.FS.Lstat(path)
	if _, bound := d.remaining(ctx, start); bound != nil {
		return File{}, bound
	}
	return info, err
}
func (d Detector) readlink(ctx context.Context, path string, start time.Time) (string, error) {
	if _, err := d.remaining(ctx, start); err != nil {
		return "", err
	}
	target, err := d.FS.Readlink(path)
	if _, bound := d.remaining(ctx, start); bound != nil {
		return "", bound
	}
	return target, err
}

// resolve walks every path component. Lstat on the whole path alone would follow
// ancestor directory aliases invisibly and could dispatch the CLT shim. Relative
// targets and .. are processed against the resolved parent, not cleaned across
// unresolved symlinks. Both repeated link states and the40-link bound stop cycles.
func (d Detector) resolve(ctx context.Context, path string, start time.Time) (string, error) {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return "", fs.ErrInvalid
	}
	pending := strings.Split(path, "/")
	current := "/"
	links := 0
	seen := map[string]bool{}
	for len(pending) > 0 {
		if _, err := d.remaining(ctx, start); err != nil {
			return "", err
		}
		part := pending[0]
		pending = pending[1:]
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			current = filepath.Dir(current)
			continue
		}
		candidate := filepath.Join(current, part)
		info, err := d.stat(ctx, candidate, start)
		if err != nil {
			return "", err
		}
		if info.Kind == "symlink" {
			key := candidate + "\x00" + strings.Join(pending, "\x00")
			if seen[key] || links >= 40 {
				return "", fs.ErrInvalid
			}
			seen[key] = true
			links++
			target, err := d.readlink(ctx, candidate, start)
			if err != nil {
				return "", err
			}
			if target == "" || strings.ContainsRune(target, 0) {
				return "", fs.ErrInvalid
			}
			if filepath.IsAbs(target) {
				current = "/"
			}
			pending = append(strings.Split(target, "/"), pending...)
			continue
		}
		if len(pending) > 0 {
			if info.Kind != "directory" {
				return "", fs.ErrInvalid
			}
			current = candidate
			continue
		}
		if info.Kind == "regular" && info.Executable {
			return candidate, nil
		}
		if info.Kind == "special" {
			return "", fs.ErrInvalid
		}
		return "", fs.ErrNotExist
	}
	return "", fs.ErrNotExist
}
func (d Detector) managed(ctx context.Context, path string, source catalogue.Source, start time.Time) (bool, error) {
	if source.Manager != "homebrew" {
		return false, nil
	}
	for _, prefix := range []string{"/opt/homebrew", "/usr/local"} {
		if source.Type == "formula" && strings.HasPrefix(path, filepath.Join(prefix, "Cellar", source.Name)+string(filepath.Separator)) {
			return true, nil
		}
		if source.Type == "cask" {
			info, err := d.stat(ctx, filepath.Join(prefix, "Caskroom", source.Name), start)
			if err == nil && info.Kind == "directory" {
				return true, nil
			}
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return false, err
			}
		}
	}
	return false, nil
}
func compareVersions(a, b string) (int, bool) {
	parse := func(value string) ([]big.Int, bool) {
		parts := strings.Split(value, ".")
		if len(parts) < 1 || len(parts) > 4 {
			return nil, false
		}
		out := make([]big.Int, 4)
		for i, part := range parts {
			if part == "" || strings.Trim(part, "0123456789") != "" {
				return nil, false
			}
			if _, ok := out[i].SetString(part, 10); !ok {
				return nil, false
			}
		}
		return out, true
	}
	left, ok := parse(a)
	if !ok {
		return 0, false
	}
	right, ok := parse(b)
	if !ok {
		return 0, false
	}
	for i := range left {
		if c := left[i].Cmp(&right[i]); c != 0 {
			return c, true
		}
	}
	return 0, true
}

// fixedFile checks each component without resolving aliases. The closed metadata
// route has no owner-selected file/key or executable-discovery fallback.
func (d Detector) fixedFile(ctx context.Context, p, leafKind string, start time.Time) error {
	current := "/"
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := d.stat(ctx, current, start)
		if err != nil {
			return err
		}
		kind := "directory"
		if i == len(parts)-1 {
			kind = leafKind
		}
		if info.Kind != kind {
			return fs.ErrInvalid
		}
	}
	return nil
}
func (d Detector) orbStackMetadata(ctx context.Context, item catalogue.Item) Result {
	result := Result{Item: item.ID, State: "unknown"}
	tool := item.Tool
	source := tool.Source
	if item.ID != "orbstack" || item.Kind != "runtime" || tool.VersionMetadata != catalogue.OrbStackAppMetadata || tool.Command != "" || len(tool.VersionArgs) != 0 || source.Manager != "homebrew" || source.Type != "cask" || source.Name != "orbstack" || source.Tap != "homebrew/cask" {
		result.Reason = "probe_forbidden"
		return result
	}
	if d.Metadata == nil {
		result.Reason = "probe_unavailable"
		return result
	}
	start := d.Clock.Now()
	if err := d.fixedFile(ctx, OrbStackInfoPlist, "regular", start); err != nil {
		result.Reason = "path_unverifiable"
		if errors.Is(err, fs.ErrNotExist) {
			result.State = "absent"
			result.Reason = "not_found"
		}
		return result
	}
	result.Path = OrbStackInfoPlist
	result.State = "present_unverifiable"
	remaining, err := d.remaining(ctx, start)
	if err != nil {
		result.Reason = "probe_budget_exhausted"
		return result
	}
	readCtx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	value, err := d.Metadata.OrbStackAppVersion(readCtx, remaining)
	_, boundErr := d.remaining(ctx, start)
	if err != nil || boundErr != nil || readCtx.Err() != nil || len(value) > 64*1024 {
		result.Reason = "probe_failed"
		return result
	}
	pattern, err := regexp.Compile(tool.VersionRegex)
	if err != nil {
		result.Reason = "invalid_version_pattern"
		return result
	}
	match := pattern.FindStringSubmatch(value)
	if len(match) == 0 {
		result.Reason = "version_unparsed"
		return result
	}
	version := match[0]
	if len(match) > 1 {
		version = match[1]
	}
	compared, ok := compareVersions(version, tool.MinVersion)
	if !ok {
		result.Reason = "version_unparsed"
		return result
	}
	result.Version = version
	// Application support version and cask registration are distinct evidence.
	// A newer app version never stands in for the separate source-layout guard.
	for _, prefix := range []string{"/opt/homebrew", "/usr/local"} {
		err := d.fixedFile(ctx, prefix+"/Caskroom/orbstack", "directory", start)
		if err == nil {
			result.Managed = true
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			result.Reason = "source_unverifiable"
			return result
		}
	}
	if _, err := d.remaining(ctx, start); err != nil {
		result.Reason = "probe_budget_exhausted"
		return result
	}
	if !result.Managed {
		result.State = "present_other_source"
		return result
	}
	result.State = "present_ok"
	if compared < 0 {
		result.State = "present_outdated"
	}
	return result
}
