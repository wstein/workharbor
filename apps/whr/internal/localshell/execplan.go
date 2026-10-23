// SPDX-License-Identifier: EUPL-1.2
package localshell

import (
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"path"
	"regexp"
	"strings"
)

var fullContainerID = regexp.MustCompile(`^[a-f0-9]{64}$`)

// ExecPlan contains only validated values for the single FR-004 invocation.
// Its fields are private; Args returns a new fixed argument vector each time.
type ExecPlan struct {
	endpoint string
	metadata Metadata
	shell    string
}

func NewExecPlan(endpoint string, m Metadata, shell string) (ExecPlan, error) {
	if !strings.HasPrefix(endpoint, "unix://") || !path.IsAbs(strings.TrimPrefix(endpoint, "unix://")) {
		return ExecPlan{}, exitcode.Errorf(exitcode.PathConflict, "local shell requires a Unix Docker endpoint")
	}
	for _, v := range []struct {
		field, value string
		valid        bool
	}{{"container ID", m.ID, fullContainerID.MatchString(m.ID)}, {"container name", m.Name, containerName.MatchString(m.Name)}, {"user", m.User, userName.MatchString(m.User)}, {"working directory", m.Directory, validDirectory(m.Directory)}, {"TERM", m.Term, terminalName.MatchString(m.Term)}, {"human_shell", shell, shell == "fish" || shell == "bash" || shell == "zsh" || shell == "nu"}} {
		if !v.valid {
			return ExecPlan{}, invalidValue(v.field, v.value)
		}
	}
	return ExecPlan{endpoint: endpoint, metadata: m, shell: shell}, nil
}
func (p ExecPlan) Args() []string {
	m := p.metadata
	return []string{"--host=" + p.endpoint, "exec", "--interactive", "--tty", "--detach-keys=ctrl-],ctrl-^", "--user=" + m.User, "--workdir=" + m.Directory, "--env=TERM=" + m.Term, "--env=WHR_LOCAL_WORKSPACE=" + m.Name, "--env=WHR_LOCAL_MODE=1", m.ID, p.shell, "-l"}
}
