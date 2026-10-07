package doctor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/wstein/workharbor/internal/render"
	"github.com/wstein/workharbor/internal/textsafe"
)

// Volume is a mounted disk volume that could hold the workspaces, as
// `diskutil info -plist` describes it. Name and Mount are raw: escape them
// (textsafe.Escape) before they are shown.
type Volume struct {
	Name     string
	Mount    string
	FS       string // diskutil's FilesystemType, such as apfs, exfat, msdos
	Size     int64  // bytes
	Free     int64  // bytes; -1 when diskutil did not say
	Internal bool
}

// APFS reports whether the volume is APFS.
func (v Volume) APFS() bool { return strings.EqualFold(v.FS, "apfs") }

// maxVolumes bounds how many mount points are asked about.
const maxVolumes = 32

// mountPoints collects the MountPoint entries of `diskutil list -plist`, in the
// order diskutil gives them and without duplicates. Only absolute paths without
// control characters are kept: they are passed to diskutil as arguments.
func mountPoints(list any) []string {
	var out []string
	seen := map[string]bool{}
	var walk func(v any, depth int)
	walk = func(v any, depth int) {
		if depth > plistMaxDepth {
			return
		}
		switch t := v.(type) {
		case map[string]any:
			if m, ok := t["MountPoint"].(string); ok && strings.HasPrefix(m, "/") && textsafe.Escape(m) == m && !seen[m] && len(out) < maxVolumes {
				seen[m] = true
				out = append(out, m)
			}
			for _, k := range sortedKeys(t) {
				if k != "MountPoint" {
					walk(t[k], depth+1)
				}
			}
		case []any:
			for _, e := range t {
				walk(e, depth+1)
			}
		}
	}
	walk(list, 0)
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// parseVolume reads one `diskutil info -plist` answer. A volume that is not
// writable, is the sealed system image, or has no mount point, size or name
// cannot hold workspaces: ok is false. On APFS FreeSpace is 0 for the volume,
// and the free space is the container's (APFSContainerFree).
func parseVolume(b []byte) (Volume, bool) {
	root, err := parsePlist(b)
	if err != nil {
		return Volume{}, false
	}
	m, _ := root.(map[string]any)
	str := func(k string) string { s, _ := m[k].(string); return s }
	num := func(k string) (int64, bool) { n, ok := m[k].(int64); return n, ok }
	flag := func(k string) bool { f, _ := m[k].(bool); return f }
	v := Volume{Name: str("VolumeName"), Mount: str("MountPoint"), FS: str("FilesystemType"), Free: -1, Internal: flag("Internal")}
	v.Size, _ = num("TotalSize")
	if v.APFS() {
		if n, ok := num("APFSContainerFree"); ok {
			v.Free = n
		}
	} else if n, ok := num("FreeSpace"); ok {
		v.Free = n
	}
	if !strings.HasPrefix(v.Mount, "/") || v.Size <= 0 || v.Name == "" || !flag("Writable") || flag("SystemImage") {
		return Volume{}, false
	}
	return v, true
}

// volumes lists the volumes that could hold the workspaces: what
// `diskutil list -plist` mounts, each described by `diskutil info -plist`. The
// Mac's other system volumes (Preboot, VM, Update, ...) are left out. It reads
// only; a command that fails or prints something unreadable is an error with
// no raw output.
func (d Deps) volumes(ctx context.Context) ([]Volume, error) {
	out, err := d.output(ctx, "diskutil", "list", "-plist")
	if err != nil {
		return nil, errors.New("diskutil list did not answer: " + oneLine(err.Error()))
	}
	list, err := parsePlist([]byte(out))
	if err != nil {
		return nil, errors.New("diskutil list could not be read: " + err.Error())
	}
	var vols []Volume
	for _, mount := range mountPoints(list) {
		if strings.HasPrefix(mount, "/System/Volumes/") && mount != dataMount {
			continue
		}
		info, err := d.output(ctx, "diskutil", "info", "-plist", mount)
		if err != nil {
			continue
		}
		if v, ok := parseVolume([]byte(info)); ok {
			vols = append(vols, v)
		}
	}
	if len(vols) == 0 {
		return nil, errors.New("diskutil listed no volume that can hold workspaces")
	}
	return vols, nil
}

// dataMount is the Mac's own data volume, the default for the workspaces.
const dataMount = "/System/Volumes/Data"

// size formats bytes in decimal units, as macOS does.
func size(n int64) string {
	if n < 0 {
		return "unknown"
	}
	f, unit := float64(n), "B"
	for _, u := range []string{"kB", "MB", "GB", "TB"} {
		if f < 1000 {
			break
		}
		f, unit = f/1000, u
	}
	if unit == "B" {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", f, unit)
}

// clip shortens s to at most n runes, with "..." where it cut.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-3]) + "..."
}

// label is the two lines that describe a volume in the choice, at most 76
// columns wide. Names come from disks and may hold escapes: they are escaped.
func (v Volume) label(n int) string {
	kind := "other (" + clip(textsafe.Escape(v.FS), 12) + ")"
	if v.APFS() {
		kind = "APFS"
	}
	place := "external"
	if v.Internal {
		place = "internal"
	}
	return fmt.Sprintf("%3d) %s: %s, %s free, %s, %s\n     %s", n, clip(textsafe.Escape(v.Name), 24),
		size(v.Size), size(v.Free), kind, place, clip(textsafe.Escape(v.Mount), 70))
}

// workspacesIn is the folder for the workspaces on a volume: the Mac's own data
// volume keeps them in the home folder, any other volume gets its own folder.
func (d Deps) workspacesIn(v Volume) string {
	if v.Mount == dataMount || v.Mount == "/" {
		return filepath.Join(d.Home, "workspaces")
	}
	return filepath.Join(v.Mount, "workspaces")
}

// askFolder asks for a folder by its path; Enter takes def and q quits.
func askFolder(p Prompter, def string) (string, error) {
	s, err := p.Line("Folder for workspaces [" + def + "]")
	if err != nil {
		return "", err
	}
	s = strings.TrimSpace(s)
	switch {
	case strings.EqualFold(s, "q"), strings.EqualFold(s, "quit"):
		return "", render.ErrQuit
	case s == "":
		return def, nil
	case !filepath.IsAbs(s) || textsafe.Escape(s) != s:
		return "", errors.New("that is not an absolute path; nothing was written")
	}
	return filepath.Clean(s), nil
}

// chooseWorkspaces lists the volumes and asks which one holds the workspaces:
// a number, Enter for the default (the Mac's own data volume), q to quit, or
// the last entry to type a path. It only chooses a folder: it never erases,
// formats or adds a volume. With --yes it takes the default without asking.
// Without a usable disk list it asks for the path alone.
func (d Deps) chooseWorkspaces(ctx context.Context, p Prompter) (string, error) {
	def := filepath.Join(d.Home, "workspaces")
	vols, err := d.volumes(ctx)
	if err != nil {
		p.Show("The disk list is not available (" + err.Error() + ").")
		return askFolder(p, def)
	}
	defIdx := 1
	for i, v := range vols {
		if v.Mount == dataMount {
			defIdx = i + 1
			break
		}
	}
	other := len(vols) + 1
	lines := []string{"Volumes that can hold the workspaces:"}
	for i, v := range vols {
		lines = append(lines, v.label(i+1))
	}
	lines = append(lines, fmt.Sprintf("%3d) other path", other))
	p.Show(strings.Join(lines, "\n"))
	if d.Yes {
		p.Show(fmt.Sprintf("yes: volume %d, the default", defIdx))
		return d.workspacesIn(vols[defIdx-1]), nil
	}
	s, err := p.Line(fmt.Sprintf("Volume number [%d, q quits]", defIdx))
	if err != nil {
		return "", err
	}
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "q") || strings.EqualFold(s, "quit") {
		return "", render.ErrQuit
	}
	n := defIdx
	if s != "" {
		if n, err = strconv.Atoi(s); err != nil || n < 1 || n > other {
			return "", errors.New("that is not a number from the list; nothing was written")
		}
	}
	if n == other {
		return askFolder(p, def)
	}
	return d.workspacesIn(vols[n-1]), nil
}
