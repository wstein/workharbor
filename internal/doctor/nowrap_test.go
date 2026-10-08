package doctor

import (
	"context"
	"regexp"
	"testing"
)

// A command written between backticks in a step's prose wraps at 80 columns,
// and a wrapped command cannot be copied (issue #400). A command with
// arguments belongs in Fix.Try (or Unreachable.Command), which the wizard
// prints on a line of its own.
var embeddedCommand = regexp.MustCompile("`[^`\\s]+\\s[^`]*`")

func noWrappedCommand(t *testing.T, step string, f *Fix) {
	t.Helper()
	if f == nil {
		return
	}
	for field, text := range map[string]string{"Guide": f.Guide, "Desc": f.Desc} {
		if m := embeddedCommand.FindString(text); m != "" {
			t.Errorf("step %s: %s embeds the command %s in prose, where it wraps at 80 columns; move it to Fix.Try", step, field, m)
		}
	}
}

func TestNoStepTextEmbedsACommandThatWraps(t *testing.T) {
	for _, dev := range []bool{false, true} {
		for _, user := range []string{"workharbor", "werner"} {
			d := hostDeps(scripted{})
			d.Dev, d.User = dev, user
			for _, c := range Checks(d) {
				noWrappedCommand(t, c.Name, c.Fix)
				if c.Reach == nil {
					continue
				}
				if u := c.Reach(context.Background()); u != nil {
					for field, text := range map[string]string{"Why": u.Why, "Where": u.Where} {
						if m := embeddedCommand.FindString(text); m != "" {
							t.Errorf("step %s: Unreachable.%s embeds the command %s in prose; use Command", c.Name, field, m)
						}
					}
				}
			}
		}
	}
}

func TestTheScanMatchesACommandWithArgumentsOnly(t *testing.T) {
	if !embeddedCommand.MatchString("run `whr setup --only a` now") {
		t.Error("a command with arguments is not found")
	}
	if embeddedCommand.MatchString("the `whr` command and `--local`") {
		t.Error("a single word is not a command that wraps")
	}
}
