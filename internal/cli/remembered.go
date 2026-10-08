package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/wstein/workharbor/internal/config"
)

// rememberedWarning is what setup and doctor print when the development
// installation comes from the configuration's development_prefix (D24, issue
// #276) and not from a flag typed on this call: the file and the way out are
// named, so the weaker mode is never silent. A managed installation takes no
// key and no `--dev`; no environment variable is read for either.
func rememberedWarning(configPath string) string {
	line := "  remembered as " + config.DevelopmentPrefixKey + " in " + configPath
	if len([]rune(line)) > 80 { // a long path goes on a line of its own
		line = "  remembered as " + config.DevelopmentPrefixKey + " in\n  " + configPath
	}
	return developmentWarning + "\n" + line
}

// rememberedPrefix returns the development prefix the configuration remembers,
// or "" when this call is not governed by it: `whr setup --managed` is leaving
// development mode, and an explicit --prefix without --dev is a managed call
// that ignores the key. Otherwise the key is read from the configuration file
// only, its value and file are checked as config.ReadDevelopmentPrefix checks
// them, and a whr run from a managed prefix refuses it. A refusal is an error:
// the key is never trusted when it fails a check. The caller applies it only
// when neither --dev nor --prefix was given, because an explicit flag wins.
func rememberedPrefix(cmd *cobra.Command, dev, managed bool, configPath, exe string) (string, error) {
	if managed || (cmd.Flags().Changed("prefix") && !dev) {
		return "", nil
	}
	key, err := config.ReadDevelopmentPrefix(configPath)
	if err != nil {
		return "", fmt.Errorf("%s refuses %s; `whr setup --managed` removes it: %w", configPath, config.DevelopmentPrefixKey, err)
	}
	if key != "" && config.UnderManagedPrefix(exe) {
		return "", usageError{fmt.Sprintf("%s holds %s, but this whr (%s) runs from a managed prefix, which refuses it: run `whr setup --managed`, or delete the key", configPath, config.DevelopmentPrefixKey, exe)}
	}
	return key, nil
}

// useRemembered reports whether a call takes the remembered prefix: the key is
// set and neither --dev nor --prefix was given.
func useRemembered(cmd *cobra.Command, dev bool, key string) bool {
	return key != "" && !dev && !cmd.Flags().Changed("prefix")
}

// developerInstallHint is the sentence for a whr that looks like a developer
// installation but was not selected with --dev: the account that runs
// the supervisor owns the binary, so it could replace its own supervisor (D24,
// D49), which only a development installation accepts, and only by name. It is
// empty for anything else. The manual (installation, "development installation")
// says --dev is selected explicitly and never inferred, so setup refuses and
// doctor says so; neither carries on as if it were a managed install.
func developerInstallHint(exe string, uid int) string {
	if exe == "" || uid == 0 {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return ""
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != uid {
		return ""
	}
	return fmt.Sprintf("this looks like a developer install: %s is owned by the account that runs whr, so it could replace its own supervisor; run it with --dev (see the manual, install-upgrade-release: development installation), or install whr with an administrator account", resolved)
}
