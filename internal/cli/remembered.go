package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/wstein/workharbor/internal/config"
)

// rememberedWarning is what setup and doctor print when the development
// installation comes from the configuration's development_prefix (D24, issue
// #276) and not from a flag typed on this call: the file and the way out are
// named, so the weaker mode is never silent. A managed installation takes no
// key and no `--dev`; no environment variable is read for either.
func rememberedWarning(configPath string) string {
	return developmentWarning + "\n  remembered as " + config.DevelopmentPrefixKey + " in " + configPath
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
