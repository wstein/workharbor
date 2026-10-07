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
					r.Fix = context.command(step.FixCommand(outcome.Detail))
				}
				break
			}
		}
		checks = append(checks, doctor.ReportCheck{Result: r, Fixed: &outcome.Fixed, Asked: &outcome.Asked})
	}
	return doctor.Present(checks)
}

func writeSetupReport(st *state, configPath string, p doctor.Presentation, source string, phase doctor.Phase, account string, dev bool) error {
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
	artifact := p.Artifact(time.Now(), version.Get().Version, source, phase, account, dev, home, hostname)
	_, err = doctor.WriteArtifact(config.StateDirOf(cc.StateDir, home), artifact)
	if err != nil {
		return fmt.Errorf("setup report: %w", err)
	}
	return nil
}

// repairContext preserves invocation settings in diagnostic repair commands.
// It decorates the existing command so doctor retains its JSON byte contract.
type repairContext struct {
	Dev     bool
	Managed bool
	Prefix  string
	Account string
	RunAs   string
}

func (c repairContext) command(fix string) string {
	if !strings.HasPrefix(fix, "whr setup") {
		return fix
	}
	for _, flag := range []struct {
		enabled bool
		name    string
	}{{c.Dev, "--dev"}, {c.Managed, "--managed"}} {
		if !flag.enabled {
			continue
		}
		switch {
		case fix == "whr setup":
			fix += " " + flag.name
		case strings.HasPrefix(fix, "whr setup host "):
			fix = strings.Replace(fix, "whr setup host ", "whr setup host "+flag.name+" ", 1)
		default:
			fix = strings.Replace(fix, "whr setup ", "whr setup "+flag.name+" ", 1)
		}
	}
	if c.Prefix != "" {
		fix += " --prefix " + shellArgument(c.Prefix)
	}
	if c.Account != "" && c.Account != doctor.WhrUser {
		fix += " --user " + shellArgument(c.Account)
	}
	// a fix of the user phase runs as whr's account, whatever check names it; a
	// `whr setup host` fix is the administrator's
	if c.RunAs != "" && !strings.HasPrefix(fix, "whr setup host") {
		fix += " (run as " + c.RunAs + ")"
	}
	return fix
}
