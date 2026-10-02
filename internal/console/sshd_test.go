package console

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/baseimage"
)

// directives reads the sshd configuration into keyword -> values, lowercased
// keywords as sshd compares them; the first occurrence wins in sshd, so a second
// one is a mistake this test reports.
func directives(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(string(SSHDConfig())))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, _ := strings.Cut(line, " ")
		k = strings.ToLower(k)
		if _, dup := out[k]; dup && k != "subsystem" {
			t.Errorf("%s is set twice: the first one wins in sshd", k)
		}
		out[k] = strings.TrimSpace(v)
	}
	return out
}

func TestTheConsolesSSHDTrustsTheAuthorityAndNothingElse(t *testing.T) {
	d := directives(t)
	want := map[string]string{
		"authenticationmethods":        "publickey",
		"passwordauthentication":       "no",
		"kbdinteractiveauthentication": "no",
		"gssapiauthentication":         "no",
		"hostbasedauthentication":      "no",
		"permitemptypasswords":         "no",
		"permitrootlogin":              "no",
		"authorizedkeysfile":           "none",
		"trustedusercakeys":            "/tmp/whr-ssh-ca.pub",
		"authorizedprincipalsfile":     "/tmp/whr-ssh-principals",
		"allowusers":                   "whr",
		"strictmodes":                  "no",
		"allowagentforwarding":         "no",
		"x11forwarding":                "no",
		"permittunnel":                 "no",
		"permitopen":                   "127.0.0.1:* [::1]:* localhost:*",
		"allowstreamlocalforwarding":   "no",
		"gatewayports":                 "no",
		"permituserenvironment":        "no",
		"permituserrc":                 "no",
		"hostkey":                      "/home/whr/.whr-sshd/host_ed25519",
		"pidfile":                      "none",
		"maxauthtries":                 "3",
		"logingracetime":               "20",
		"subsystem":                    "sftp internal-sftp",
	}
	for k, v := range want {
		if d[k] != v {
			t.Errorf("%s = %q, want %q", k, d[k], v)
		}
	}
	// Nothing that adds a way in: a listen address wider than the console's own
	// interface is the runtime's to decide, not the configuration's.
	for _, banned := range []string{"authorizedkeyscommand", "authorizedkeyscommandUser", "match", "include", "forcecommand", "pubkeyacceptedalgorithms"} {
		if v, ok := d[strings.ToLower(banned)]; ok {
			t.Errorf("%s = %q is not part of the console's sshd configuration", banned, v)
		}
	}
}

func TestTheLauncherIsPOSIXShAndRefusesAnythingButAnAuthorityKey(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "whr-sshd")
	if err := os.WriteFile(script, SSHD(), 0o700); err != nil { //nolint:gosec // a script the test runs
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "sh", "-n", script).CombinedOutput(); err != nil { //nolint:gosec // the test's own script
		t.Fatalf("sh -n: %v\n%s", err, out)
	}
	for name, ca := range map[string]string{
		"none":           "",
		"a password":     "hunter2",
		"an RSA key":     "ssh-rsa AAAAB3NzaC1yc2E whatever",
		"two lines":      "ssh-ed25519 AAAA one\nssh-ed25519 AAAA two",
		"a private line": "-----BEGIN OPENSSH PRIVATE KEY-----",
	} {
		cmd := exec.CommandContext(ctx, "sh", script) //nolint:gosec // the test's own script
		cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "WHR_SSH_CA=" + ca}
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "WHR_SSH_CA") {
			t.Errorf("%s: err=%v out=%q", name, err, out)
		}
	}
	// Nothing was written before the refusal.
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("the launcher wrote files before it checked the key: %v", entries)
	}
}

func TestBothConsoleImagesInstallTheSSHServerAndTheFiles(t *testing.T) {
	for _, d := range []baseimage.Distro{baseimage.Fedora, baseimage.Ubuntu} {
		cf, err := Containerfile(d)
		if err != nil {
			t.Fatal(err)
		}
		text := string(cf)
		for _, want := range []string{"openssh-server", "COPY whr-sshd /usr/local/bin/whr-sshd", "COPY sshd_config /etc/whr/sshd_config", "/tmp/whr-proxy-vars"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the Containerfile lacks %q", d, want)
			}
		}
		if !strings.Contains(text, "openssh-client") {
			t.Errorf("%s: the host key is made with ssh-keygen: the client package is needed", d)
		}
	}
}

// Files sshd reads are replaced by a rename, never rewritten in place, and the host
// key's public half is derived from its private half, never linked in beside it.
func TestTheLauncherWritesAtomicallyAndDerivesThePublicHostKey(t *testing.T) {
	script := string(SSHD())
	for _, bad := range []string{">/tmp/whr-ssh-ca.pub", ">/tmp/whr-ssh-principals", ">/tmp/whr-proxy-vars", `ln "$tmp.pub"`, ": >"} {
		if strings.Contains(script, bad) {
			t.Errorf("the launcher still has %q", bad)
		}
	}
	for _, want := range []string{"| put /tmp/whr-ssh-ca.pub", "| put /tmp/whr-ssh-principals", "} | put /tmp/whr-proxy-vars", "ssh-keygen -y -f", "mv -f"} {
		if !strings.Contains(script, want) {
			t.Errorf("the launcher lacks %q", want)
		}
	}
	if n := strings.Count(script, `ln "$tmp"`); n != 1 {
		t.Errorf("the host key is linked in %d places, want one", n)
	}
}
