package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/toolstore"
	"github.com/wstein/workharbor/internal/version"
)

// runTools implements `whr tools build`: it fills the tool store (design §5.6,
// D19) with the pinned agent CLI, checked against the pin, adds the launcher built from this commit, and makes the profile.
// Stdout is data (one line per entry and the profile), stderr is human text.

// toolsClient is the HTTP client of `whr tools build`; nil uses the store's
// default. Tests set it to trust their TLS server.
var toolsClient *http.Client

func runTools(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "build" {
		fmt.Fprintln(stderr, "usage: whr tools build -store <dir> [-shim <whr-shim linux-arm64 binary>] [-platform linux-arm64|linux-arm64-musl] [-tools claude,antigravity]")
		return exitcode.Usage
	}
	fs := flag.NewFlagSet("tools build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("store", "", "the tool store directory (roots.tool_store)")
	shim := fs.String("shim", "", "the whr-shim binary for the platform, built from this commit")
	platform := fs.String("platform", "linux-arm64", "the guest platform: linux-arm64 for a glibc base, linux-arm64-musl for Alpine")
	only := fs.String("tools", "claude", "the pinned tools to build, comma separated; antigravity is opt-in")
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
	store := &toolstore.Store{Root: *dir, Client: toolsClient}
	known := map[string]bool{}
	for _, p := range pins {
		known[p.Name] = true
	}
	want := map[string]bool{}
	for _, n := range strings.Split(*only, ",") {
		n = strings.TrimSpace(n)
		switch {
		case n == "":
			fmt.Fprintln(stderr, "whr tools build: empty tool name in -tools")
			return exitcode.Usage
		case !known[n]:
			fmt.Fprintf(stderr, "whr tools build: unknown tool %q in -tools\n", n)
			return exitcode.Usage
		}
		want[n] = true // a repeated name is the same tool
	}
	var entries []toolstore.Entry
	var used []toolstore.Pin
	for _, p := range pins {
		if p.Platform != *platform || !want[p.Name] {
			continue
		}
		fmt.Fprintf(stderr, "fetching %s %s for %s and checking it against the pin\n", p.Name, p.Version, p.Platform)
		e, err := store.Download(ctx, p)
		if err != nil {
			fmt.Fprintln(stderr, "whr tools build:", err)
			return exitcode.Error
		}
		entries = append(entries, e)
		used = append(used, p)
	}
	if len(entries) == 0 {
		fmt.Fprintf(stderr, "whr tools build: no pinned tool of %s for %s\n", *only, *platform)
		return exitcode.NotFound
	}
	profile := toolstore.ProfileName(used...)
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
