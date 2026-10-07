package doctor

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// loginPicture is the square Workharbor logo (256x256 PNG) that the host step
// "login-picture" gives the account; it is embedded, never downloaded.
//
//go:embed assets/login-picture.png
var loginPicture []byte

// LoginPictureDir and LoginPictureFile are where the picture is installed: a
// root-owned directory outside every home, so the account cannot replace the
// file its Picture attribute points to. Offboard removes the directory.
const (
	LoginPictureDir  = "/Library/User Pictures/Workharbor"
	LoginPictureFile = LoginPictureDir + "/logo.png"
)

// loginPictureTemp is where the bytes wait before sudo install copies them.
func loginPictureTemp() string { return filepath.Join(setupDir(), "login-picture.png") }

func (d Deps) pictureFile() string {
	if d.PictureFile != "" {
		return d.PictureFile
	}
	return LoginPictureFile
}

// dsclPicture reads the path out of `dscl . -read /Users/<name> Picture`. On
// the development Mac (macOS 27, own account) the output is "Picture:\n <path>"
// (verified); the one-line "Picture: <path>" form is accepted too (unverified).
// An absent attribute prints nothing after the key, or fails: "" then.
func dsclPicture(out string) string {
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "Picture:"))
	if strings.Contains(s, "\n") {
		return ""
	}
	return s
}

// loginPictureStep sets the account picture.
//
// VERIFIED (read-only, on the development Mac, own account): the Picture
// attribute holds a file path (/Library/User Pictures/Animals/Penguin.heic);
// JPEGPhoto is empty there; dscl has -read and -create and -delete (dscl . -help).
// UNVERIFIED (needs one real-host run as an administrator, never run here):
// that `sudo dscl . -create /Users/workharbor Picture <path>` is accepted
// without a GUI, that the login window and System Settings then show the
// image, and that nothing else (JPEGPhoto, a cached copy) must be set. The
// JPEGPhoto binary attribute was not chosen: its hex form for dscl is not
// documented here, and the Picture path is the one form seen in use.
func loginPictureStep(d Deps) Check {
	return Check{
		Name: "login-picture", Phase: PhaseHost, Step: 2, Title: "the Workharbor logo as " + d.account() + "'s login picture", Optional: true,
		Run: func(ctx context.Context) (Status, string) {
			out, err := d.output(ctx, "dscl", ".", "-read", "/Users/"+d.account(), "Picture")
			if err != nil {
				if st, msg, ok := notHere(err); ok {
					return st, msg
				}
				err = dsclFailure(out, err)
				if DSCLNotFound(err) {
					return NotVerified, "there is no user " + d.account() + " yet"
				}
				return NotVerified, "dscl did not say what the picture is: " + oneLine(err.Error())
			}
			if got := dsclPicture(out); got != d.pictureFile() {
				return Fail, "the picture of " + d.account() + " is " + orNone(got) + ", not " + d.pictureFile()
			}
			b, err := os.ReadFile(d.pictureFile())
			if errors.Is(err, fs.ErrNotExist) {
				return Fail, d.pictureFile() + " is not there"
			}
			if err != nil {
				return NotVerified, "could not read " + d.pictureFile() + ": " + oneLine(err.Error())
			}
			if !bytes.Equal(b, loginPicture) {
				return Fail, d.pictureFile() + " is not the Workharbor logo"
			}
			return OK, "the login picture is the Workharbor logo (" + d.pictureFile() + ")"
		},
		Fix: &Fix{
			Desc: "write the embedded logo to a private temporary file, install it as root's, point the account's Picture at it",
			Do:   func(context.Context, Prompter) error { return writeTemp(loginPictureTemp(), string(loginPicture)) },
			Cmds: []Cmd{
				{Sudo: true, Argv: []string{"install", "-d", "-m", "0755", "-o", "root", "-g", "wheel", filepath.Dir(d.pictureFile())}},
				{Sudo: true, Argv: []string{"install", "-m", "0644", "-o", "root", "-g", "wheel", loginPictureTemp(), d.pictureFile()}},
				{Sudo: true, Argv: []string{"dscl", ".", "-create", "/Users/" + d.account(), "Picture", d.pictureFile()}},
			},
		},
	}
}
