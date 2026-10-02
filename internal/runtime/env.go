package runtime

import (
	"fmt"
	"regexp"
	"strings"
)

// reservedEnv are environment variables the supervisor sets or relies on. A
// repository may not set them: it could point the agent's traffic past the
// egress proxy's settings, move the agent's home, replace the agent binary
// through PATH, or preload a library (design D38: a file may request, never
// grant). They are refused in the environment of a spec and in a build
// argument, because the proxy variables are predefined build arguments too.
var reservedEnv = map[string]bool{
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true, "ALL_PROXY": true, "FTP_PROXY": true,
	"PATH": true, "HOME": true, "LD_PRELOAD": true, "LD_LIBRARY_PATH": true,
}

// reservedEnvPrefix are prefixes of variables the agent CLIs, workharbor and the builder read (BUILDKIT_SYNTAX names a build frontend,
// an image the builder runs).
var reservedEnvPrefix = []string{"WHR_", "CLAUDE_", "ANTHROPIC_", "OPENAI_", "CODEX_", "GEMINI_", "GOOGLE_", "BUILDKIT_"}

// ReservedEnv reports whether a variable name is one the supervisor sets or the
// agent reads, in any letter case.
func ReservedEnv(name string) bool {
	u := strings.ToUpper(name)
	if reservedEnv[u] {
		return true
	}
	for _, p := range reservedEnvPrefix {
		if strings.HasPrefix(u, p) {
			return true
		}
	}
	return false
}

// envNameRe is a variable name the adapter passes on.
var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// checkEnv returns what is wrong with an environment or build-argument map.
func checkEnv(kind string, env map[string]string) []string {
	var problems []string
	for k, v := range env {
		switch {
		case !envNameRe.MatchString(k):
			problems = append(problems, fmt.Sprintf("%s name %q is not a variable name", kind, k))
		case ReservedEnv(k):
			problems = append(problems, fmt.Sprintf("%s %q is set by the supervisor", kind, k))
		case strings.ContainsAny(v, "\x00\n\r") || len(v) > 4096:
			problems = append(problems, fmt.Sprintf("%s %q has a value with a control character or over 4096 bytes", kind, k))
		}
	}
	return problems
}
