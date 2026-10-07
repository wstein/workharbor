package cli

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/wstein/workharbor/internal/render"
	"github.com/wstein/workharbor/internal/runlog"
	"github.com/wstein/workharbor/internal/setup"
)

// startRunLog opens the log of this run (issue #379) at path, or at the fixed
// per-run path under the state directory when path is empty, and gives it to the
// real terminal host. --verbose also streams every record to stderr. Close the
// result with finish, which prints the log path.
func (st *state) startRunLog(env *SetupEnv, command, path string, verbose bool) (*runlog.Log, error) {
	explicit := path != ""
	if !explicit && env.UID == 0 {
		// root: a default log would create a root-owned state directory in a
		// kept HOME; give --log-file to log a root run
		fmt.Fprintln(st.env.Stderr, "note: no run log as root; give --log-file to write one")
		return nil, nil
	}
	if !explicit && (env.NoRunLog || !filepath.IsAbs(st.env.Getenv("HOME"))) {
		// no home to put it under: a relative HOME would write into the current directory
		return nil, nil
	}
	if !explicit {
		path = runlog.Path("", st.env.Getenv("HOME"), command, time.Now())
	}
	lg, err := runlog.Open(path)
	if err != nil && !explicit {
		// the default place is not writable: the run goes on, without a log
		fmt.Fprintf(st.env.Stderr, "note: no run log: %s\n", clean(err.Error()))
		return nil, nil
	}
	if err != nil {
		return nil, usageError{"cannot open the run log " + clean(path) + ": " + clean(err.Error())}
	}
	if verbose {
		lg.Stream = st.env.Stderr
	}
	if t, ok := env.Host.(setup.Terminal); ok {
		t.Log = lg
		env.Host = t
	}
	return lg, nil
}

// finishRunLog closes the log and says where it is.
func (st *state) finishRunLog(lg *runlog.Log, style render.Style) {
	if lg == nil {
		return
	}
	_ = lg.Close()
	render.Writer{W: st.env.Stderr, S: style}.KV("log", lg.Path())
}

// logOpts are the --log-file and --verbose flags of a command.
type logOpts struct {
	file    string
	verbose bool
}
