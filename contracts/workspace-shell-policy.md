# Workspace shell policy port (issue #81)

Status: bounded proposed executable policy; no live integration or trust approval.
[Feature 007](../specs/007-bundledock-shell-policy/spec.md) is the scope authority.

`workspaceshell.Prepare(environment, description)` returns a fresh `Plan` containing
`Environment`, `BashRC` and `ZshRC`, or a typed `Error` with stable `Code`.
The input `ToolboxDescription` is data, not authority: mount path, artifact digest,
source revision, platform, read-only observation and entry kinds. A valid result
only means that these supplied values match this policy's supported profile.
It cannot prove actual bytes, a real mount, integrity, owner approval or execution
trust. The future resolver/attacher must independently establish and record those
facts; never deserialize source-controlled fields as authorization.

Supported profile: `/opt/bundledock/toolbox`; `sha256:` plus 64 lowercase hex;
40/64 lowercase hex source revision; `linux/amd64` or `linux/arm64`; read-only;
`bin` directory and regular files `share/shell/init.bash`, `share/shell/init.zsh`,
`share/fish/vendor_conf.d/bdock-shell.fish`,
`share/nushell/vendor/autoload/bdock-shell.nu`. Missing or unsupported description
returns `unsupported_attachment`. This is a layout compatibility check, not a
filesystem check; symlink resolution/integrity belong to the future attacher.

Environment map keys must be nonempty and contain neither `=` nor NUL; values
must contain no NUL. Search entries are nonempty absolute POSIX paths with no
control characters. No filesystem normalization occurs: only exact fixed toolbox
entries are removed. Reject invalid input as `invalid_environment`; input maps
and entry maps remain untouched, including on failure.

PATH requires a nonempty image search list after stripping exact toolbox bin
occurrences. Append one fixed bin entry iff attached. Strip every exact toolbox
share entry from XDG_DATA_DIRS; missing/empty values or an empty retained list use
`/usr/local/share:/usr/share`. Prepend share iff attached and shortcuts enabled.
Retain all other entry order. Preserve unrelated environment settings.

Force `BDOCK_STARSHIP=0` unconditionally, even with no attachment or an inherited
`1`. `BDOCK_SHORTCUTS=0` leaves toolbox binaries available but omits toolbox share
and both rc fragments: Nushell's unconditional shortcut definitions never become
autoloadable through this plan. `BDOCK_ZSH_EXPAND` passes through. No prompt-enable
mode exists. Opt-outs do not unload prior user definitions or hooks.

BashRC and ZshRC contain only fixed shell-specific snippets with an interactive
check and readable-file guard. The future approved image integration installs each
in the actual respective system rc file. POSIX sh gets neither fragment. Prepare
never sources or executes content. Its returned environment must be applied before
any vendor autoload or system rc starts; setting variables in rc alone is too late.
No output fragment may override the prompt fence. User rc can change environment
and same-user processes can change startup inputs; no hostile-code isolation is
claimed. Image auto-prompt incompatibilities must be rejected by later admission.

New shell/console spawn is the adoption boundary. Reconnecting an existing
persistent console retains its environment; settings require close/new session.
T009 does not accept bundles and is unchanged. T013/T014 will review adapter design
and transport separately. No implicit process, attachment, launch or restart occurs.

Unit tests establish deterministic planning, refusals and idempotence only. Actual
read-only mounts, integrity, four-shell vendor behavior, image rc paths/order and
prompt custom-module sentinels remain future Linux integration acceptance. A fake
fixture or substituted path is surrogate evidence and cannot satisfy those gates.
