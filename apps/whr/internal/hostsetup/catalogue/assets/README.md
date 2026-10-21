# Host setup snapshot

These are production catalogue assets for Feature 009/T001, separate from the
synthetic `1.0` snapshots in loader tests. All thirteen entries are preselected;
each remains independently opt-out. The owner approved these initial minimums
in H40 on 2026-10-20 ("Ja, diese Startbaseline verwenden"):

| Entry | Minimum |
| --- | --- |
| orbstack | 2.2.3 |
| git | 2.56.0 |
| gh | 2.102.1 |
| jq | 1.8.2 |
| fish | 4.9.3 |
| starship | 1.27.0 |
| ripgrep | 15.2.0 |
| fd | 10.5.0 |
| fzf | 0.74.5 |
| zoxide | 0.10.0 |
| bat | 0.26.1 |
| jj | 0.46.0 |
| herdr | 0.9.5 |

These floors are an owner support decision, not a claim of testing older versions.
They do not track BundleDock. The owner's read-only observations and the separate
approved floor map remain in the workspace evidence record
`scratch/whr009-mac13-observations.json`; no runtime reads that record.
The twelve command probes use `--version` with the command identities and parser
shapes in the owner-run `scratch/whr009-mac13-readonly.pl`. Tests construct output
strings around those observed values; they do not claim captured raw stdout.

OrbStack application metadata reported `2.2.4`; Homebrew's installed cask
registration reported `2.2.3,20963`. The former is the installed application support
version; the latter is package provenance. The closed `version_metadata` value
`orbstack_app` selects the fixed `CFBundleShortVersionString` key in
`/Applications/OrbStack.app/Contents/Info.plist`; T002 must implement the read-only
route and source/path checks. A valid application version alone does not establish
Homebrew provenance or justify a repair. Catalogue loading starts nothing,
runs no probe, reads no host files and performs no network lookup. Real detection
and package-manager behavior belong to later tasks.

Only fish, starship, fzf and zoxide carry configuration templates. The fish
preferences suppress the greeting; the fzf and zoxide drop-ins initialize their
own interactive shell integrations only when the corresponding command exists.
The starship file supplies prompt preferences without assuming a terminal font.
An owner who already initializes starship retains that initialization; this
snapshot does not change the login shell or overwrite `config.fish`.
Every template is bound to its SHA-256 in the catalogue; later dotfile code must
preview, back up and confirm replacement per FR-013. No template is applied here.

Declared HTTPS URLs are read-only version lookups of Homebrew formula/cask
metadata, not an allowlist of Homebrew's own installation download hosts.
Homebrew installs may use its own registries/upstream downloads. The OrbStack
cask declares that its installer may raise an operating-system prompt; the
initial catalogue contains no privileged command.
