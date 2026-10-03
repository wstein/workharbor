package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/textsafe"
)

// Env is everything a command touches outside its arguments, so tests run
// commands without a terminal, a home directory or a network.
type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	// NewClient builds the API client from the configuration file path. Nil
	// uses NewClient.
	NewClient func(configPath string) (*Client, error)
	// Host is the machine `whr service` works on; zero means this one.
	Host Host
	// Setup is the machine `whr setup` works on; zero means this one.
	Setup SetupEnv
	// Extra are commands the binary adds, such as version, tools and serve.
	Extra []*cobra.Command
	// TTY is the user's terminal, for `whr console`. Nil means the terminal that
	// Stdin and Stdout are, if they are one.
	TTY func() (TTY, error)
	// SSH runs the ssh client with the arguments `whr ssh` builds and returns its
	// exit code. Nil runs the ssh of this machine.
	SSH func(ctx context.Context, args []string) (int, error)
	// Executable returns the path of this binary, which ssh runs as its
	// ProxyCommand. Nil uses os.Executable.
	Executable func() (string, error)
}

// state is what the commands share: the flags of the root and the client, made
// on first use so `whr version` and `whr help` need no configuration.
type state struct {
	env        *Env
	configPath string
	asJSON     bool
	client     *Client
}

func (s *state) api() (*Client, error) {
	if s.client != nil {
		return s.client, nil
	}
	path := s.configPath
	if path == "" {
		path = DefaultConfigPath(s.env.Getenv)
	}
	mk := s.env.NewClient
	if mk == nil {
		mk = NewClient
	}
	c, err := mk(path)
	if err != nil {
		return nil, err
	}
	s.client = c
	return c, nil
}

// Execute runs the CLI and returns the exit code (design §9.2). Cobra's own
// error and usage printing is silenced: an error is one line on stderr and its
// code comes from internal/exitcode, so a script can rely on both.
func Execute(ctx context.Context, env Env, args []string) int {
	root, ran := newRoot(&env)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return exitcode.OK
	}
	code := exitcode.From(err)
	if !*ran && code == exitcode.Error {
		code = exitcode.Usage // cobra refused the command line: an unknown command, flag or argument
	}
	if msg := oneLineError(err); msg != "" { // a command that already explained itself returns an empty one
		fmt.Fprintf(env.Stderr, "whr: %s\n", msg)
	}
	return code
}

// oneLineError keeps an error to one line for the terminal: a message may carry
// text from the server or the forge.
func oneLineError(err error) string {
	return clean(strings.Join(strings.Fields(err.Error()), " "))
}

func newRoot(env *Env) (*cobra.Command, *bool) {
	st := &state{env: env}
	ran := new(bool)
	root := &cobra.Command{
		Use:           "whr",
		Short:         "workharbor: supervise AI coding agents working on your issues",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			*ran = true
			return cmd.Help()
		},
	}
	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)
	root.SetIn(env.Stdin)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err.Error()} })
	root.PersistentFlags().StringVar(&st.configPath, "config", "", "the configuration file (default $WHR_CONFIG or ~/.config/whr/config.json)")
	root.PersistentFlags().BoolVar(&st.asJSON, "json", false, "print the API's envelope (or one JSON event per line for logs -f) instead of text")

	// A command that ran owns its errors: only cobra's own refusal of the command
	// line, which happens before any RunE, is a usage error. The mark goes on
	// every command of the tree, not only the top ones.
	var mark func(c *cobra.Command)
	mark = func(c *cobra.Command) {
		if inner := c.RunE; inner != nil {
			c.RunE = func(cmd *cobra.Command, args []string) error {
				*ran = true
				return inner(cmd, args)
			}
		}
		c.SilenceErrors, c.SilenceUsage = true, true
		for _, sub := range c.Commands() {
			mark(sub)
		}
	}
	add := func(c *cobra.Command) {
		mark(c)
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{
		newLs(st), newRun(st), newLogs(st), newSay(st), newCancel(st), newPause(st), newResume(st), newPurge(st), newInbox(st), newApprove(st, true), newApprove(st, false), newAnswer(st), newWs(st), newAgent(st), newDoctor(st), newGitHub(st), newService(st), newPasskey(st), newPreview(st), newOpen(st), newKillAll(st), newUsage(st), newShow(st), newConsole(st), newSSH(st), newSetup(st),
	} {
		add(c)
	}
	for _, c := range env.Extra {
		add(c)
	}
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		if c.Name() == "completion" {
			group(c) // an unknown shell is a usage error, not a help page
		}
	}
	return root, ran
}

// notFoundError is a reference that matches nothing. It is exit code 3.
type notFoundError struct{ msg string }

func (e notFoundError) Error() string { return e.msg }
func (notFoundError) ExitCode() int   { return exitcode.NotFound }

// clean replaces control characters, which text from agents, issues and the
// forge could use to move the cursor or rewrite the terminal, and the Unicode
// bidirectional controls and line separators that reorder or hide text (Trojan
// Source), with '?'.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == ' ' {
			return ' '
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || textsafe.IsBidiOrSeparator(r) {
			return '?'
		}
		return r
	}, s)
}

// needsHumanError is a command that did what it could and now waits for a human.
// It is exit code 6 (design §9.2).
type needsHumanError struct{ msg string }

func (e needsHumanError) Error() string { return e.msg }
func (needsHumanError) ExitCode() int   { return exitcode.NeedsHuman }
