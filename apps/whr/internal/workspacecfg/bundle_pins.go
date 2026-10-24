// SPDX-License-Identifier: EUPL-1.2
package workspacecfg

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// BundlePin describes a declared reference, never the contents of a volume.
type BundlePin struct{ Bundle, Reference, Volume, Target string }

var bundleName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
var bundleRef = regexp.MustCompile(`^quay\.io/bundledock/([a-z][a-z0-9-]{0,31})(:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?@sha256:([0-9a-f]{64})$`)

// ReadBundlePins projects only the initial Anchorage pin format. It reuses the
// bounded no-follow reader and strict JSONC scanner; no hook is evaluated.
// Other pin formats, ambiguous declarations and unsafe files are undetermined.
func ReadBundlePins(ctx context.Context, directory string) ([]BundlePin, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d, err := openDir(directory)
	if err != nil {
		return nil, errors.New("bundle pins unavailable or unsupported")
	}
	defer d.Close()
	return readBundlePins(ctx, d)
}

// ReadBundlePinsForSource compares the identities of opened source and label
// roots, then reads only through the same held source descriptor. A path may
// change after observation without redirecting reads to another directory.
func ReadBundlePinsForSource(ctx context.Context, directory, expectedDirectory string) ([]BundlePin, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d, err := openDir(directory)
	if err != nil {
		return nil, ErrBundleSourceMismatch
	}
	defer d.Close()
	return readBoundBundlePins(ctx, d, expectedDirectory)
}

func readBundlePins(ctx context.Context, d *dir) ([]BundlePin, error) {
	fail := func() ([]BundlePin, error) { return nil, errors.New("bundle pins unavailable or unsupported") }
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Do not choose devcontainer pins over a second, undefined pin authority.
	if _, aerr := d.readFile(".devcontainer/bundle-pins.json", 64<<10); aerr == nil || aerr.kind != accMissing {
		return fail()
	}
	raw, aerr := d.readFile(".devcontainer/devcontainer.json", 64<<10)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if aerr != nil {
		return fail()
	}
	if !utf8.Valid(raw) || bytes.HasPrefix(raw, []byte("\xef\xbb\xbf")) || bytes.IndexByte(raw, 0) >= 0 {
		return fail()
	}
	doc, serr := parseStrict(raw, true)
	if serr != nil || doc.kind != jObject {
		return fail()
	}
	init, ok := doc.get("initializeCommand")
	if !ok || init.kind != jObject {
		return fail()
	}
	mounts, ok := doc.get("mounts")
	if !ok || mounts.kind != jArray {
		return fail()
	}
	pins := []BundlePin{}
	for _, member := range init.obj {
		a := member.val
		if a.kind != jArray {
			return fail()
		}
		for _, v := range a.arr {
			if v.kind != jString {
				return fail()
			}
		}
		if member.key == "host" && len(a.arr) == 2 && a.arr[0].str == "sh" && a.arr[1].str == "${localWorkspaceFolder}/.devcontainer/host-info.sh" {
			continue
		}
		if !bundleName.MatchString(member.key) || len(a.arr) != 5 || a.arr[0].str != "sh" || a.arr[1].str != "${localWorkspaceFolder}/.devcontainer/initialize.sh" || a.arr[4].str != member.key {
			return fail()
		}
		ref := bundleRef.FindStringSubmatch(a.arr[2].str)
		if ref == nil || ref[1] != member.key || a.arr[3].str != "bundledock-"+member.key+"-"+ref[3] {
			return fail()
		}
		pins = append(pins, BundlePin{member.key, a.arr[2].str, a.arr[3].str, "/opt/bdock/" + member.key})
	}
	if len(pins) == 0 {
		return fail()
	}
	// The declared mount must agree with the pin, and every bundle mount must
	// have exactly one pin. String mount options are intentionally a narrow set.
	seen := map[string]bool{}
	for _, m := range mounts.arr {
		if m.kind != jString {
			return fail()
		}
		opts := strings.Split(m.str, ",")
		values := map[string]string{}
		for _, opt := range opts {
			k, v, assigned := strings.Cut(opt, "=")
			if assigned && (k == "readonly" || k == "volume-nocopy") {
				return fail()
			}
			if _, dup := values[k]; dup {
				return fail()
			}
			values[k] = v
		}
		// Refuse aliases and unknown keys before deciding whether a mount is a
		// bundle declaration. Otherwise src/dst can hide an undeployed bundle.
		for k, v := range values {
			if k != "type" && k != "source" && k != "target" && k != "readonly" && !(k == "volume-nocopy" && v == "") {
				return fail()
			}
		}
		target := values["target"]
		relevant := strings.HasPrefix(target, "/opt/bdock") || strings.HasPrefix(values["source"], "bundledock-")
		if !relevant {
			continue
		}
		matched := false
		for _, p := range pins {
			if p.Target != target {
				continue
			}
			if seen[target] || values["type"] != "volume" || values["source"] != p.Volume {
				return fail()
			}
			if v, ok := values["readonly"]; !ok || v != "" {
				return fail()
			}
			matched = true
			seen[target] = true
		}
		if !matched {
			return fail()
		}
	}
	if len(seen) != len(pins) {
		return fail()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return pins, nil
}

// readBoundBundlePins keeps both identity handles alive through the pin reads.
// The label root supplies identity only; configuration is read from d.
func readBoundBundlePins(ctx context.Context, d *dir, expectedDirectory string) ([]BundlePin, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if expectedDirectory == "" || !filepath.IsAbs(expectedDirectory) {
		return nil, ErrBundleSourceMismatch
	}
	resolved, err := filepath.EvalSymlinks(expectedDirectory)
	if err != nil {
		return nil, ErrBundleSourceMismatch
	}
	expected, err := openDir(resolved)
	if err != nil {
		return nil, ErrBundleSourceMismatch
	}
	defer expected.Close()
	actualInfo, err := d.root.stat()
	if err != nil {
		return nil, ErrBundleSourceMismatch
	}
	expectedInfo, err := expected.root.stat()
	if err != nil || !sameNativeFile(actualInfo, expectedInfo) {
		return nil, ErrBundleSourceMismatch
	}
	return readBundlePins(ctx, d)
}

// ErrBundleSourceMismatch reports unavailable or different physical source roots.
var ErrBundleSourceMismatch = errors.New("bundle source does not match selected workspace")
