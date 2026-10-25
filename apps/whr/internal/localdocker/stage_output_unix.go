//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

var stageFactKeys = []string{"HOST_NAME", "HOST_IP", "HOST_OS", "HOST_ARCH", "HOST_ENGINE", "HOST_ENGINE_ARCH", "HOST_CONTEXT"}

// Only the producer's single-quoted POSIX data form is admitted. This parser
// does not invoke a shell, expand variables, or interpret command substitution.
func validateStageHostFacts(data []byte) error {
	bad := func() error { return stageConflict("host facts literal data invalid") }
	if len(data) > 65536 || !utf8.Valid(data) || len(data) == 0 || data[len(data)-1] != '\n' {
		return bad()
	}
	lines := bytes.Split(data[:len(data)-1], []byte{'\n'})
	if len(lines) != len(stageFactKeys) {
		return bad()
	}
	for i, line := range lines {
		prefix := stageFactKeys[i] + "="
		if !bytes.HasPrefix(line, []byte(prefix)) {
			return bad()
		}
		s := string(line[len(prefix):])
		if len(s) < 2 || s[0] != '\'' {
			return bad()
		}
		var value strings.Builder
		for {
			s = s[1:]
			end := strings.IndexByte(s, '\'')
			if end < 0 {
				return bad()
			}
			value.WriteString(s[:end])
			s = s[end+1:]
			if s == "" {
				break
			}
			if !strings.HasPrefix(s, "\\''") {
				return bad()
			}
			value.WriteByte('\'')
			s = s[2:]
		}
		if value.Len() > 4096 {
			return bad()
		}
		for _, r := range value.String() {
			if unicode.IsControl(r) {
				return bad()
			}
		}
	}
	return nil
}

func readStageHostFacts(path string) ([]byte, string, error) {
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, "", stageConflict("host facts file unavailable")
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var before, after unix.Stat_t
	initial, statErr := f.Stat()
	if statErr != nil || unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || int(before.Uid) != os.Getuid() || before.Nlink != 1 || before.Size > 65536 {
		return nil, "", stageConflict("host facts file identity or quota")
	}
	b, e := io.ReadAll(io.LimitReader(f, 65537))
	if e != nil || len(b) > 65536 {
		return nil, "", stageConflict("host facts file read quota")
	}
	if e = validateStageHostFacts(b); e != nil {
		return nil, "", e
	}
	final, statErr := f.Stat()
	if statErr != nil || unix.Fstat(fd, &after) != nil || before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size || !initial.ModTime().Equal(final.ModTime()) || before.Mode != after.Mode || before.Nlink != after.Nlink {
		return nil, "", stageConflict("host facts file changed during admission")
	}
	h := sha256.Sum256(b)
	return b, "sha256:" + hex.EncodeToString(h[:]), nil
}

func publishStageHostFacts(clone string, data []byte) error {
	// Open the entire clone-relative path without following either directory or
	// target symlinks; publication never resolves through candidate-controlled data.
	root, e := unix.Open(clone, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return stageConflict("candidate host facts clone unavailable")
	}
	defer unix.Close(root)
	dir, e := unix.Openat(root, ".devcontainer", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return stageConflict("candidate host facts directory unavailable")
	}
	defer unix.Close(dir)
	var st unix.Stat_t
	if e = unix.Fstatat(dir, "host.env", &st, unix.AT_SYMLINK_NOFOLLOW); e != unix.ENOENT {
		return stageConflict("candidate host facts target must be absent")
	}
	if e = validateStageHostFacts(data); e != nil {
		return e
	}
	var nonce [16]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		return stageConflict("candidate host facts private name unavailable")
	}
	temp := ".whr-host-facts-" + hex.EncodeToString(nonce[:])
	fd, e := unix.Openat(dir, temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0644)
	if e != nil {
		return stageConflict("candidate host facts publication unavailable")
	}
	f := os.NewFile(uintptr(fd), temp)
	if e = f.Chmod(0644); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		e = unix.Linkat(dir, temp, dir, "host.env", 0)
	}
	if e != nil {
		return stageConflict("candidate host facts publication retained or changed")
	}
	if e = unix.Unlinkat(dir, temp, 0); e != nil {
		return stageConflict("candidate host facts publication uncertain")
	}
	if e = unix.Fsync(dir); e != nil {
		return stageConflict("candidate host facts durability uncertain")
	}
	actual, _, e := readStageHostFacts(filepath.Join(clone, ".devcontainer", "host.env"))
	if e != nil || !bytes.Equal(actual, data) {
		return stageConflict("candidate host facts publication changed")
	}
	return nil
}
