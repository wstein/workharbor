package domain

import (
	"regexp"
	"strings"
)

var branchNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// AgentBranchPrefix is the namespace of the agents' topic branches. No
// integration branch lies under it.
const AgentBranchPrefix = "agent/"

// ValidBranchName accepts a plain branch name: no leading dash or slash, no
// "..", no empty or dotted path elements, no ".lock" suffix, not HEAD, and only
// the characters of branchNameRE (so no control character). It is the one
// ref-name rule that the configuration, hostgit and NewWorkspace share.
func ValidBranchName(name string) bool {
	if name == "HEAD" || !branchNameRE.MatchString(name) || strings.Contains(name, "..") ||
		strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".lock") ||
		strings.HasSuffix(name, ".") || strings.Contains(name, "//") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// InAgentNamespace reports whether the first path component of a branch name is
// "agent" under case folding. Repositories live on case-insensitive file systems
// (APFS), where refs/heads/Agent/x and refs/heads/agent/x are one file, so the
// namespace is compared without case everywhere it is checked, and a bare
// "agent" counts: git cannot hold it beside agent/<role>.
func InAgentNamespace(name string) bool {
	first, _, _ := strings.Cut(name, "/")
	return strings.EqualFold(first, "agent")
}

// ValidIntegrationBranch reports whether name can be a workspace's integration
// branch: a valid branch name outside the agents' namespace (D42, design §4.3).
func ValidIntegrationBranch(name string) bool {
	return ValidBranchName(name) && !InAgentNamespace(name)
}
