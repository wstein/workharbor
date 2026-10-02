package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/termproto"
)

// consoleInfo is the API's description of an open console.
type consoleInfo struct {
	EnvID     string   `json:"env_id"`
	ReadWrite []string `json:"read_write"`
	Reused    bool     `json:"reused"`
}

// shellExit is the exit code of the console shell, as the exit code of whr.
type shellExit struct{ code int }

func (e shellExit) Error() string { return "the console shell exited with " + strconv.Itoa(e.code) }
func (e shellExit) ExitCode() int {
	if e.code <= 0 || e.code > 255 {
		return exitcode.Error
	}
	return e.code
}

func newConsole(s *state) *cobra.Command {
	var write, closeIt, status bool
	cmd := &cobra.Command{
		Use:   "console [workspace]",
		Short: "Open a shell in the console, an environment for your own work across workspaces",
		Long: "The console (design D43) is an environment without an agent: a VM with the usual tools, every workspace " +
			"mounted read-only (so a shell here cannot change what an agent works on), behind the egress allowlist, " +
			"with none of your credentials or the agents'. The shell starts in the workspace, if one is named, or in " +
			"the workspaces' root. With --write the named workspace is mounted read-write, which needs the console " +
			"closed first if it was opened with other writable workspaces. git in the console ignores the hooks, " +
			"filters and aliases a repository sets, because agents write them. The first console builds its image, " +
			"which takes a while. The shell comes through this command, over the API socket and token; nothing logs you " +
			"in to the host. --status says whether a console is open, --close closes it (its home is kept).",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := s.api()
			if err != nil {
				return err
			}
			ws := ""
			if len(args) == 1 {
				ws = args[0]
			}
			switch {
			case status && closeIt, (status || closeIt) && (write || ws != ""):
				return usageError{"--status and --close stand alone"}
			case write && ws == "":
				return usageError{"--write needs the workspace to make writable"}
			case status:
				return consoleStatus(cmd.Context(), s, c)
			case closeIt:
				raw, _, err := c.Do(cmd.Context(), "DELETE", "/v1/console", nil, newKey())
				if err != nil {
					return err
				}
				return s.emit(raw, func(io.Writer) error {
					fmt.Fprintln(s.env.Stderr, "the console is closed; its home is kept")
					return nil
				})
			}
			return runConsole(cmd.Context(), s, c, ws, write)
		},
	}
	cmd.Flags().BoolVar(&write, "write", false, "mount the named workspace read-write (the console must be closed or opened with the same)")
	cmd.Flags().BoolVar(&closeIt, "close", false, "close the console; its home is kept")
	cmd.Flags().BoolVar(&status, "status", false, "say whether a console is open")
	return cmd
}

func consoleStatus(ctx context.Context, s *state, c *Client) error {
	raw, data, err := c.Do(ctx, "GET", "/v1/console", nil, "")
	if err != nil {
		return err
	}
	return s.emit(raw, func(w io.Writer) error {
		if string(data) == "null" {
			fmt.Fprintln(s.env.Stderr, "no console is open")
			return nil
		}
		var info consoleInfo
		if err := json.Unmarshal(data, &info); err != nil {
			return fmt.Errorf("the console description is not what this whr expects: %w", err)
		}
		return table(w, []string{"ENVIRONMENT", "WRITABLE"}, [][]string{{info.EnvID, strings.Join(info.ReadWrite, ",")}})
	})
}

// runConsole opens the console and a shell in it, and carries the terminal until
// the shell ends.
func runConsole(ctx context.Context, s *state, c *Client, workspace string, write bool) error {
	open := s.env.TTY
	if open == nil {
		open = func() (TTY, error) { return osTerminal(s.env.Stdin, s.env.Stdout) }
	}
	tty, err := open()
	if err != nil {
		return usageError{err.Error()}
	}
	rw := []string{}
	if write {
		rw = append(rw, workspace)
	}
	fmt.Fprintln(s.env.Stderr, "opening the console (the first time builds its image, which takes a while)...")
	_, data, err := c.Do(ctx, "POST", "/v1/console", map[string][]string{"read_write": rw}, newKey())
	if err != nil {
		return err
	}
	var info consoleInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return fmt.Errorf("the console description is not what this whr expects: %w", err)
	}
	mode := "read-only"
	if len(info.ReadWrite) > 0 {
		mode = "read-write: " + strings.Join(info.ReadWrite, ", ")
	}
	fmt.Fprintf(s.env.Stderr, "console open (workspaces %s); type exit to leave it\n", mode)

	cols, rows, _ := tty.Size()
	q := url.Values{}
	q.Set("cols", strconv.Itoa(int(cols)))
	q.Set("rows", strconv.Itoa(int(rows)))
	if t := s.env.Getenv("TERM"); t != "" {
		q.Set("term", t)
	}
	if workspace != "" {
		q.Set("workspace", workspace)
	}
	conn, br, err := c.Upgrade(ctx, "/v1/console/shell?"+q.Encode(), termproto.Upgrade)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	restore, err := tty.MakeRaw()
	if err != nil {
		return fmt.Errorf("put the terminal in raw mode: %w", err)
	}
	defer restore()

	var wmu sync.Mutex
	send := func(f termproto.Frame) error {
		wmu.Lock()
		defer wmu.Unlock()
		return termproto.Write(conn, f)
	}
	// The user's keys go to the shell. When they end (the terminal closed), the
	// connection is closed, and the shell goes with it.
	go func() {
		buf := make([]byte, 4096)
		in := tty.Reader()
		for {
			n, err := in.Read(buf)
			if n > 0 {
				if send(termproto.Frame{Type: termproto.TypeData, Data: append([]byte(nil), buf[:n]...)}) != nil {
					return
				}
			}
			if err != nil {
				_ = conn.Close()
				return
			}
		}
	}()
	resizes, stop := tty.Resizes()
	defer stop()
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		for {
			select {
			case <-resizes:
				if cols, rows, err := tty.Size(); err == nil {
					_ = send(termproto.ResizeFrame(cols, rows))
				}
			case <-stopped:
				return
			}
		}
	}()

	out := tty.Writer()
	for {
		f, err := termproto.Read(br)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || ctx.Err() != nil {
				return errors.New("the console closed the connection")
			}
			return fmt.Errorf("the console stream: %w", err)
		}
		switch f.Type {
		case termproto.TypeData:
			if _, err := out.Write(f.Data); err != nil {
				return err
			}
		case termproto.TypeExit:
			code, err := termproto.ParseExit(f)
			if err != nil {
				return err
			}
			if code == 0 {
				return nil
			}
			return shellExit{code}
		}
	}
}
