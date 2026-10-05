package skillset

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

type Store struct {
	Root      string
	Forbidden []string
}

type Package struct {
	Pin       Pin
	Manifest  Manifest
	Directory string
	Text      string
}

func (p Package) Target() string { return "/skills/" + p.Pin.InventorySHA256 }

func overlaps(first, second string) bool {
	return first == second || strings.HasPrefix(first, second+string(filepath.Separator)) || strings.HasPrefix(second, first+string(filepath.Separator))
}

func containsIdentity(root, candidate string) bool {
	info, err := os.Stat(root)
	if err != nil {
		return false
	}
	for current := candidate; ; current = filepath.Dir(current) {
		if candidateInfo, err := os.Stat(current); err == nil && os.SameFile(info, candidateInfo) {
			return true
		}
		if current == filepath.Dir(current) {
			return false
		}
	}
}

func secure(info fs.FileInfo, directory bool) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() || info.Mode().Perm()&0o022 != 0 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return errors.New("skill content must be owned by the supervisor with no group/other write or special permissions")
	}
	if directory {
		if !info.IsDir() {
			return errors.New("skill directory is not a directory")
		}
	} else if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 != 0 || stat.Nlink != 1 {
		return errors.New("skill resource must be regular, nonexecutable text with no links")
	}
	return nil
}

func (s Store) CheckRoot() error {
	if !filepath.IsAbs(s.Root) || filepath.Clean(s.Root) != s.Root || s.Root == "/" {
		return errors.New("skill store needs a dedicated absolute directory")
	}
	resolved, err := filepath.EvalSymlinks(s.Root)
	if err != nil {
		return err
	}
	if resolved != s.Root {
		return errors.New("skill store cannot alias another directory")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if s.Root == home || strings.HasPrefix(home, s.Root+"/") {
		return errors.New("skill store cannot expose the host home")
	}
	for _, forbidden := range append(append([]string{}, s.Forbidden...), filepath.Join(home, ".ssh"), filepath.Join(home, ".claude"), filepath.Join(home, ".codex")) {
		if forbidden == "" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(forbidden)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			resolved = filepath.Clean(forbidden)
		}
		if overlaps(s.Root, resolved) || containsIdentity(s.Root, resolved) || containsIdentity(resolved, s.Root) {
			return fmt.Errorf("skill store overlaps forbidden root %s", forbidden)
		}
	}
	for current := s.Root; current != "/"; current = filepath.Dir(current) {
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return errors.New("skill store cannot live in a repository")
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	info, err := os.Lstat(s.Root)
	if err != nil {
		return err
	}
	return secure(info, true)
}

func readText(root *os.Root, name string, limit int64) ([]byte, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	return readTextChecked(root, name, limit, before)
}

func openNoFollow(root *os.Root, name string, directory bool) (*os.File, error) {
	parent, err := root.OpenFile(".", os.O_RDONLY|unix.O_NONBLOCK|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	info, err := parent.Stat()
	if err != nil {
		return nil, err
	}
	if err := secure(info, true); err != nil {
		return nil, err
	}
	components := strings.Split(name, "/")
	for index, component := range components {
		isDirectory := directory || index < len(components)-1
		flags := unix.O_RDONLY | unix.O_NONBLOCK | unix.O_NOFOLLOW | unix.O_CLOEXEC
		if isDirectory {
			flags |= unix.O_DIRECTORY
		}
		descriptor, err := unix.Openat(int(parent.Fd()), component, flags, 0)
		if err != nil {
			return nil, &os.PathError{Op: "openat", Path: name, Err: err}
		}
		file := os.NewFile(uintptr(descriptor), name)
		if index == len(components)-1 {
			return file, nil
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return nil, err
		}
		if err := secure(info, true); err != nil {
			return nil, err
		}
		parent = file
	}
	return nil, errors.New("empty skill resource path")
}

func readTextChecked(root *os.Root, name string, limit int64, before fs.FileInfo) ([]byte, error) {
	if err := secure(before, false); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if before.Size() > limit {
		return nil, fmt.Errorf("%s exceeds skill resource limit", name)
	}
	file, err := openNoFollow(root, name, false)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if err := secure(opened, false); err != nil {
		return nil, err
	}
	if !os.SameFile(before, opened) {
		return nil, errors.New("skill resource changed during validation")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
		return nil, fmt.Errorf("%s is over limit, changed or not UTF-8 text", name)
	}
	for _, char := range string(data) {
		if char < 32 && char != '\n' && char != '\r' && char != '\t' {
			return nil, fmt.Errorf("%s contains binary control bytes", name)
		}
	}
	return data, nil
}

func validate(directory string, pin Pin) (Package, map[string][]byte, error) {
	if err := pin.Validate(); err != nil {
		return Package{}, nil, err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return Package{}, nil, err
	}
	if err := secure(info, true); err != nil {
		return Package{}, nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return Package{}, nil, err
	}
	defer root.Close()
	manifest, err := readText(root, ManifestName, MaxManifestBytes)
	if err != nil {
		return Package{}, nil, err
	}
	if Digest(manifest) != pin.ManifestSHA256 {
		return Package{}, nil, errors.New("skill manifest hash differs from the external lock")
	}
	var declaration Manifest
	if err := decode(manifest, &declaration); err != nil {
		return Package{}, nil, err
	}
	if err := declaration.Validate(pin); err != nil {
		return Package{}, nil, err
	}
	expected := map[string]string{ManifestName: pin.ManifestSHA256}
	directories := map[string]bool{".": true}
	aliases := map[string]string{}
	for _, file := range declaration.Files {
		expected[file.Path] = file.SHA256
		for parent := filepath.ToSlash(filepath.Dir(file.Path)); parent != "."; parent = filepath.ToSlash(filepath.Dir(parent)) {
			directories[parent] = true
		}
	}
	contents := map[string][]byte{}
	total := int64(0)
	err = walkBounded(root, func(name string, entry fs.FileInfo) error {
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		if err := secure(info, entry.IsDir()); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if name != "." && !validPath(name) {
			return fmt.Errorf("invalid skill resource path %q", name)
		}
		key := strings.ToLower(name)
		if previous, ok := aliases[key]; ok && previous != name {
			return errors.New("case-colliding skill paths")
		}
		aliases[key] = name
		if entry.IsDir() {
			if !directories[name] {
				return fmt.Errorf("unexpected skill directory %s", name)
			}
			return nil
		}
		digest, ok := expected[name]
		if !ok {
			return fmt.Errorf("unexpected skill resource %s", name)
		}
		total += info.Size()
		if total > MaxTotalBytes {
			return errors.New("skill package exceeds total byte limit")
		}
		data, err := readText(root, name, MaxFileBytes)
		if err != nil {
			return err
		}
		if Digest(data) != digest {
			return fmt.Errorf("skill resource hash changed: %s", name)
		}
		contents[name] = data
		return nil
	})
	if err != nil {
		return Package{}, nil, err
	}
	if len(contents) != len(expected) {
		return Package{}, nil, errors.New("skill inventory has missing resources")
	}
	return Package{Pin: pin, Manifest: declaration, Directory: directory, Text: string(contents[declaration.Entrypoint])}, contents, nil
}

func walkBounded(root *os.Root, visit func(string, fs.FileInfo) error) error {
	count := 0
	var walk func(string) error
	walk = func(name string) error {
		count++
		if count > MaxFiles*16 {
			return errors.New("skill directory entry count exceeds limits")
		}
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		if err := visit(name, info); err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		directory, err := openNoFollow(root, name, true)
		if err != nil {
			return err
		}
		defer directory.Close()
		opened, err := directory.Stat()
		if err != nil {
			return err
		}
		if err := secure(opened, true); err != nil {
			return err
		}
		if !os.SameFile(info, opened) {
			return errors.New("skill directory changed during validation")
		}
		for {
			entries, err := directory.ReadDir(128)
			for _, entry := range entries {
				if err := walk(filepath.Join(name, entry.Name())); err != nil {
					return err
				}
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
		}
	}
	return walk(".")
}

func (s Store) Load(pin Pin) (Package, error) {
	if err := s.CheckRoot(); err != nil {
		return Package{}, err
	}
	if err := pin.Validate(); err != nil {
		return Package{}, err
	}
	directory := s.revisionDirectory(pin)
	if _, err := os.Lstat(directory); errors.Is(err, fs.ErrNotExist) {
		directory = filepath.Join(s.Root, pin.InventorySHA256)
	} else if err != nil {
		return Package{}, err
	}
	result, _, err := validate(directory, pin)
	return result, err
}

func (s Store) revisionDirectory(pin Pin) string {
	return filepath.Join(s.Root, pin.InventorySHA256+"-"+pin.ManifestSHA256)
}

func (s Store) Install(source string, pin Pin) (Package, error) {
	if err := s.CheckRoot(); err != nil {
		return Package{}, err
	}
	_, contents, err := validate(source, pin)
	if err != nil {
		return Package{}, err
	}
	target := s.revisionDirectory(pin)
	if _, err := os.Lstat(target); err == nil {
		return s.Load(pin)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Package{}, err
	}
	temporary, err := os.MkdirTemp(s.Root, ".install-")
	if err != nil {
		return Package{}, err
	}
	defer os.RemoveAll(temporary)
	for name, data := range contents {
		filename := filepath.Join(temporary, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			return Package{}, err
		}
		if err := os.WriteFile(filename, data, 0o400); err != nil {
			return Package{}, err
		}
	}
	if _, _, err := validate(temporary, pin); err != nil {
		return Package{}, err
	}
	if err := os.Rename(temporary, target); err != nil {
		return Package{}, err
	}
	return s.Load(pin)
}
