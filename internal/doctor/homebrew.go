package doctor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/wstein/workharbor/internal/textsafe"
)

// HomebrewInstallURL is where Homebrew publishes its installer. It is trusted
// by TLS alone; the SHA-256 whr prints is informational, not a pin (option C of
// the bootstrap note, #505).
const HomebrewInstallURL = "https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh"

const homebrewScriptMax = 4 << 20 // far above the script's real size

// downloadHTTPS fetches a small file over HTTPS; the default Deps.Download.
func downloadHTTPS(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // a fixed https URL
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, homebrewScriptMax+1))
	if err != nil {
		return nil, err
	}
	if len(b) > homebrewScriptMax {
		return nil, errors.New("the script is larger than expected; not using it")
	}
	return b, nil
}

// homebrewFix downloads install.sh to a private file, shows its path, URL and
// SHA-256, and runs it as the administrator only after an explicit answer. The
// confirmation is asked through the Prompter, which --yes does not answer and an
// unattended run refuses, so there is no way around it. Nothing here refuses a
// single-account install: install.sh itself refuses root, and that is enough.
func (d Deps) homebrewFix() *Fix {
	var dir string // the private directory, removed by Cleanup
	run := func(script string) []Cmd {
		return []Cmd{{Argv: []string{"/usr/bin/env", "NONINTERACTIVE=1", "/bin/bash", script}}}
	}
	return &Fix{
		Desc:      "download Homebrew's install.sh to a private file, show its SHA-256, and run it only after you confirm",
		Cmds:      run("<the downloaded install.sh>"),
		NeedsSudo: true, // known before Build downloads anything (#507)
		Build: func(ctx context.Context, p Prompter) ([]Cmd, error) {
			dl := d.Download
			if dl == nil {
				dl = downloadHTTPS
			}
			body, err := dl(ctx, HomebrewInstallURL)
			if err != nil {
				return nil, fmt.Errorf("could not download %s: %s; nothing was run", HomebrewInstallURL, oneLine(err.Error()))
			}
			if len(body) == 0 {
				return nil, errors.New("the download of " + HomebrewInstallURL + " was empty; nothing was run")
			}
			if dir, err = os.MkdirTemp("", "whr-homebrew-"); err != nil { // mode 0700, this user's
				return nil, err
			}
			script := filepath.Join(dir, "install.sh")
			if err := writeTemp(script, string(body)); err != nil {
				return nil, err
			}
			sum := sha256.Sum256(body)
			p.Show(fmt.Sprintf("downloaded %d bytes from %s\nfile:    %s\nsha256:  %s  (informational: it is not checked against anything)\nRead the script before you answer, for example with: less %s",
				len(body), HomebrewInstallURL, textsafe.Escape(script), hex.EncodeToString(sum[:]), textsafe.Escape(script)))
			ok, err := p.Confirm("Run this script now, as you, with sudo for its own steps?")
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, errors.New("not confirmed; nothing was run")
			}
			return append([]Cmd{{Sudo: true, Argv: []string{"-v"}}}, run(script)...), nil
		},
		Cleanup: func() {
			if dir != "" {
				_ = os.RemoveAll(dir)
				dir = ""
			}
		},
		Guide: "Homebrew's installer is a script from the internet, trusted by TLS only. This step does not install the Command Line Tools on purpose: run xcode-select --install yourself (a dialog opens on the Mac's screen) for make.",
		Try:   []string{"xcode-select --install"},
	}
}
