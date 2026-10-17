// SPDX-License-Identifier: EUPL-1.2

//go:build linux

package workspacecfg_test

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
)

// snapshot lists every entry of a tree (including .git) with name, type, size,
// mode, inode, mtime and ctime; atime is excluded.
func snapshot(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		st := info.Sys().(*syscall.Stat_t)
		rel, _ := filepath.Rel(root, p)
		lines = append(lines, fmt.Sprintf("%s mode=%o size=%d ino=%d mtime=%d.%09d ctime=%d.%09d",
			rel, st.Mode, st.Size, st.Ino, st.Mtim.Sec, st.Mtim.Nsec, st.Ctim.Sec, st.Ctim.Nsec))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
