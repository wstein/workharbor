// Command ghguard is the PreToolUse hook for the Bash tool (#314). It reads the
// hook JSON on stdin and prints a PreToolUse "deny" decision when the command
// calls GraphQL through `gh api`; otherwise it prints nothing. It always exits 0
// on purpose: the hook is started with `go run`, which turns exit status 2 into
// 1, and only exit 2 would block. Unparsable input is denied (fail closed).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/wstein/workharbor/internal/ghguard"
)

func main() {
	data, err := io.ReadAll(os.Stdin)
	var in struct {
		ToolName  string `json:"tool_name"`
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if err == nil {
		err = json.Unmarshal(data, &in)
	}
	if err != nil {
		deny("ghguard: unreadable hook input, refusing: " + err.Error())
		return
	}
	if in.ToolName != "Bash" && in.ToolName != "Monitor" && in.ToolName != "PowerShell" {
		return
	}
	if r := ghguard.Check(in.ToolInput.Command); r != "" {
		deny("ghguard: " + r)
	}
}

func deny(reason string) {
	out, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "deny",
		"permissionDecisionReason": reason,
	}})
	fmt.Println(string(out))
}
