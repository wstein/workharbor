package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"

	"github.com/wstein/workharbor/internal/termproto"
)

// sshHost is the name `ssh` is given for the console. It is not resolved: the
// connection goes through the ProxyCommand, and the host key is pinned under it.
const sshHost = "whr-console"

// sshCert is the API's answer to a certificate request.
type sshCert struct {
	Certificate string `json:"certificate"`
	HostKey     string `json:"host_key"`
	Principal   string `json:"principal"`
	ExpiresAt   string `json:"expires_at"`
}

// sshDir is where the client keeps its key, the short-lived certificate and the
// pinned host key: $WHR_SSH_DIR, else $XDG_STATE_HOME/whr/ssh, else
// ~/.local/state/whr/ssh. It is on the machine that runs `whr`, never the console's.
func sshDir(getenv func(string) string) (string, error) {
	if d := getenv("WHR_SSH_DIR"); d != "" {
		if !filepath.IsAbs(d) {
			return "", errors.New("WHR_SSH_DIR must be an absolute path")
		}
		return d, nil
	}
	if x := getenv("XDG_STATE_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "whr", "ssh"), nil
	}
	h := getenv("HOME")
	if h == "" || !filepath.IsAbs(h) {
		return "", errors.New("cannot find a home directory for the SSH key: set WHR_SSH_DIR")
	}
	return filepath.Join(h, ".local", "state", "whr", "ssh"), nil
}

type sshFiles struct{ dir, key, pub, cert, hosts string }

func sshFilesIn(dir string) sshFiles {
	return sshFiles{
		dir: dir, key: filepath.Join(dir, "id_ed25519"), pub: filepath.Join(dir, "id_ed25519.pub"),
		cert: filepath.Join(dir, "id_ed25519-cert.pub"), hosts: filepath.Join(dir, "known_hosts"),
	}
}

// ensureKey makes the client's key if there is none: an Ed25519 key that stays on
// this machine. Only its public half goes to the supervisor, to be signed.
func (f sshFiles) ensureKey() (pubLine string, err error) {
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return "", err
	}
	if b, err := os.ReadFile(f.pub); err == nil { //nolint:gosec // the client's own file
		if _, statErr := os.Stat(f.key); statErr == nil {
			return strings.TrimSpace(string(b)), nil
		}
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	block, err := ssh.MarshalPrivateKey(priv, "whr-console")
	if err != nil {
		return "", err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " whr-console"
	if err := writeFileAtomic(f.key, pem.EncodeToMemory(block), 0o600); err != nil {
		return "", err
	}
	if err := writeFileAtomic(f.pub, []byte(line+"\n"), 0o644); err != nil { //nolint:gosec // a public key
		return "", err
	}
	return line, nil
}

// writeFileAtomic writes a file next to its place and renames it, so a reader
// (ssh) never sees half of it.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

// refreshSSH gets a fresh certificate for the client's key and pins the console's
// host key, opening the console first if none is open. It runs before every
// connection, so the certificate a connection uses is minutes old.
func refreshSSH(ctx context.Context, s *state, c *Client, f sshFiles, forwarding bool) error {
	line, err := f.ensureKey()
	if err != nil {
		return fmt.Errorf("the SSH key: %w", err)
	}
	raw, data, err := c.Do(ctx, "GET", "/v1/console", nil, "")
	if err != nil {
		return err
	}
	_ = raw
	if string(data) == "null" {
		fmt.Fprintln(s.env.Stderr, "opening the console (the first time builds its image, which takes a while)...")
		if _, _, err := c.Do(ctx, "POST", "/v1/console", map[string][]string{"read_write": {}}, newKey()); err != nil {
			return err
		}
	}
	_, data, err = c.Do(ctx, "POST", "/v1/console/ssh/certificate", map[string]any{"public_key": line, "forwarding": forwarding}, newKey())
	if err != nil {
		return err
	}
	var got sshCert
	if err := json.Unmarshal(data, &got); err != nil {
		return fmt.Errorf("the certificate is not what this whr expects: %w", err)
	}
	if !strings.HasPrefix(got.Certificate, "ssh-") || strings.ContainsAny(got.Certificate, "\n\r") || !strings.HasPrefix(got.HostKey, "ssh-ed25519 ") || strings.ContainsAny(got.HostKey, "\n\r") {
		return errors.New("the supervisor sent a certificate or host key that is not one line of a key")
	}
	if err := writeFileAtomic(f.cert, []byte(got.Certificate+"\n"), 0o644); err != nil { //nolint:gosec // a public certificate
		return err
	}
	// The host key is pinned from the API, which the token authenticates, not
	// learned from the first connection.
	return writeFileAtomic(f.hosts, []byte(sshHost+" "+got.HostKey+"\n"), 0o600)
}

// sshOptions are the options every connection gets: only the certificate, only
// the pinned host key, nothing from the user's own ssh configuration or agent.
func sshOptions(f sshFiles, proxy string) []string {
	return []string{
		"-F", "/dev/null",
		"-o", "ProxyCommand=" + proxy,
		"-o", "IdentitiesOnly=yes",
		"-i", f.key,
		"-o", "CertificateFile=" + f.cert,
		"-o", "UserKnownHostsFile=" + f.hosts,
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "CheckHostIP=no",
		"-o", "PreferredAuthentications=publickey",
		"-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no",
		"-o", "ForwardAgent=no",
		"-o", "ServerAliveInterval=30",
		"-l", "whr",
	}
}

// shellWord quotes a word for the shell ssh runs a ProxyCommand through.
func shellWord(s string) string {
	plain := func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+:=@%", r)
	}
	if s != "" && strings.IndexFunc(s, func(r rune) bool { return !plain(r) }) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// proxyCommand is the command line ssh runs to reach the console: this binary, in
// its proxy mode, with the configuration file the user named.
func proxyCommand(s *state, forwarding bool) (string, error) {
	exe := "whr"
	if s.env.Executable != nil {
		p, err := s.env.Executable()
		if err != nil {
			return "", err
		}
		exe = p
	} else if p, err := os.Executable(); err == nil {
		exe = p
	}
	parts := []string{shellWord(exe)}
	if s.configPath != "" {
		parts = append(parts, "--config", shellWord(s.configPath))
	}
	parts = append(parts, "ssh", "--proxy")
	if forwarding {
		parts = append(parts, "--forward")
	}
	return strings.Join(parts, " "), nil
}

func newSSH(s *state) *cobra.Command {
	var proxy, config, forward bool
	cmd := &cobra.Command{
		Use:   "ssh [-- command...]",
		Short: "Open an SSH session in the console, with a certificate that lasts minutes",
		Long: "SSH (issue #32) lands in the console, never on the host. Your key stays on this machine; each connection gets " +
			"a certificate for it from the supervisor, valid for minutes and for the user whr only, and the console's host " +
			"key is pinned from the supervisor, so nothing is trusted on first sight. The console listens on no port: ssh " +
			"is carried over the API, with this command as its ProxyCommand. Port forwarding, which the editors' remote " +
			"modes need, is off unless you pass --forward. --config prints a block for ~/.ssh/config, so that VS Code and " +
			"JetBrains reach the console as the host whr-console. Anything after -- is run in the console instead of a shell.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if proxy && config {
				return usageError{"--proxy and --config stand alone"}
			}
			dir, err := sshDir(s.env.Getenv)
			if err != nil {
				return usageError{err.Error()}
			}
			f := sshFilesIn(dir)
			switch {
			case config:
				if len(args) > 0 {
					return usageError{"--config takes no command"}
				}
				return sshConfig(s, f, forward)
			case proxy:
				if len(args) > 0 {
					return usageError{"--proxy takes no command"}
				}
				return sshProxy(cmd.Context(), s, f, forward)
			}
			return sshRun(cmd.Context(), s, f, forward, args)
		},
	}
	cmd.Flags().BoolVar(&proxy, "proxy", false, "carry one SSH connection over the API (the ProxyCommand ssh runs)")
	cmd.Flags().BoolVar(&config, "config", false, "print a ~/.ssh/config block for the console")
	cmd.Flags().BoolVar(&forward, "forward", false, "allow port forwarding to the console's own loopback")
	return cmd
}

func sshConfig(s *state, f sshFiles, forward bool) error {
	pc, err := proxyCommand(s, forward)
	if err != nil {
		return err
	}
	lines := []string{
		"# The workharbor console (issue #32). Add this to ~/.ssh/config, then use the host " + sshHost,
		"# from ssh, VS Code Remote-SSH or JetBrains. Each connection runs whr, which gets a",
		"# fresh certificate for the key below, so nothing here expires or needs renewing.",
		"Host " + sshHost,
		"    HostName " + sshHost,
		"    User whr",
		"    ProxyCommand " + pc,
		"    IdentityFile " + f.key,
		"    CertificateFile " + f.cert,
		"    IdentitiesOnly yes",
		"    UserKnownHostsFile " + f.hosts,
		"    GlobalKnownHostsFile /dev/null",
		"    StrictHostKeyChecking yes",
		"    CheckHostIP no",
		"    PreferredAuthentications publickey",
		"    ForwardAgent no",
		"    ServerAliveInterval 30",
	}
	_, err = fmt.Fprintln(s.env.Stdout, strings.Join(lines, "\n"))
	return err
}

// sshProxy is the ProxyCommand: refresh the certificate, then carry the bytes of
// one SSH connection between this process's standard input and output and the
// sshd that runs for it in the console.
func sshProxy(ctx context.Context, s *state, f sshFiles, forward bool) error {
	c, err := s.api()
	if err != nil {
		return err
	}
	if err := refreshSSH(ctx, s, c, f, forward); err != nil {
		return err
	}
	conn, br, err := c.Upgrade(ctx, "/v1/console/ssh", termproto.SSHUpgrade)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	done := make(chan struct{}, 2)
	go func() { // ssh to sshd
		_, _ = io.Copy(conn, s.env.Stdin)
		done <- struct{}{}
	}()
	go func() { // sshd to ssh
		_, _ = io.Copy(s.env.Stdout, br)
		done <- struct{}{}
	}()
	<-done
	return nil
}

// sshRun refreshes the certificate and runs ssh with this binary as its
// ProxyCommand. The exit code is ssh's.
func sshRun(ctx context.Context, s *state, f sshFiles, forward bool, command []string) error {
	c, err := s.api()
	if err != nil {
		return err
	}
	if err := refreshSSH(ctx, s, c, f, forward); err != nil {
		return err
	}
	pc, err := proxyCommand(s, forward)
	if err != nil {
		return err
	}
	args := append(sshOptions(f, pc), sshHost)
	args = append(args, command...)
	run := s.env.SSH
	if run == nil {
		run = runSSH(s.env)
	}
	code, err := run(ctx, args)
	if err != nil {
		return err
	}
	if code != 0 {
		return shellExit{code}
	}
	return nil
}

// runSSH runs the ssh of this machine with the terminal of whr.
func runSSH(env *Env) func(context.Context, []string) (int, error) {
	return func(ctx context.Context, args []string) (int, error) {
		cmd := exec.CommandContext(ctx, "ssh", args...) //nolint:gosec // ssh with options this file builds
		cmd.Stdin, cmd.Stdout, cmd.Stderr = env.Stdin, env.Stdout, env.Stderr
		err := cmd.Run()
		var ee *exec.ExitError
		switch {
		case err == nil:
			return 0, nil
		case errors.As(err, &ee):
			return ee.ExitCode(), nil
		default:
			return 0, fmt.Errorf("run ssh: %w (is it installed?)", err)
		}
	}
}
