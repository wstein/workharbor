package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os/signal"
	"syscall"

	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/toolstore"
	"github.com/wstein/workharbor/internal/version"
)

// runTools implements `whr tools build`: it fills the tool store (design §5.6,
// D19) with the pinned agent CLI, checked against the pin and the vendor's
// manifest, adds the launcher built from this commit, and makes the profile.
// Stdout is data (one line per entry and the profile), stderr is human text.
func runTools(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "build" {
		fmt.Fprintln(stderr, "usage: whr tools build -store <dir> [-shim <whr-shim linux-arm64 binary>] [-platform linux-arm64]")
		return exitcode.Usage
	}
	fs := flag.NewFlagSet("tools build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("store", "", "the tool store directory (roots.tool_store)")
	shim := fs.String("shim", "", "the whr-shim binary for the platform, built from this commit")
	platform := fs.String("platform", "linux-arm64", "the guest platform")
	pinsFile := fs.String("pins", "", "a pins file instead of the built-in pins")
	if err := fs.Parse(args[1:]); err != nil {
		return exitcode.Usage
	}
	if *dir == "" || fs.NArg() != 0 {
		fmt.Fprintln(stderr, "whr tools build: -store is required")
		return exitcode.Usage
	}
	pins, err := toolstore.Pins()
	if *pinsFile != "" {
		pins, err = toolstore.LoadPins(*pinsFile)
	}
	if err != nil {
		fmt.Fprintln(stderr, "whr tools build:", err)
		return exitcode.Error
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store := &toolstore.Store{Root: *dir}
	var entries []toolstore.Entry
	profile := ""
	for _, p := range pins {
		if p.Platform != *platform {
			continue
		}
		fmt.Fprintf(stderr, "fetching %s %s for %s and checking it against the pin and the vendor manifest\n", p.Name, p.Version, p.Platform)
		e, err := store.Download(ctx, p)
		if err != nil {
			fmt.Fprintln(stderr, "whr tools build:", err)
			return exitcode.Error
		}
		entries = append(entries, e)
		profile = p.Name + "-" + p.Version
	}
	if len(entries) == 0 {
		fmt.Fprintf(stderr, "whr tools build: no pinned tool for %s\n", *platform)
		return exitcode.NotFound
	}
	if *shim != "" {
		e, err := store.AddFile("whr-shim", version.Get().Version, *platform, *shim)
		if err != nil {
			fmt.Fprintln(stderr, "whr tools build:", err)
			return exitcode.Error
		}
		entries = append(entries, e)
	}
	if err := store.Profile(profile, entries...); err != nil {
		fmt.Fprintln(stderr, "whr tools build:", err)
		return exitcode.Error
	}
	for _, e := range entries {
		fmt.Fprintf(stdout, "store %s sha256:%s\n", e.Dir, e.SHA256)
	}
	fmt.Fprintf(stdout, "profile %s/profiles/%s\n", *dir, profile)
	return exitcode.OK
}
