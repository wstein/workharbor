// Package launchd installs `whr serve` as a macOS LaunchAgent (design §5.3,
// issue #38). Apple Container registers its services in the logged-in user's
// GUI launchd domain (gui/<uid>), so the job must be a LaunchAgent of that user
// in its Aqua session, never a LaunchDaemon: a daemon, or a shell from SSH or
// sudo, cannot reach them.
package launchd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode"
)

// Label is the job's launchd label. Provisional, with the command.
const Label = "io.github.wstein.workharbor"

// Spec is everything the job runs with. All paths are absolute.
type Spec struct {
	Label     string
	Whr       string // the installed whr binary
	Config    string // the configuration file
	Container string // the container CLI
	Home      string // the user's home directory: logs and the working directory
}

// LogDir is where the job's output goes.
func (s Spec) LogDir() string { return filepath.Join(s.Home, "Library", "Logs", "whr") }

// PlistPath is where the job's plist lives.
func (s Spec) PlistPath() string {
	return filepath.Join(s.Home, "Library", "LaunchAgents", s.Label+".plist")
}

// Validate refuses a spec that cannot be rendered safely: every path must be
// absolute and clean of control characters. Where the binary lies does not
// matter in the alpha (#493).
func (s Spec) Validate() error {
	for name, p := range map[string]string{"whr": s.Whr, "config": s.Config, "container": s.Container, "home": s.Home} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return fmt.Errorf("%s path %q must be absolute and clean", name, p)
		}
		if strings.ContainsFunc(p, unicode.IsControl) {
			return fmt.Errorf("%s path has a control character", name)
		}
	}
	if s.Label == "" || strings.ContainsFunc(s.Label, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '-' }) {
		return fmt.Errorf("label %q: letters, digits, '.' and '-' only", s.Label)
	}
	return nil
}

// CheckBinary refuses a whr that is not an executable regular file after
// symbolic links. Where the file lies and who owns it do not matter in the
// alpha (#493); the doctor warns about it.
func CheckBinary(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("find %s: %w", path, err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s is not an executable file", resolved)
	}
	return nil
}

// script is what the job runs: start the container system (idempotent; the
// kernel is installed beforehand, host setup step 6) and then the supervisor.
// The paths are arguments, never text of the script, so a path cannot change
// what runs.
const script = `"$1" system start --disable-kernel-install || exit 1
exec "$2" serve --config "$3"`

// xmlText escapes what XML text must not contain. Control characters are
// refused before this (Spec.Validate), and quotes and newlines are fine in text.
var xmlText = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

var plistTemplate = template.Must(template.New("plist").Funcs(template.FuncMap{"x": xmlText.Replace}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{x .Label}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>/bin/sh</string>
		<string>-c</string>
		<string>{{x .Script}}</string>
		<string>whr-launch</string>
		<string>{{x .Container}}</string>
		<string>{{x .Whr}}</string>
		<string>{{x .Config}}</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>{{x .Path}}</string>
	</dict>
	<key>WorkingDirectory</key>
	<string>{{x .Home}}</string>
	<key>LimitLoadToSessionType</key>
	<string>Aqua</string>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ThrottleInterval</key>
	<integer>30</integer>
	<key>ExitTimeOut</key>
	<integer>60</integer>
	<key>StandardOutPath</key>
	<string>{{x .Log}}/whr.out.log</string>
	<key>StandardErrorPath</key>
	<string>{{x .Log}}/whr.err.log</string>
</dict>
</plist>
`))

// Plist renders the job. It carries no secret: the configuration file holds
// only the paths of secrets, and no environment variable but PATH is set.
func Plist(s Spec) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	err := plistTemplate.Execute(&b, map[string]string{
		"Label": s.Label, "Script": script, "Container": s.Container, "Whr": s.Whr, "Config": s.Config, "Home": s.Home,
		"Log":  s.LogDir(),
		"Path": filepath.Dir(s.Container) + ":/usr/bin:/bin:/usr/sbin:/sbin",
	})
	// Indented with tabs in the source, written with two spaces, as the files of
	// this repository are.
	return bytes.ReplaceAll(b.Bytes(), []byte("\t"), []byte("  ")), err
}

// Runner runs a command and returns its combined output.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs real commands.
type ExecRunner struct{}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // launchctl with arguments this package builds
	return out, err
}

// Manager installs, removes and inspects the job in one user's GUI domain.
type Manager struct {
	R   Runner
	UID int
	// GOOS is runtime.GOOS; a test sets it.
	GOOS string
	// RetryAfter is the pause between the attempts to load a job again after
	// unloading it. Default 300 ms.
	RetryAfter time.Duration
}

func (m Manager) domain() string { return "gui/" + strconv.Itoa(m.UID) }

func (m Manager) retryAfter() time.Duration {
	if m.RetryAfter > 0 {
		return m.RetryAfter
	}
	return 300 * time.Millisecond
}

// Errors of the manager.
var (
	ErrNotMac     = errors.New("launchd is macOS only")
	ErrNoGUILogIn = errors.New("this shell is not in a graphical login session")
	ErrRoot       = errors.New("run this as the user whose session it is, not as root: the job belongs to that user's login")
)

// CheckSession refuses a shell outside the user's Aqua session: SSH, sudo and
// a daemon are in other domains, where Apple Container's services are not.
func (m Manager) CheckSession(ctx context.Context) error {
	if m.GOOS != "darwin" {
		return ErrNotMac
	}
	if m.UID == 0 { // a sudo shell can still report Aqua, and gui/0 does not exist
		return ErrRoot
	}
	out, err := m.R.Run(ctx, "launchctl", "managername")
	if name := strings.TrimSpace(string(out)); err != nil || name != "Aqua" {
		return fmt.Errorf("%w (launchctl managername says %q): log in as this user on the Mac, or over Screen Sharing, and run it from a Terminal there", ErrNoGUILogIn, name)
	}
	return nil
}

func (m Manager) loaded(ctx context.Context, label string) bool {
	_, err := m.R.Run(ctx, "launchctl", "print", m.domain()+"/"+label)
	return err == nil
}

// Install writes the plist (0644, owned by the user) and loads it. It replaces
// a job that is already loaded, so running it again is safe.
func (m Manager) Install(ctx context.Context, s Spec) error {
	if err := m.CheckSession(ctx); err != nil {
		return err
	}
	data, err := Plist(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.LogDir(), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.LogDir(), 0o700); err != nil { //nolint:gosec // a directory needs the execute bit; MkdirAll leaves an existing one as it is
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.PlistPath()), 0o755); err != nil { //nolint:gosec // ~/Library/LaunchAgents is world-readable by convention
		return err
	}
	wasLoaded := m.loaded(ctx, s.Label)
	if wasLoaded {
		if out, err := m.R.Run(ctx, "launchctl", "bootout", m.domain()+"/"+s.Label); err != nil {
			return fmt.Errorf("unload the running job: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.PlistPath()), ".whr-plist-")
	if err != nil {
		return err
	}
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := os.WriteFile(tmp.Name(), data, 0o644); err != nil { //nolint:gosec // launchd refuses a plist that others can write, and needs it readable
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil { //nolint:gosec // see above
		return err
	}
	if err := os.Rename(tmp.Name(), s.PlistPath()); err != nil {
		return err
	}
	return m.bootstrap(ctx, s, wasLoaded)
}

// bootstrap loads the job. Right after a bootout launchd may still be tearing the
// old one down and refuse the new one, so a reinstall tries a few times.
func (m Manager) bootstrap(ctx context.Context, s Spec, retry bool) error {
	attempts := 1
	if retry {
		attempts = 5
	}
	var out []byte
	var err error
	for i := range attempts {
		if out, err = m.R.Run(ctx, "launchctl", "bootstrap", m.domain(), s.PlistPath()); err == nil {
			return nil
		}
		if i < attempts-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(m.retryAfter()):
			}
		}
	}
	return fmt.Errorf("load the job: %w: %s", err, strings.TrimSpace(string(out)))
}

// Uninstall unloads the job and removes its plist. A job that is not loaded, or a
// plist that is not there, is not an error. The logs stay.
func (m Manager) Uninstall(ctx context.Context, s Spec) error {
	if err := m.CheckSession(ctx); err != nil {
		return err
	}
	if m.loaded(ctx, s.Label) {
		if out, err := m.R.Run(ctx, "launchctl", "bootout", m.domain()+"/"+s.Label); err != nil {
			return fmt.Errorf("unload the job: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	if err := os.Remove(s.PlistPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Status is what launchd says about the job.
type Status struct {
	Installed bool   `json:"installed"` // the plist exists
	Loaded    bool   `json:"loaded"`    // launchd knows the job
	State     string `json:"state,omitempty"`
	PID       int    `json:"pid,omitempty"`
	LastExit  string `json:"last_exit,omitempty"`
}

// Status asks launchd about the job.
func (m Manager) Status(ctx context.Context, s Spec) (Status, error) {
	if err := m.CheckSession(ctx); err != nil {
		return Status{}, err
	}
	var st Status
	if _, err := os.Stat(s.PlistPath()); err == nil {
		st.Installed = true
	}
	out, err := m.R.Run(ctx, "launchctl", "print", m.domain()+"/"+s.Label)
	if err != nil {
		return st, nil // not loaded
	}
	st.Loaded = true
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), " = ")
		if !ok {
			continue
		}
		switch k {
		case "state":
			st.State = v
		case "pid":
			st.PID, _ = strconv.Atoi(v)
		case "last exit code", "last exit status":
			st.LastExit = v
		}
	}
	return st, nil
}
