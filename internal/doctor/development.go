package doctor

import (
	"context"
	"errors"
	"io/fs"
	"strings"

	"github.com/wstein/workharbor/internal/config"
)

// developmentModeCheck is the shared check that keeps a remembered development
// installation visible (D24, issue #276, D49): `warn` on every run while the
// configuration holds development_prefix, naming the key, the file and the way
// out; `fail` when the key is refused (a bad value, a file that is not the
// account's own, a whr run from a managed prefix). It reads the key from the
// configuration file alone, never from the environment.
func (d Deps) developmentModeCheck() func(context.Context) (Status, string) {
	return func(context.Context) (Status, string) {
		prefix, err := config.ReadDevelopmentPrefix(d.ConfigPath)
		switch {
		case err != nil:
			return Fail, problems(err) + ": the managed setup removes the key"
		case prefix == "":
			return OK, "no " + config.DevelopmentPrefixKey + " in " + d.ConfigPath + ": a managed installation"
		case d.Whr != "" && config.UnderManagedPrefix(d.Whr):
			return Fail, d.Whr + " runs from a managed prefix, which refuses " + config.DevelopmentPrefixKey + " in " + d.ConfigPath + ": the managed setup removes it"
		}
		return Warn, config.DevelopmentPrefixKey + " " + prefix + " is set in " + d.ConfigPath + ": a development installation, whose user-writable supervisor lacks managed-install replacement protection; the managed setup (or deleting the key) leaves it"
	}
}

// developmentKeyStep is the user step that writes or removes the key, and the
// only code that does (D24, #276). It writes only when `whr setup` was given
// --dev (or --dev through the remembered key itself) and removes only for
// --managed; `whr doctor` and `whr serve` never reach it. Each fix shows the
// change as a diff and asks again, and --dry-run prints and writes nothing.
func (d Deps) developmentKeyStep() Check {
	const title = "the development installation remembered in the configuration (D24, issue #276)"
	prefix := d.prefix()
	return Check{
		Name: "development-key", Phase: PhaseUser, Step: 1, Title: title,
		Run: func(context.Context) (Status, string) {
			key, err := config.ReadDevelopmentPrefix(d.ConfigPath)
			switch {
			case d.Managed && err == nil && key == "":
				return OK, "no " + config.DevelopmentPrefixKey + " in " + d.ConfigPath
			case d.Managed && err != nil:
				return Fail, problems(err)
			case d.Managed:
				return Fail, config.DevelopmentPrefixKey + " " + key + " is set in " + d.ConfigPath + ": a managed installation refuses it"
			case err != nil:
				return Fail, problems(err)
			case !d.Dev:
				return OK, "a managed call: " + config.DevelopmentPrefixKey + " is not read or changed here"
			case key == prefix:
				return OK, config.DevelopmentPrefixKey + " " + key + " is remembered in " + d.ConfigPath
			case key == "":
				return Fail, d.ConfigPath + " does not remember this development installation (" + config.DevelopmentPrefixKey + " " + prefix + ")"
			}
			return Fail, config.DevelopmentPrefixKey + " in " + d.ConfigPath + " is " + key + ", not " + prefix
		},
		Fix: d.developmentKeyFix(prefix),
	}
}

func (d Deps) developmentKeyFix(prefix string) *Fix {
	desc := "write " + config.DevelopmentPrefixKey + " " + prefix + " to " + d.ConfigPath + " as a diff, after a y, written atomically, keeping every other key"
	if d.Managed {
		desc = "remove " + config.DevelopmentPrefixKey + " from " + d.ConfigPath + " as a diff, after a y, written atomically, keeping every other key"
	}
	return &Fix{Desc: desc, Do: func(_ context.Context, p Prompter) error {
		if !d.Managed && !d.Dev {
			return errors.New("the development_prefix key is written only by the development setup")
		}
		m, err := readConfigMap(d.ConfigPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return errors.New(d.ConfigPath + " does not exist: run the config-base step first")
			}
			return err
		}
		old, err := marshalConfig(m)
		if err != nil {
			return err
		}
		// the JSON decoder matches key names without regard to case, so every spelling
		// of the key is the key: "DEVELOPMENT_PREFIX" is read as it and goes with it
		for k := range m {
			if strings.EqualFold(k, config.DevelopmentPrefixKey) {
				delete(m, k)
			}
		}
		if !d.Managed {
			if msg := config.CheckDevelopmentPrefix(prefix); msg != "" {
				return errors.New(config.DevelopmentPrefixKey + " " + prefix + " " + msg)
			}
			m[config.DevelopmentPrefixKey] = prefix
		}
		updated, err := marshalConfig(m)
		if err != nil {
			return err
		}
		// no whole-file validation here: the step runs before the GitHub App is in the
		// file, and the key is the only thing it changes (its value was checked above)
		p.Show(lineDiff(string(old), string(updated)))
		ok, err := p.Confirm("Write this change to " + d.ConfigPath)
		if err != nil || !ok {
			if err == nil {
				err = errors.New("not written")
			}
			return err
		}
		if err := replaceWithBackup(p, d.ConfigPath, append(updated, '\n')); err != nil {
			return err
		}
		if d.Managed {
			p.Show("whr removed " + config.DevelopmentPrefixKey + " from " + d.ConfigPath + ": this is a managed installation again")
		} else {
			p.Show("whr wrote " + config.DevelopmentPrefixKey + " " + prefix + " to " + d.ConfigPath + ": the doctor warns on every run while it is set, and the managed setup removes it")
		}
		return nil
	}}
}
