// SPDX-License-Identifier: EUPL-1.2

package commands

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/output"
)

type cobraRoot = *cobra.Command

const (
	annotationID    = "whr.id"    // canonical id of a command
	annotationGroup = "whr.group" // canonical path of a group, space separated
)

// initBool lists the boolean flags of workspace.init; its other contract
// flags take a value. --json is the global flag.
var initBool = map[string]bool{"--non-interactive": true, "--yes": true, "--dry-run": true, "--no-agents-block": true}

// build generates the Cobra tree from the table: canonical groups and
// commands, the group alias, and short forms as hidden root commands that share
// the handler closure of their canonical command (Cobra aliases only rename
// siblings).
func (a *app) build() {
	root := &cobra.Command{
		Use:               "whr",
		Args:              cobra.ArbitraryArgs,
		SilenceErrors:     true,
		SilenceUsage:      true,
		DisableAutoGenTag: true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		Annotations:       map[string]string{annotationGroup: ""},
	}
	root.SetOut(a.env.Stdout)
	root.SetErr(a.env.Stderr)
	root.PersistentFlags().BoolVar(&a.jsonFlag, "json", false, "write one JSON envelope to standard output")
	root.PersistentFlags().StringVar(&a.colorFlag, "color", string(output.ColorAuto), "color mode: auto, always or never")
	root.PersistentFlags().BoolVar(&a.noColor, "no-color", false, "disable color (same as --color never)")
	showVersion := root.Flags().Bool("version", false, "print the version (same as the version command)")
	root.PersistentPreRunE = func(*cobra.Command, []string) error { return a.colorError() }
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		if name := c.Annotations[annotationID]; name != "" {
			err = fmt.Errorf("%s: %w", name, err)
		}
		return exitcode.Errorf(exitcode.PathBadArguments, "%v", err).
			WithRemediation("run %s --help", strings.TrimSpace(c.CommandPath()))
	})
	a.root = root
	a.builtin = map[string]Handler{"help": a.helpHandler, "completion": a.completionHandler}

	root.RunE = func(c *cobra.Command, args []string) error {
		if *showVersion {
			if len(args) > 0 {
				return exitcode.Errorf(exitcode.PathBadArguments, "--version takes no other arguments")
			}
			return a.run(c.Context(), mustEntry("version"), nil)
		}
		return a.group(c, nil, args)
	}

	groups := map[string]*cobra.Command{"": root}
	var group func(path []string) *cobra.Command
	group = func(path []string) *cobra.Command {
		key := joinPath(path)
		if g, ok := groups[key]; ok {
			return g
		}
		parent := group(path[:len(path)-1])
		name := path[len(path)-1]
		g := &cobra.Command{Use: name, Args: cobra.ArbitraryArgs, ValidArgsFunction: cobra.NoFileCompletions, Annotations: map[string]string{annotationGroup: key}}
		if len(path) == 1 {
			g.Aliases = groupAliases[name]
		}
		g.RunE = func(c *cobra.Command, args []string) error { return a.group(c, path, args) }
		parent.AddCommand(g)
		groups[key] = g
		return g
	}

	for _, e := range entries {
		e := e
		runE := func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			if e.ID == "workspace.stage" {
				ctx = context.WithValue(ctx, localStageKey{}, a.localStageOptions(c))
			}
			if e.ID == "workspace.drift" {
				local, _ := c.Flags().GetBool("local")
				source, _ := c.Flags().GetString("source")
				ctx = context.WithValue(ctx, localDriftKey{}, localDriftInvocation{local: local, source: source})
			}
			if e.ID == "workspace.shell" {
				ctx = context.WithValue(ctx, localShellKey{}, a.localShellOptions(c))
			}
			if e.ID == "workspace.console" {
				if local, _ := c.Flags().GetBool("local"); local {
					return exitcode.Errorf(exitcode.PathBadArguments, "console --local is unavailable; use whr shell --local <ws> inside the host multiplexer session")
				}
			}
			if e.ID == "workspace.rm" {
				ctx = context.WithValue(ctx, localRmKey{}, a.localRmOptions(c))
			}
			if e.ID == "workspace.ls" {
				ctx = context.WithValue(ctx, localLsKey{}, localLsOptions(c))
			}
			if e.ID == "workspace.init" {
				options, err := initOptions(c)
				interactive := !options.NonInteractive && !options.DryRun && !a.jsonMode() && a.env.InputTerminal && a.env.IsTerminal(a.env.Stderr) && a.env.Stdin != nil
				if err == nil && !interactive && !options.Yes && !options.DryRun {
					err = exitcode.Errorf(exitcode.PathHumanDecisionMissing, "unattended init requires --yes (or --dry-run); provide missing settings with init flags")
				}
				ctx = context.WithValue(ctx, initContextKey{}, initInvocation{options: options, err: err, diagnostics: a.env.Stderr, input: a.env.Stdin, interactive: interactive})
			}
			return a.run(ctx, e, args)
		}
		leaf := func(use string) *cobra.Command {
			c := &cobra.Command{Use: use, Args: cobra.ArbitraryArgs, RunE: runE, ValidArgsFunction: cobra.NoFileCompletions,
				Annotations: map[string]string{annotationID: e.ID}}
			if e.ID == "completion" {
				shells := []string{"bash", "zsh", "fish"} // not ValidArgs: Cobra would then skip ValidArgsFunction
				c.ValidArgsFunction = func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
					var out []cobra.Completion
					for _, shell := range shells {
						if len(args) == 0 && strings.HasPrefix(shell, toComplete) {
							out = append(out, shell)
						}
					}
					return out, cobra.ShellCompDirectiveNoFileComp
				}
			}
			if e.ID == "workspace.init" {
				addInitFlags(c, e)
			} else if e.ID == "workspace.stage" {
				c.Flags().Bool("local", false, "use the local Docker daemon; no control plane")
				c.Flags().String("source", "", "explicit source directory matching the selected primary")
				c.Flags().Bool("smoke", false, "run the trusted workspace smoke through stdin")
				c.Flags().Bool("dry-run", false, "preview only; no hook, build, receipt or native mutation")
				c.Flags().String("trust", "", "trust this exact previewed source fingerprint (independent of --yes)")
				c.Flags().Bool("yes", false, "confirm effects and the disclosed pause-writers condition")
			} else if e.ID == "workspace.drift" {
				c.Flags().Bool("local", false, "compare local running primary bundle references")
				c.Flags().String("source", "", "explicit local source directory matching the primary label")
			} else if e.ID == "workspace.rm" {
				c.Flags().Bool("local", false, "remove local workspace containers; no control plane")
				c.Flags().String("role", "", "local cleanup role: staging or temp")
				c.Flags().String("label", "", "local label selector: key or key=value")
				c.Flags().Bool("volumes", false, "delete selected home volumes (requires --force)")
				c.Flags().Bool("force", false, "authorize selected home volume deletion with --volumes")
				c.Flags().Bool("dry-run", false, "preview removal without mutations")
				c.Flags().Bool("yes", false, "confirm the proposed removal")
			} else if e.ID == "workspace.ls" {
				c.Flags().Bool("local", false, "discover local workspace containers; no control plane")
				c.Flags().String("role", "", "local role: primary, staging or temp")
				c.Flags().String("label", "", "local label selector: key or key=value")
			} else if e.ID == "workspace.shell" || e.ID == "workspace.console" {
				c.Flags().Bool("local", false, "local Docker login shell (console refuses; use host session)")
				if !a.available(e.ID) {
					c.FParseErrWhitelist.UnknownFlags = true
				}
			} else if !a.available(e.ID) {
				// Unavailable commands accept and ignore any flags so that they refuse
				// with their one-line explanation instead of a flag error.
				c.FParseErrWhitelist.UnknownFlags = true
			}
			c.Hidden = !a.available(e.ID)
			return c
		}
		switch len(e.Path) {
		case 1:
			if e.ID == "help" {
				c := leaf("help [command]")
				root.SetHelpCommand(c)
				continue
			}
			root.AddCommand(leaf(e.Path[0]))
		default:
			group(e.Path[:len(e.Path)-1]).AddCommand(leaf(e.Path[len(e.Path)-1]))
		}
		switch len(e.Short) {
		case 0:
		case 1:
			s := leaf(e.Short[0])
			s.Hidden = true
			root.AddCommand(s)
		default: // "service install": a hidden root group shared by the short forms
			g := groups["short:"+joinPath(e.Short[:1])]
			if g == nil {
				g = &cobra.Command{
					Use: e.Short[0], Args: cobra.ArbitraryArgs, Hidden: true, ValidArgsFunction: cobra.NoFileCompletions,
					Annotations: map[string]string{annotationGroup: joinPath(e.Path[:len(e.Path)-1])},
				}
				path := e.Path[:len(e.Path)-1]
				g.RunE = func(c *cobra.Command, args []string) error { return a.group(c, path, args) }
				root.AddCommand(g)
				groups["short:"+joinPath(e.Short[:1])] = g
			}
			g.AddCommand(leaf(e.Short[len(e.Short)-1]))
		}
	}
	hideEmptyGroups(root)
	root.ValidArgsFunction = a.rootCompletion
	root.SetHelpFunc(func(c *cobra.Command, _ []string) { a.flagHelp(c) })
}

func mustEntry(id string) Entry {
	for _, e := range entries {
		if e.ID == id {
			return e
		}
	}
	panic("unknown table id " + id)
}

func addInitFlags(c *cobra.Command, e Entry) {
	for _, f := range e.Flags {
		name := strings.TrimPrefix(f, "--")
		switch {
		case name == "json": // global
		case name == "overwrite":
			c.Flags().StringArray(name, nil, initFlagHelp(name))
		case initBool[f]:
			c.Flags().Bool(name, false, initFlagHelp(name))
		default:
			c.Flags().String(name, "", initFlagHelp(name))
		}
	}
}

func initFlagHelp(name string) string {
	return map[string]string{
		"name": "workspace name", "project-url": "Git URL of the project to pin", "start-dir": "directory a session starts in",
		"devcontainer": "devcontainer file to use", "template": "starter template", "human-shell": "interactive shell: fish, bash, zsh or nu",
		"console-mode": "console mode: herdr or plain", "shortcuts": "shell shortcuts: on or off",
		"non-interactive": "never prompt", "yes": "confirm the proposed writes", "dry-run": "show the proposal without writing",
		"overwrite": "replace this existing path (repeatable)", "no-agents-block": "do not write the agent workflow block into AGENTS.md",
	}[name]
}

// hideEmptyGroups hides every group none of whose commands is available, so
// help and completion do not offer it; the group still resolves when typed.
func hideEmptyGroups(c *cobra.Command) bool {
	if !c.HasSubCommands() {
		return !c.Hidden
	}
	visible := false
	for _, sub := range c.Commands() {
		if hideEmptyGroups(sub) {
			visible = true
		}
	}
	if !visible && c.Parent() != nil {
		c.Hidden = true
	}
	return visible && !c.Hidden
}

// group handles a bare group (help) or an unknown word below it.
func (a *app) group(c *cobra.Command, path []string, args []string) error {
	if len(args) > 0 {
		return unknown(path, args[0])
	}
	a.flagHelp(c)
	return nil
}

// colorError validates the color mode shared by commands and help.
func (a *app) colorError() error {
	if _, err := output.ParseColorMode(a.colorFlag); err != nil {
		return exitcode.Errorf(exitcode.PathBadArguments, "%v", err)
	}
	return nil
}

// flagHelp prints the help of c to standard output (it is data), or refuses
// --json with a structured usage error.
func (a *app) flagHelp(c *cobra.Command) {
	w := a.writer()
	if err := a.colorError(); err != nil {
		a.exit = w.Failure("help", asError(err))
		return
	}
	if a.jsonMode() {
		a.exit = w.Failure("help", exitcode.Errorf(exitcode.PathUnsupportedJSON, "help does not support --json"))
		return
	}
	fmt.Fprint(a.env.Stdout, a.renderHelp(c))
}

func (a *app) helpHandler(_ context.Context, call Call) (Result, error) {
	target, rest, err := a.root.Find(call.Args)
	if err != nil {
		return Result{}, exitcode.Errorf(exitcode.PathUnknownCommand, "%v", err)
	}
	if len(rest) > 0 {
		return Result{}, unknown(groupPath(target), rest[0])
	}
	return Result{Human: a.renderHelp(target)}, nil
}

func groupPath(c *cobra.Command) []string {
	if g := c.Annotations[annotationGroup]; g != "" {
		return strings.Fields(g)
	}
	return nil
}

func (a *app) completionHandler(_ context.Context, call Call) (Result, error) {
	if len(call.Args) != 1 {
		return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "completion needs exactly one shell: bash, zsh or fish").
			WithRemediation("run whr completion bash|zsh|fish")
	}
	var buf bytes.Buffer
	var err error
	switch call.Args[0] {
	case "bash":
		err = a.root.GenBashCompletionV2(&buf, true)
	case "zsh":
		err = a.root.GenZshCompletion(&buf)
	case "fish":
		err = a.root.GenFishCompletion(&buf, true)
	default:
		return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "unsupported shell %q: use bash, zsh or fish", call.Args[0])
	}
	if err != nil {
		return Result{}, err
	}
	return Result{Human: buf.String()}, nil
}

// rootCompletion offers the spellings Cobra does not list by itself: the group
// alias and the short forms of available commands. Hidden commands are excluded
// from Cobra's own completion, so both spellings are generated from the table.
func (a *app) rootCompletion(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []cobra.Completion
	seen := map[string]bool{}
	offer := func(name, desc string) {
		if seen[name] || !strings.HasPrefix(name, toComplete) {
			return
		}
		seen[name] = true
		out = append(out, cobra.CompletionWithDesc(name, desc))
	}
	for _, e := range entries {
		if !a.available(e.ID) {
			continue
		}
		for _, alias := range groupAliases[e.Path[0]] {
			offer(alias, "alias of "+e.Path[0])
		}
		if e.Short != nil {
			offer(e.Short[0], Describe(e.ID))
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
