package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/setup"
	"github.com/wstein/workharbor/internal/version"
)

func setupPresentation(steps []doctor.Check, outcomes []setup.Outcome, phase doctor.Phase, repair repairContext) doctor.Presentation {
	checks := make([]doctor.ReportCheck, 0, len(outcomes))
	for _, outcome := range outcomes {
		r := doctor.Result{Check: outcome.Step, Phase: phase, Status: outcome.Status, Detail: outcome.Detail}
		for _, step := range steps {
			if step.Name == outcome.Step {
				r.Step = step.Step
				if outcome.Status != doctor.OK {
					context := repair
					if outcome.UseUser != "" {
						context.Account = outcome.UseUser
					}
					fix := step.FixCommand(outcome.Detail)
					if outcome.Remedy != "" { // not reachable: the step that unblocks it, not its own
						fix = outcome.Remedy
					}
					r.Fix = context.command(fix)
				}
				break
			}
		}
		checks = append(checks, doctor.ReportCheck{Result: r, Fixed: &outcome.Fixed, Asked: &outcome.Asked})
	}
	return doctor.Present(checks)
}

func writeSetupReport(st *state, configPath string, p doctor.Presentation, source string, phase doctor.Phase, account string) error {
	// A missing config is normal during onboarding. Read only state_dir: report
	// writing must also work when the rest of the configuration is not ready.
	var cc struct {
		StateDir string `json:"state_dir"`
	}
	data, err := os.ReadFile(configPath) //nolint:gosec // the user names their configuration file
	if err == nil {
		err = json.Unmarshal(data, &cc)
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("setup report configuration: %w", err)
	}
	home := st.env.Getenv("HOME")
	hostname, _ := os.Hostname()
	artifact := p.Artifact(time.Now(), version.Get().Version, source, phase, account, home, hostname)
	_, err = doctor.WriteArtifact(config.StateDirOf(cc.StateDir, home), artifact)
	if err != nil {
		return fmt.Errorf("setup report: %w", err)
	}
	return nil
}

// repairContext preserves invocation settings in diagnostic repair commands.
// It decorates the existing command so doctor retains its JSON byte contract.
type repairContext struct {
	Prefix  string
	Account string
	RunAs   string
}

func (c repairContext) command(fix string) string {
	if !strings.HasPrefix(fix, "whr setup") {
		return fix
	}
	host := isHostFix(fix)
	if c.RunAs != "" && fix == "whr setup --only config-base" {
		// the administrator's run initializes the account's base configuration itself
		fix, host = "whr setup --only config-first", true
	}
	if c.Prefix != "" {
		fix += " --prefix " + shellArgument(c.Prefix)
	}
	if c.Account != "" && c.Account != doctor.WhrUser {
		fix += " --user " + shellArgument(c.Account)
	}
	// a fix of the user phase runs as whr's account, whatever check names it; a
	// fix naming a host step (--only <host step>) is the administrator's
	if c.RunAs != "" && !host {
		fix += " (run as " + c.RunAs + ")"
	}
	return fix
}

// isHostFix reports whether the fix names a step of the administrator's part.
func isHostFix(fix string) bool {
	f := strings.Fields(fix)
	for _, w := range f { // a doctor fix that already names the account (--user) is the administrator's
		if w == "--user" {
			return true
		}
	}
	for i, w := range f {
		if w == "--only" && i+1 < len(f) {
			for _, s := range doctor.Steps(doctor.Checks(doctor.Deps{ConfigPath: "x/config.json"}), doctor.PhaseHost) {
				if s.Name == f[i+1] {
					return true
				}
			}
		}
	}
	return false
}
