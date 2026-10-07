package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"strings"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/textsafe"
)

// defaultListen is where `whr serve` binds when the configuration has no listen.
const defaultListen = "127.0.0.1:8787"

// publicURLStep is the step `public-url` (issue #396): whr's public name is in
// the configuration, well formed, and the listener behind the forwarder is on
// loopback. It reads only: the configuration and, on a Mac, `socketfilterfw
// --getglobalstate`, which needs no sudo. Whether the forwarder serves the name
// to the port, and whether a browser reaches it, needs the network and is never
// claimed. The firewall is named only because it is the usual suspect: the
// application firewall does not filter a loopback connection, so it cannot be
// why a request to the listener on this Mac fails.
func (d Deps) publicURLStep() Check {
	return Check{
		Name: "public-url", Phase: PhaseUser, Step: 2, Title: "whr's public name in the configuration (manual step 7)",
		Run: func(ctx context.Context) (Status, string) {
			m, err := readConfigMap(d.ConfigPath)
			if err != nil {
				return NotVerified, "needs the configuration (the config-base step): " + oneLine(err.Error())
			}
			listen, _ := m["listen"].(string)
			if listen == "" {
				listen = defaultListen
			}
			host, _, err := net.SplitHostPort(listen)
			if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
				return Fail, "listen " + textsafe.Escape(listen) + " is not a loopback address: the forwarder reaches whr on loopback (D29)"
			}
			raw, _ := m["public_url"].(string)
			if raw == "" {
				return NotVerified, "no public_url: whr github app create has no link a phone can open. Set it with this step, or open the link on this Mac with `whr github app create --local` (http://" + listen + ")"
			}
			n, err := config.NormalizePublicURL(raw)
			if err != nil || n != strings.TrimSuffix(raw, "/") {
				if err == nil {
					return Fail, "public_url " + textsafe.Escape(raw) + " is not in its normal form; this step writes " + n
				}
				return Fail, "public_url " + textsafe.Escape(raw) + ": " + oneLine(err.Error())
			}
			return OK, "public_url " + n + ", listening on loopback " + listen + "; " + d.firewallNote(ctx) + "; whether the forwarder serves the name to that port is not checked (it needs the network)"
		},
		Fix: &Fix{
			Desc: "ask for whr's public name, show it normalised, and after a y add public_url to " + d.ConfigPath + " (written atomically, other keys kept)",
			Do: func(_ context.Context, p Prompter) error {
				return d.writePublicURL(p)
			},
			Guide: "Forward whr's name to its loopback port with HTTPS (`tailscale serve`, manual step 7); the name is the one the forwarder prints, for example whr.example.ts.net. whr does not guess it: reading it would need the network.",
		},
	}
}

// firewallNote says what the application firewall is, without claiming that it
// matters: it never filters a connection to loopback.
func (d Deps) firewallNote(ctx context.Context) string {
	out, err := d.output(ctx, "/usr/libexec/ApplicationFirewall/socketfilterfw", "--getglobalstate")
	if err != nil {
		if errors.Is(err, errNotHere) {
			return "the firewall is not checked off a Mac"
		}
		return "the firewall state could not be read (" + oneLine(err.Error()) + ")"
	}
	switch firewallWord(out, false) {
	case answerNo:
		return "the application firewall is off"
	case answerUnknown:
		return "the application firewall's answer is not one this check knows (unverified on macOS 26)"
	}
	return "the application firewall is on; it does not filter loopback, so it cannot block the listener on this Mac, only other devices that reach this Mac directly"
}

func (d Deps) writePublicURL(p Prompter) error {
	m, err := readConfigMap(d.ConfigPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return errors.New("run the config-base step first: " + d.ConfigPath + " does not exist")
		}
		return err
	}
	line, err := p.Line("whr's public name behind the forwarder, for example whr.example.ts.net (https:// is added)")
	if err != nil {
		return err
	}
	n, err := config.NormalizePublicURL(line)
	if err != nil {
		return errors.New("that is not usable: " + oneLine(err.Error()) + "; nothing was written")
	}
	m["public_url"] = n
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	p.Show("public_url will be " + textsafe.Escape(n))
	if d.Yes {
		p.Show("yes: write " + textsafe.Escape(d.ConfigPath))
	} else if ok, err := p.Confirm("Write " + textsafe.Escape(d.ConfigPath)); err != nil || !ok {
		if err == nil {
			err = errors.New("not written")
		}
		return err
	}
	return replaceWithBackup(p, d.ConfigPath, append(raw, '\n'))
}
