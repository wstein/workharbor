// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"regexp"
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
		case isDotGit(part):
			return "has a .git component"
		}
	}
	return ""
}

// isDotGit reports whether a path component is .git in any case, optionally
// followed by dots or spaces (ignored by some file systems).
func isDotGit(component string) bool {
	return strings.EqualFold(strings.TrimRight(component, ". "), ".git")
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
	_, userinfo, host, _, _ := urlAuthority(u)
	if strings.HasPrefix(userinfo, "-") || strings.HasPrefix(host, "-") {
		return "has a user or host starting with a hyphen"
	}
	return ""
}
