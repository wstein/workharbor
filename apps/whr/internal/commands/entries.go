// SPDX-License-Identifier: EUPL-1.2

package commands

// entries is the Go mirror of contracts/cli/commands.json. Changing it needs
// the same change in the contract, its freeze test and an owner-reviewed
// amendment (ADR 0014, FR-052).
var entries = []Entry{
	{ID: "workspace.promote", Path: []string{"workspace", "promote"}, Increment: "first", Needs: "none", Writes: "local", JSON: "envelope", Flags: []string{"--local", "--dry-run", "--yes", "--json"}},
	{ID: "workspace.stage", Path: []string{"workspace", "stage"}, Increment: "first", Needs: "none", Writes: "local", JSON: "envelope", Flags: []string{"--local", "--source", "--smoke", "--dry-run", "--trust", "--yes", "--json"}},
	{ID: "workspace.drift", Path: []string{"workspace", "drift"}, Increment: "first", Needs: "none", Writes: "none", JSON: "envelope", Flags: []string{"--local", "--source", "--json"}},
	{ID: "workspace.home.clone", Path: []string{"workspace", "home", "clone"}, Increment: "first", Needs: "none", Writes: "local", JSON: "envelope", Flags: []string{"--local", "--to", "--exclude", "--include-credentials", "--dry-run", "--trust", "--yes", "--json"}},
	{ID: "workspace.home.backup", Path: []string{"workspace", "home", "backup"}, Increment: "first", Needs: "none", Writes: "local", JSON: "envelope", Flags: []string{"--local", "--to", "--dry-run", "--trust", "--yes", "--json"}},
	{ID: "workspace.recover", Path: []string{"workspace", "recover"}, Increment: "first", Needs: "none", Writes: "local", JSON: "rejected", Flags: []string{"--local", "--toolbox", "--nixbox", "--rw", "--net", "--dry-run"}},
	{ID: "workspace.ls", Path: []string{"workspace", "ls"}, Short: []string{"ls"}, Increment: "later", Needs: "control_plane", Writes: "none", JSON: "envelope", BlockedOn: "owner_api", Flags: []string{"--local", "--role", "--label", "--json"}},
	{ID: "workspace.show", Path: []string{"workspace", "show"}, Short: []string{"show"}, Increment: "later", Needs: "control_plane", Writes: "none", JSON: "envelope", BlockedOn: "owner_api"},
	{ID: "workspace.create", Path: []string{"workspace", "create"}, Short: []string{"create"}, Increment: "later", Needs: "control_plane", Writes: "remote", JSON: "envelope", BlockedOn: "owner_api", Flags: []string{"--cpus", "--memory"}},
	{ID: "workspace.start", Path: []string{"workspace", "start"}, Short: []string{"start"}, Increment: "later", Needs: "control_plane", Writes: "remote", JSON: "envelope", BlockedOn: "owner_api"},
	{ID: "workspace.stop", Path: []string{"workspace", "stop"}, Short: []string{"stop"}, Increment: "later", Needs: "control_plane", Writes: "remote", JSON: "envelope", BlockedOn: "owner_api"},
	{ID: "workspace.rebuild", Path: []string{"workspace", "rebuild"}, Short: []string{"rebuild"}, Increment: "later", Needs: "control_plane", Writes: "remote", JSON: "envelope", BlockedOn: "owner_api"},
	{ID: "workspace.rm", Path: []string{"workspace", "rm"}, Short: []string{"rm"}, Increment: "later", Needs: "control_plane", Writes: "remote", JSON: "envelope", BlockedOn: "owner_api", Flags: []string{"--local", "--role", "--label", "--volumes", "--force", "--dry-run", "--yes", "--json"}},
	{ID: "workspace.shell", Path: []string{"workspace", "shell"}, Short: []string{"shell"}, Increment: "later", Needs: "control_plane", Writes: "none", JSON: "events", BlockedOn: "owner_api", Flags: []string{"--local"}},
	{ID: "workspace.console", Path: []string{"workspace", "console"}, Short: []string{"console"}, Increment: "later", Needs: "control_plane", Writes: "none", JSON: "events", BlockedOn: "owner_api", Flags: []string{"--local"}},
	{ID: "workspace.ssh", Path: []string{"workspace", "ssh"}, Short: []string{"ssh"}, Increment: "later", Needs: "control_plane", Writes: "none", JSON: "rejected", BlockedOn: "owner_api"},
	{ID: "workspace.open", Path: []string{"workspace", "open"}, Short: []string{"open"}, Increment: "later", Needs: "control_plane", Writes: "none", JSON: "envelope", BlockedOn: "owner_api"},
	{ID: "workspace.init", Path: []string{"workspace", "init"}, Short: []string{"init"}, Increment: "first", Needs: "none", Writes: "local", JSON: "envelope", Flags: []string{"--name", "--project-url", "--start-dir", "--devcontainer", "--template", "--human-shell", "--console-mode", "--shortcuts", "--non-interactive", "--yes", "--dry-run", "--overwrite", "--no-agents-block", "--helper-base-image", "--json"}},
	{ID: "workspace.validate", Path: []string{"workspace", "validate"}, Short: []string{"validate"}, Increment: "first", Needs: "none", Writes: "none", JSON: "envelope"},
	{ID: "host.setup", Path: []string{"host", "setup"}, Short: []string{"setup"}, Increment: "later", Needs: "host_local", Writes: "host", JSON: "envelope", BlockedOn: "host_setup_wi"},
	{ID: "host.doctor", Path: []string{"host", "doctor"}, Short: []string{"doctor"}, Increment: "first", Needs: "none", Writes: "none", JSON: "envelope"},
	{ID: "host.offboard", Path: []string{"host", "offboard"}, Short: []string{"offboard"}, Increment: "later", Needs: "host_local", Writes: "host", JSON: "envelope", BlockedOn: "host_setup_wi"},
	{ID: "host.service.install", Path: []string{"host", "service", "install"}, Short: []string{"service", "install"}, Increment: "later", Needs: "host_local", Writes: "host", JSON: "envelope", BlockedOn: "host_setup_wi"},
	{ID: "host.service.uninstall", Path: []string{"host", "service", "uninstall"}, Short: []string{"service", "uninstall"}, Increment: "later", Needs: "host_local", Writes: "host", JSON: "envelope", BlockedOn: "host_setup_wi"},
	{ID: "host.service.status", Path: []string{"host", "service", "status"}, Short: []string{"service", "status"}, Increment: "later", Needs: "host_local", Writes: "none", JSON: "envelope", BlockedOn: "host_setup_wi"},
	{ID: "host.ls", Path: []string{"host", "ls"}, Increment: "later", Needs: "control_plane", Writes: "none", JSON: "envelope", BlockedOn: "owner_api"},
	{ID: "host.show", Path: []string{"host", "show"}, Increment: "later", Needs: "control_plane", Writes: "none", JSON: "envelope", BlockedOn: "owner_client"},
	{ID: "auth.login", Path: []string{"auth", "login"}, Short: []string{"login"}, Increment: "later", Needs: "control_plane", Writes: "local", JSON: "envelope", BlockedOn: "owner_client"},
	{ID: "auth.status", Path: []string{"auth", "status"}, Increment: "later", Needs: "control_plane", Writes: "none", JSON: "envelope", BlockedOn: "owner_client"},
	{ID: "auth.logout", Path: []string{"auth", "logout"}, Short: []string{"logout"}, Increment: "later", Needs: "control_plane", Writes: "local", JSON: "envelope", BlockedOn: "owner_client"},
	{ID: "op.show", Path: []string{"op", "show"}, Increment: "later", Needs: "control_plane", Writes: "none", JSON: "envelope", BlockedOn: "owner_api"},
	{ID: "op.wait", Path: []string{"op", "wait"}, Increment: "later", Needs: "control_plane", Writes: "none", JSON: "envelope", BlockedOn: "owner_api"},
	{ID: "agent.ls", Path: []string{"agent", "ls"}, Increment: "later", Needs: "control_plane", Writes: "none", JSON: "envelope", BlockedOn: "owner_api"},
	{ID: "agent.attach", Path: []string{"agent", "attach"}, Increment: "later", Needs: "control_plane", Writes: "none", JSON: "events", BlockedOn: "owner_api"},
	{ID: "agent.status", Path: []string{"agent", "status"}, Increment: "later", Needs: "control_plane", Writes: "none", JSON: "envelope", BlockedOn: "owner_api"},
	{ID: "agent.send", Path: []string{"agent", "send"}, Increment: "later", Needs: "control_plane", Writes: "remote", JSON: "envelope", BlockedOn: "owner_api"},
	{ID: "agent.stop", Path: []string{"agent", "stop"}, Increment: "later", Needs: "control_plane", Writes: "remote", JSON: "envelope", BlockedOn: "owner_api"},
	{ID: "version", Path: []string{"version"}, Increment: "first", Needs: "none", Writes: "none", JSON: "envelope"},
	{ID: "completion", Path: []string{"completion"}, Increment: "first", Needs: "none", Writes: "none", JSON: "rejected"},
	{ID: "help", Path: []string{"help"}, Increment: "first", Needs: "none", Writes: "none", JSON: "rejected"},
}
