package cli

import (
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/offboard"
	"github.com/wstein/workharbor/internal/setup"
	"github.com/wstein/workharbor/internal/setup/answers"
	"github.com/wstein/workharbor/internal/setup/protocol"
)

// exitError is a command that already explained itself and ends with a code.
type exitError struct{ code int }

func (exitError) Error() string   { return "" }
func (e exitError) ExitCode() int { return e.code }

// OffboardEnv is what `whr offboard` needs besides SetupEnv, so a test never
// reads a directory or an owner from this machine. Zero values mean this one.
type OffboardEnv struct {
	Stat    offboard.StatFunc
	ReadDir func(string) ([]string, error)
	Now     func() time.Time
	// Prefix is where an installed whr lives; empty means the default prefix.
	Prefix string
}

const offboardLong = `Remove the workharbor macOS account that "whr setup host" created (provisional).

The default is a dry run: it inspects read-only and prints exactly what would be
removed. --delete removes the account and its home folder with
"sudo sysadminctl -deleteUser workharbor -adminUser <you> -adminPassword -", after you type the word workharbor at
the terminal. It never takes an answer from --answers or --unattended. --allow-admin
allows removing an administrator account. whr asks for your password itself, without echo, and hands it to sysadminctl on its input; it is never in the command line.

Every macOS command here is unverified. The manual has the commands by hand, for
volumes, backups and other accounts, in the section
"Remove the workharbor account" of docs/content/docs/manual/host-setup.md.`

// newOffboard is `whr offboard host` (provisional, issue #346).
func newOffboard(st *state) *cobra.Command {
	var (
		del, allowAdmin, unattended, plain bool
		answers                            string
	)
	run := func(cmd *cobra.Command, _ []string) error {
		style := st.style(st.env.Stderr, plain)
		o := offboard.Out{Out: st.env.Stdout, Err: st.env.Stderr, Style: style}
		env, err := st.env.Setup.resolve(st, style)
		if err != nil {
			return err
		}
		in := offboard.Invocation{
			RunUser: env.User, RunUID: env.UID, SudoUser: st.env.Getenv("SUDO_USER"),
			Delete: del, AllowAdmin: allowAdmin, Answers: cmd.Flags().Changed("answers"), Unattended: unattended,
			Terminal: env.IsTerminal(),
		}
		refused := offboard.InvocationGuards(in)
		if st.asJSON {
			refused = append(refused, offboard.Refusal{Guard: "json", Code: exitcode.Usage, Msg: "--json is not supported yet"})
		}
		if env.GOOS != "darwin" {
			refused = append(refused, offboard.Refusal{Guard: "os", Code: exitcode.Usage, Msg: "this runs only on a Mac"})
		}
		return offboardRun(cmd, st, env, in, refused, o)
	}
	parent := &cobra.Command{
		Use:   "offboard",
		Short: "remove what whr setup created (provisional)",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	host := &cobra.Command{
		Use:   "host",
		Short: "remove the workharbor macOS account, as the administrator (provisional, unverified)",
		Long:  offboardLong,
		Args:  cobra.NoArgs,
		RunE:  run,
	}
	f := host.Flags()
	f.BoolVar(&del, "delete", false, "really remove the account and its home folder, after you type the word workharbor (default: a dry run)")
	f.BoolVar(&allowAdmin, "allow-admin", false, "allow removing an account that is an administrator")
	f.StringVar(&answers, "answers", "", "refused: this command asks every answer at the terminal")
	f.BoolVar(&unattended, "unattended", false, "refused: this command asks every answer at the terminal")
	f.BoolVar(&plain, "plain", false, "no colour and no symbols beyond ASCII, as when the output is not a terminal")
	parent.AddCommand(host)
	return parent
}

func offboardRun(cmd *cobra.Command, st *state, env SetupEnv, in offboard.Invocation, refused []offboard.Refusal, o offboard.Out) error {
	stop := func(rs []offboard.Refusal) error {
		for _, r := range rs {
			o.Refusal(r)
		}
		o.Note("nothing was changed")
		return exitError{rs[0].Code}
	}
	if len(refused) > 0 {
		return stop(refused)
	}
	ctx := cmd.Context()
	exe, exeErr := env.Executable()
	if exeErr == nil {
		exeErr = setup.CheckInstalled(exe, installedPrefixes(prefixOf(st.env.Offboard, st.dev, st.env.Getenv("HOME")))...)
	}
	if exeErr != nil {
		if in.Delete {
			return stop([]offboard.Refusal{{Guard: "installed", Code: exitcode.Usage, Msg: oneLineError(exeErr)}})
		}
		o.Note("note (dry run): %s", oneLineError(exeErr))
	}
	oe := st.env.Offboard
	if oe.Stat == nil {
		oe.Stat = offboard.OSStat
	}
	if oe.ReadDir == nil {
		oe.ReadDir = func(p string) ([]string, error) {
			es, err := os.ReadDir(filepath.Clean(p))
			var out []string
			for _, e := range es {
				out = append(out, e.Name())
			}
			return out, err
		}
	}
	if oe.Now == nil {
		oe.Now = time.Now
	}
	d := offboard.Deps{Runner: env.Host, Stat: oe.Stat, ReadDir: oe.ReadDir}
	f := offboard.Inspect(ctx, d, in)
	if rs := offboard.Guards(f); len(rs) > 0 {
		return stop(rs)
	}
	offboard.Plan(o, f)
	if !in.Delete {
		o.Note("dry run: nothing was changed; add --delete to remove the account")
		o.Note("%s", offboard.Unverified)
		return nil
	}
	// Keep the record in the administrator's account, which survives removal.
	// Open only after the typed confirmation and final account recheck.
	var log *protocol.Log
	defer func() {
		if log != nil {
			_ = log.Close()
		}
	}()
	lg := offboard.Log{Record: func(result string, code int, ran [][]string) error {
		appendEntry := func(e protocol.Entry) error {
			e.Account, e.Cmd, e.Phase = env.User, protocol.CmdOffboard, protocol.PhaseHost
			return log.Append(e)
		}
		if result == "started" {
			open := env.OpenLog
			if open == nil {
				open = func(home string) (*protocol.Log, error) { return protocol.Open(home, oe.Now, nil) }
			}
			var err error
			log, err = open(st.env.Getenv("HOME"))
			if err != nil {
				return err
			}
			for _, w := range log.Warnings() {
				o.Note("note: %s", clean(w))
			}
			if err := appendEntry(protocol.Entry{Event: protocol.EventRunStart, Source: protocol.SourceInteractive}); err != nil {
				return err
			}
			fix := answers.FixDigest(doctor.Check{
				Name: "delete-user", Phase: doctor.PhaseHost,
				Fix: &doctor.Fix{Cmds: []doctor.Cmd{offboard.DeleteCmd(env.User)}, Irreversible: true},
			})
			return appendEntry(protocol.Entry{
				Event: protocol.EventStepBefore, Step: "delete-user", Fix: fix,
				Answer: protocol.AnswerRun, Source: protocol.SourceInteractive, Status: string(doctor.Fail),
			})
		}
		outcome, end, status := protocol.OutFixed, protocol.RunDone, doctor.OK
		if code != exitcode.OK {
			outcome, end, status = protocol.OutNotFixed, protocol.RunError, doctor.Fail
		}
		if err := appendEntry(protocol.Entry{
			Event: protocol.EventStepAfter, Step: "delete-user", Outcome: outcome,
			Status: string(status), Exit: &code, Ran: protocol.RanDigest(ran),
		}); err != nil {
			return err
		}
		return appendEntry(protocol.Entry{Event: protocol.EventRunEnd, Outcome: end})
	}}
	if code := offboard.Execute(ctx, env.Host, d, f, lg, o); code != exitcode.OK {
		return exitError{code}
	}
	return nil
}

func prefixOf(e OffboardEnv, dev bool, home string) string {
	if e.Prefix != "" {
		return e.Prefix
	}
	if dev && filepath.IsAbs(home) {
		return filepath.Join(home, ".local")
	}
	return doctor.DefaultPrefix
}
