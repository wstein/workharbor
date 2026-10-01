package runtime

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realDir returns a temporary directory with symbolic links resolved (on macOS
// the temp directory sits below the /var link).
func realDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func mkdirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o750); err != nil {
			t.Fatal(err)
		}
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symbolic links here: %v", err)
	}
}

func reasonOf(t *testing.T, err error) Reason {
	t.Helper()
	var me *MountError
	if !errors.As(err, &me) {
		t.Fatalf("error %v is not a *MountError", err)
	}
	return me.Reason
}

func TestCheckMountOnTheRealFilesystem(t *testing.T) {
	home := realDir(t)
	mkdirs(t, filepath.Join(home, ".ssh"), filepath.Join(home, ".config", "gh"), filepath.Join(home, "proj", "sub"))
	if err := os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}

	work := realDir(t)
	symlink(t, home, filepath.Join(work, "to-home"))
	symlink(t, filepath.Join(home, ".ssh"), filepath.Join(work, "to-ssh"))
	symlink(t, filepath.Join(home, "proj"), filepath.Join(work, "to-proj"))
	symlink(t, filepath.Join(work, "to-home"), filepath.Join(work, "chain"))

	tests := []struct {
		name   string
		source string
		want   Reason
	}{
		{"project", filepath.Join(home, "proj"), ""},
		{"project subdirectory", filepath.Join(home, "proj", "sub"), ""},
		{"link to a project", filepath.Join(work, "to-proj"), ""},
		{"home", home, ReasonHome},
		{"link to home", filepath.Join(work, "to-home"), ReasonHome},
		{"link to a link to home", filepath.Join(work, "chain"), ReasonHome},
		{"parent of home", filepath.Dir(home), ReasonHomeParent},
		{"ssh", filepath.Join(home, ".ssh"), ReasonSecrets},
		{"ssh key", filepath.Join(home, ".ssh", "id_ed25519"), ReasonSecrets},
		{"link to ssh", filepath.Join(work, "to-ssh"), ReasonSecrets},
		{"ssh through a link to home", filepath.Join(work, "to-home", ".ssh"), ReasonSecrets},
		{"ssh through ..", filepath.Join(home, "proj", "..", ".ssh"), ReasonSecrets},
		{"directory containing a secret", filepath.Join(home, ".config"), ReasonSecrets},
		{"missing", filepath.Join(home, "missing"), ReasonUnresolvable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckMount(OSFS{}, home, tc.source)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("CheckMount(%q) = %v, want it allowed", tc.source, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("CheckMount(%q) allowed, want %q", tc.source, tc.want)
			}
			if got := reasonOf(t, err); got != tc.want {
				t.Errorf("CheckMount(%q) reason = %q, want %q (%v)", tc.source, got, tc.want, err)
			}
		})
	}
}

func TestCheckMountResolvesAHomeThatIsALink(t *testing.T) {
	target := realDir(t)
	mkdirs(t, filepath.Join(target, ".ssh"), filepath.Join(target, "proj"))
	link := filepath.Join(realDir(t), "home")
	symlink(t, target, link)

	if err := CheckMount(OSFS{}, link, filepath.Join(target, ".ssh")); err == nil {
		t.Error("~/.ssh must be rejected when HOME is a symbolic link to its directory")
	}
	if err := CheckMount(OSFS{}, link, filepath.Join(target, "proj")); err != nil {
		t.Errorf("a project below a linked HOME was rejected: %v", err)
	}
}

func TestCheckMountRejectsAUnixSocket(t *testing.T) {
	// A short path, because a unix socket path is limited to about 100 bytes.
	dir, err := os.MkdirTemp("", "wh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "s")
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "unix", sock)
	if err != nil {
		t.Skipf("cannot listen on a unix socket here: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	home := realDir(t)
	err = CheckMount(OSFS{}, home, sock)
	if err == nil {
		t.Fatal("a unix socket must be rejected")
	}
	if got := reasonOf(t, err); got != ReasonSocket {
		t.Errorf("reason = %q, want %q", got, ReasonSocket)
	}
	if err := CheckMount(OSFS{}, home, dir); err != nil {
		t.Errorf("the directory holding a socket was rejected: %v", err)
	}
}

func TestCheckMountRejectsTheRealHomeAndRoot(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if err := CheckMount(OSFS{}, home, home); err == nil {
		t.Error("the real home directory must be rejected")
	}
	if err := CheckMount(OSFS{}, home, string(filepath.Separator)); err == nil {
		t.Error("the filesystem root must be rejected")
	}
}

// #50: with ~/.ssh and ~/.config/gh linked into a dotfiles directory, mounting
// that directory used to pass.
func TestCheckMountRejectsADotfilesDirectoryHoldingSecrets(t *testing.T) {
	home := realDir(t)
	dotfiles := filepath.Join(home, "dotfiles")
	mkdirs(t, filepath.Join(dotfiles, "ssh"), filepath.Join(dotfiles, "gh"), filepath.Join(dotfiles, "vim"), filepath.Join(home, ".config"), filepath.Join(home, "proj"))
	symlink(t, filepath.Join(dotfiles, "ssh"), filepath.Join(home, ".ssh"))
	symlink(t, filepath.Join(dotfiles, "gh"), filepath.Join(home, ".config", "gh"))

	for _, source := range []string{
		dotfiles,
		filepath.Join(dotfiles, "ssh"),
		filepath.Join(dotfiles, "gh"),
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".config"),
	} {
		err := CheckMount(OSFS{}, home, source)
		if err == nil {
			t.Errorf("CheckMount(%q) allowed", source)
			continue
		}
		if got := reasonOf(t, err); got != ReasonSecrets {
			t.Errorf("CheckMount(%q) reason = %q, want %q", source, got, ReasonSecrets)
		}
	}
	for _, source := range []string{filepath.Join(dotfiles, "vim"), filepath.Join(home, "proj")} {
		if err := CheckMount(OSFS{}, home, source); err != nil {
			t.Errorf("CheckMount(%q) = %v, want it allowed", source, err)
		}
	}
}

// #50 on a real APFS volume: a home with a decomposed (NFD) name, reached
// through its precomposed (NFC) spelling, and a case variant. On a filesystem
// that treats the spellings as different files these cases do not apply.
func TestCheckMountComparesByFileIdentityOnTheRealFilesystem(t *testing.T) {
	base := realDir(t)
	homeNFD := filepath.Join(base, "ju\u0308rgen")
	mkdirs(t, filepath.Join(homeNFD, ".ssh"), filepath.Join(homeNFD, "proj"))

	homeNFC := filepath.Join(base, "j\u00fcrgen")
	if _, err := os.Stat(homeNFC); err != nil {
		t.Skip("this filesystem treats precomposed and decomposed names as different")
	}
	for source, want := range map[string]Reason{
		homeNFC:                        ReasonHome,
		filepath.Join(homeNFC, ".ssh"): ReasonSecrets,
		filepath.Join(homeNFD, ".ssh"): ReasonSecrets,
		filepath.Join(homeNFC, "proj"): "",
		filepath.Join(homeNFD, "proj"): "",
	} {
		err := CheckMount(OSFS{}, homeNFD, source)
		if want == "" {
			if err != nil {
				t.Errorf("CheckMount(%q) = %v, want it allowed", source, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("CheckMount(%q) allowed, want %q", source, want)
		} else if got := reasonOf(t, err); got != want {
			t.Errorf("CheckMount(%q) reason = %q, want %q", source, got, want)
		}
	}
}

func TestCheckMountComparesCaseVariantsOnTheRealFilesystem(t *testing.T) {
	home := realDir(t)
	mkdirs(t, filepath.Join(home, ".ssh"))
	upper := filepath.Join(filepath.Dir(home), strings.ToUpper(filepath.Base(home)))
	if _, err := os.Stat(upper); err != nil {
		t.Skip("this filesystem is case-sensitive")
	}
	for source, want := range map[string]Reason{
		upper:                        ReasonHome,
		filepath.Join(upper, ".SSH"): ReasonSecrets,
		filepath.Join(home, ".SSH"):  ReasonSecrets,
	} {
		err := CheckMount(OSFS{}, home, source)
		if err == nil {
			t.Errorf("CheckMount(%q) allowed, want %q", source, want)
		} else if got := reasonOf(t, err); got != want {
			t.Errorf("CheckMount(%q) reason = %q, want %q", source, got, want)
		}
	}
}
