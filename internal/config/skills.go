package config

import "path/filepath"

func (c *Config) SkillStoreForbidden() []string {
	roots := append([]string{}, c.Roots.Workspaces...)
	if c.Roots.ToolStore != "" {
		roots = append(roots, c.Roots.ToolStore)
	}
	files := []string{c.GitHub.KeyFile, c.APITokenFile, c.AgentAPIKeyEnvFile, c.BotSigningKeyFile, c.Console.SSHCAKeyFile}
	if c.Ntfy != nil {
		files = append(files, c.Ntfy.TopicFile, c.Ntfy.TokenFile)
	}
	for _, file := range files {
		if file != "" {
			roots = append(roots, filepath.Dir(file))
		}
	}
	return roots
}
