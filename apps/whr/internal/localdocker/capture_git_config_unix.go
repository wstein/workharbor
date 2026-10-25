// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func stageConfigEntries(ctx context.Context, home string, s stageGitSnapshot) (map[string][]string, string, error) {
	data, err := stageAdminFile(s.CommonDir, "config", 256<<10)
	if os.IsNotExist(err) {
		data = nil
		err = nil
	}
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(data)
	name := filepath.Join(home, "config-input")
	if err = os.WriteFile(name, data, 0600); err != nil {
		return nil, "", stageConflict("private Git configuration publication failed")
	}
	// --no-includes reads the bounded copied bytes as data. No source repository
	// context, include path, command, helper, template or transport is evaluated.
	raw, err := stageGitRead(ctx, home, "", "", "", nil, "config", "--null", "--no-includes", "--file", name, "--list")
	if err != nil {
		return nil, "", stageConflict("Git configuration data malformed")
	}
	values := map[string][]string{}
	for _, record := range bytes.Split(raw, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		key, value, _ := bytes.Cut(record, []byte{'\n'})
		values[string(key)] = append(values[string(key)], string(value))
	}
	return values, "sha256:" + hex.EncodeToString(hash[:]), nil
}
func stageCredentialFreeURL(value string) bool {
	if value == "" || len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n\t") {
		return false
	}
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil || u.RawQuery != "" || u.Fragment != "" {
			return false
		}
		switch u.Scheme {
		case "https", "http", "git", "file":
			return u.User == nil
		case "ssh":
			if u.User != nil {
				_, password := u.User.Password()
				return !password
			}
			return true
		default:
			return false
		}
	}
	// An explicit local path is not an scp authority, even with @ or : in it.
	if strings.ContainsAny(value, "?#") {
		return false
	}
	if filepath.IsAbs(value) || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") {
		return true
	}
	// In scp syntax, reject colon-bearing user information before @. A bare
	// SSH username remains admissible, including bracketed IPv6 hosts.
	if at := strings.IndexByte(value, '@'); at >= 0 {
		authority := value[:at]
		if strings.Contains(authority, "/") {
			return true
		}
		return authority != "" && !strings.Contains(authority, ":") && !strings.Contains(value[at+1:], "@")
	}
	return true
}
func writeStageSafeConfig(ctx context.Context, home string, s stageGitSnapshot, clone string, values map[string][]string) error {
	gitdir := filepath.Join(clone, ".git")
	config := "[core]\n\trepositoryformatversion = 0\n\tbare = false\n\tfilemode = true\n\tlogallrefupdates = true\n"
	if s.ObjectFormat == "sha256" {
		config = strings.Replace(config, "version = 0", "version = 1", 1) + "[extensions]\n\tobjectformat = sha256\n"
	}
	if err := os.WriteFile(filepath.Join(gitdir, "config"), []byte(config), 0600); err != nil {
		return stageConflict("private Git safe configuration publication failed")
	}
	remotes := map[string]bool{}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !strings.HasPrefix(key, "remote.") || !strings.HasSuffix(key, ".url") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".url")
		if name == "" || strings.ContainsAny(name, "\x00\r\n\t ") {
			continue
		}
		admitted := false
		for _, v := range values[key] {
			if stageCredentialFreeURL(v) {
				if _, err := stageGitRead(ctx, home, gitdir, "", "", nil, "config", "--file", filepath.Join(gitdir, "config"), "--add", key, v); err != nil {
					return err
				}
				admitted = true
			}
		}
		remotes[name] = admitted
		if admitted {
			if _, err := stageGitRead(ctx, home, gitdir, "", "", nil, "config", "--file", filepath.Join(gitdir, "config"), "--add", "remote."+name+".fetch", "+refs/heads/*:refs/remotes/"+name+"/*"); err != nil {
				return err
			}
		}
	}
	for _, key := range keys {
		if !strings.HasPrefix(key, "branch.") || !strings.HasSuffix(key, ".remote") {
			continue
		}
		branch := strings.TrimSuffix(strings.TrimPrefix(key, "branch."), ".remote")
		if !stageRef("refs/heads/" + branch) {
			continue
		}
		v := values[key]
		if len(v) != 1 || v[0] != "." && !remotes[v[0]] {
			continue
		}
		merge := values["branch."+branch+".merge"]
		if len(merge) != 1 || !stageRef(merge[0]) {
			continue
		}
		for k, val := range map[string]string{key: v[0], "branch." + branch + ".merge": merge[0]} {
			if _, err := stageGitRead(ctx, home, gitdir, "", "", nil, "config", "--file", filepath.Join(gitdir, "config"), "--add", k, val); err != nil {
				return err
			}
		}
	}
	return nil
}
