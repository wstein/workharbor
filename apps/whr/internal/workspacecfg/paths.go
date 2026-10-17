// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"regexp"
	"strconv"
	"strings"
)

// canonicalPathProblem returns why p is not a canonical manifest path (the
// rule of contracts/workspace/workharbor.schema.json and of CanonicalPath in
// the host agent, plus the case-folded .git the resolver is held to), or "".
func canonicalPathProblem(p string) string {
	switch {
	case p == "":
		return "is empty"
	case strings.HasPrefix(p, "/"):
		return "is absolute"
	case strings.ContainsAny(p, "\\\x00\r\n\t*?[]"):
		return "contains a backslash, control character or glob character"
	}
	for _, part := range strings.Split(p, "/") {
		switch {
		case part == "":
			return "has an empty component"
		case part == "." || part == "..":
			return "has a . or .. component"
		case windowsAlias(part):
			return "has a .git component or a Windows alias of ., .. or .git"
		}
	}
	return ""
}

// windowsAlias mirrors _windows_alias in scripts/check_repo.py. After dropping
// the format characters U+200B to U+200F, U+202A to U+202E, U+206A to U+206F
// and U+FEFF and folding U+FF0E to a dot and U+00A0 and U+3000 to a space, a
// component made only of dots and spaces is an alias, and so is one whose text
// before the first colon (an NTFS stream such as .git::$DATA) is .git or the
// 8.3 short name GIT~1 once trailing dots and spaces are removed.
func windowsAlias(component string) bool {
	folded := strings.Map(func(r rune) rune {
		switch {
		case r >= 0x200b && r <= 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x206a && r <= 0x206f, r == 0xfeff:
			return -1
		case r == 0xff0e:
			return '.'
		case r == 0xa0, r == 0x3000:
			return ' '
		}
		return r
	}, component)
	if strings.Trim(folded, ". ") == "" {
		return true
	}
	base, _, _ := strings.Cut(folded, ":")
	base = strings.TrimRight(base, ". ")
	return strings.EqualFold(base, ".git") || strings.EqualFold(base, "git~1")
}

var (
	projectURLPattern  = regexp.MustCompile(`^(https://[A-Za-z0-9.-]+(?::[0-9]{1,5})?/[A-Za-z0-9._~!$&'()*+,;=:@%/-]+|ssh://(?:[A-Za-z0-9._-]+@)?[A-Za-z0-9.-]+(?::[0-9]{1,5})?/[A-Za-z0-9._~!$&'()*+,;=:@%/-]+)$`)
	projectNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	workspaceName      = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)
)

const maxURL = 2048

// urlAuthority splits a scheme://authority/rest URL; ok is false for other forms.
func urlAuthority(u string) (scheme, userinfo, host, rest string, ok bool) {
	i := strings.Index(u, "://")
	if i <= 0 {
		return "", "", "", "", false
	}
	scheme, tail := u[:i], u[i+3:]
	auth := tail
	if j := strings.IndexAny(tail, "/?#"); j >= 0 {
		auth, rest = tail[:j], tail[j:]
	}
	if k := strings.LastIndex(auth, "@"); k >= 0 {
		userinfo, auth = auth[:k], auth[k+1:]
		return scheme, userinfo + "@", auth, rest, true
	}
	return scheme, "", auth, rest, true
}

// urlHasCredentials reports a token or password in an https or ssh URL: any
// https userinfo, or an ssh user part carrying a password.
func urlHasCredentials(u string) bool {
	scheme, userinfo, _, _, ok := urlAuthority(u)
	if !ok || userinfo == "" {
		return false
	}
	switch strings.ToLower(scheme) {
	case "https", "http":
		return true
	case "ssh":
		return strings.Contains(userinfo, ":")
	}
	return false
}

func wellFormedPercent(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '%' {
			if i+2 >= len(s) || hexVal(s[i+1]) < 0 || hexVal(s[i+2]) < 0 {
				return false
			}
			i += 2
		}
	}
	return true
}

// projectURLName derives the project directory name from a manifest project
// URL: the last path component with one trailing .git removed.
func projectURLName(u string) (string, bool) {
	_, _, _, rest, ok := urlAuthority(u)
	if !ok {
		return "", false
	}
	name := rest[strings.LastIndex(rest, "/")+1:]
	name = strings.TrimSuffix(name, ".git")
	if !projectNamePattern.MatchString(name) {
		return "", false
	}
	return name, true
}

// projectURLProblem checks a manifest project URL after the credential test.
func projectURLProblem(u string) string {
	switch {
	case len(u) > maxURL:
		return "is longer than 2048 bytes"
	case !projectURLPattern.MatchString(u):
		return "is not an https:// or ssh:// URL without query, fragment or unusual characters"
	case !wellFormedPercent(u):
		return "has a malformed percent escape"
	}
	_, userinfo, host, rest, _ := urlAuthority(u)
	if !portOK(host) {
		return "has a port outside 1-65535"
	}
	if strings.Contains(rest, "//") {
		return "has an empty path component"
	}
	if strings.HasPrefix(userinfo, "-") || strings.HasPrefix(host, "-") {
		return "has a user or host starting with a hyphen"
	}
	return ""
}

// portOK accepts a host without a port or with one in 1-65535.
func portOK(host string) bool {
	i := strings.LastIndex(host, ":")
	if i < 0 {
		return true
	}
	port, err := strconv.Atoi(host[i+1:])
	return err == nil && port >= 1 && port <= 65535
}
