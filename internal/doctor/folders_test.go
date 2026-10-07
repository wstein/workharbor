package doctor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const dfHeader = "Filesystem 512-blocks Used Available Capacity Mounted on\n"

// folderDeps is a configuration with one workspace root below a real, resolved
// temp folder; df and stat are scripted.
func folderDeps(t *testing.T, rel string) (Deps, scripted, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, rel)
	cfg := filepath.Join(base, "config.json")
	if err := os.WriteFile(cfg, []byte(`{"roots":{"workspaces":["`+root+`"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := scripted{"df -P " + base: dfHeader + "/dev/disk3s5 100 1 99 1% /"}
	return Deps{GOOS: "darwin", Runner: r, ConfigPath: cfg, Home: base}, r, root
}

func folderStep(t *testing.T, d Deps) Check {
	t.Helper()
	return steps(t, d)["workspace-folders"]
}

func TestAMissingWorkspaceRootIsMadeByTheListedSudoCommands(t *testing.T) {
	d, _, root := folderDeps(t, "ws")
	c := folderStep(t, d)
	if st, msg := c.Run(context.Background()); st != Fail || !strings.Contains(msg, "does not exist") {
		t.Fatalf("%s %s", st, msg)
	}
	cmds, err := c.Fix.Build(context.Background(), &answers{})
	if err != nil {
		t.Fatal(err)
	}
	var got [][]string
	for _, c := range cmds {
		if !c.Sudo {
			t.Errorf("%v runs without sudo", c.Argv)
		}
		got = append(got, c.Full())
	}
	want := [][]string{{"sudo", "mkdir", "-p", root}, {"sudo", "chown", "workharbor", root}, {"sudo", "chmod", "0700", root}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("commands %v, want %v", got, want)
	}
	for _, c := range c.Fix.Cmds { // the dry-run preview
		if !c.Sudo || strings.Contains(strings.Join(c.Argv, " "), root) {
			t.Errorf("preview %v", c.Argv)
		}
	}
}

func TestARightWorkspaceRootIsOKAndNeedsNothing(t *testing.T) {
	d, r, root := folderDeps(t, "ws")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	r["df -P "+root] = dfHeader + "/dev/disk3s5 100 1 99 1% /"
	r["stat -f %Su %Lp "+root] = "workharbor 700\n"
	c := folderStep(t, d)
	if st, msg := c.Run(context.Background()); st != OK {
		t.Fatalf("%s %s", st, msg)
	}
	if cmds, err := c.Fix.Build(context.Background(), &answers{}); err != nil || len(cmds) != 0 {
		t.Errorf("%v %v", cmds, err)
	}
}

func TestAWrongOwnerIsReportedAndChownIsOnlyOfferedNotTaken(t *testing.T) {
	d, r, root := folderDeps(t, "ws")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	r["df -P "+root] = dfHeader + "/dev/disk3s5 100 1 99 1% /"
	r["stat -f %Su %Lp "+root] = "alice 700\n"
	c := folderStep(t, d)
	if st, msg := c.Run(context.Background()); st != Fail || !strings.Contains(msg, "belongs to alice") {
		t.Fatalf("%s %s", st, msg)
	}
	if cmds, err := c.Fix.Build(context.Background(), &answers{confirm: false}); err == nil || len(cmds) != 0 {
		t.Errorf("declined, yet %v %v", cmds, err)
	}
	cmds, err := c.Fix.Build(context.Background(), &answers{confirm: true})
	if err != nil || len(cmds) != 2 || cmds[0].Argv[0] != "chown" || cmds[1].Argv[0] != "chmod" {
		t.Errorf("%v %v", cmds, err)
	}
}

func TestAWrongModeNeedsOnlyChmod(t *testing.T) {
	d, r, root := folderDeps(t, "ws")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	r["df -P "+root] = dfHeader + "/dev/disk3s5 100 1 99 1% /"
	r["stat -f %Su %Lp "+root] = "workharbor 755\n"
	c := folderStep(t, d)
	if st, msg := c.Run(context.Background()); st != Fail || !strings.Contains(msg, "mode 755") {
		t.Fatalf("%s %s", st, msg)
	}
	cmds, err := c.Fix.Build(context.Background(), &answers{})
	if err != nil || len(cmds) != 1 || !reflect.DeepEqual(cmds[0].Argv, []string{"chmod", "0700", root}) {
		t.Errorf("%v %v", cmds, err)
	}
}

func TestAPathUnderASymlinkIsRefusedAndNoCommandIsBuilt(t *testing.T) {
	d, _, root := folderDeps(t, "link/ws")
	base := filepath.Dir(filepath.Dir(root))
	target := filepath.Join(base, "real")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	c := folderStep(t, d)
	if st, msg := c.Run(context.Background()); st != Fail || !strings.Contains(msg, "symbolic link") {
		t.Fatalf("%s %s", st, msg)
	}
	if cmds, err := c.Fix.Build(context.Background(), &answers{confirm: true}); err == nil || len(cmds) != 0 {
		t.Errorf("%v %v", cmds, err)
	}
}

func TestAVolumeThatIsNotMountedIsRefused(t *testing.T) {
	d, r, _ := folderDeps(t, "ws")
	if err := os.WriteFile(d.ConfigPath, []byte(`{"roots":{"workspaces":["/Volumes/NoSuchDisk395/workspaces"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, anc := range []string{"/Volumes", "/"} {
		r["df -P "+anc] = dfHeader + "/dev/disk3s5 100 1 99 1% /"
	}
	st, msg := folderStep(t, d).Run(context.Background())
	if st != Fail || !strings.Contains(msg, "is not mounted") {
		t.Errorf("%s %s", st, msg)
	}
}

func TestWithoutAConfigurationOrDfTheFolderIsNotVerified(t *testing.T) {
	d, r, _ := folderDeps(t, "ws")
	delete(r, "df -P "+filepath.Dir(d.ConfigPath))
	if st, _ := folderStep(t, d).Run(context.Background()); st != NotVerified {
		t.Errorf("df fails: %s", st)
	}
	d.ConfigPath = filepath.Join(t.TempDir(), "none.json")
	if st, _ := folderStep(t, d).Run(context.Background()); st != NotVerified {
		t.Errorf("no config: %s", st)
	}
}
