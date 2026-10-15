# BundleDock shared shell layer in workspaces

Status: design proposal for [issue #81](https://github.com/wstein/workharbor/issues/81),
2026-10-15. Not an accepted trust decision, implementation plan, runtime integration
or permission to execute a bundle. The owner must review the proposed policy below.

## Scope and prerequisites

[ADR 0004](../adr/0004-bundles.md) separates bundle resolution from attachment.
[ADR 0009](../adr/0009-in-container-ai-agents.md) puts agents inside the workspace;
[ADR 0012](../adr/0012-persistent-workspace-console.md) makes the owner's console
persistent. A shared user and HOME therefore cannot imply owner-only prompt
configuration. Workspace trust for hooks does not silently approve later
agent-written prompt commands.

Issue #81 calls this future bundle integration "feature 002". That identifier is
already used by [Symphony documentation smoke](../../specs/002-symphony-doc-smoke/spec.md).
Allocate a distinct feature identifier before specifying BundleDock integration;
do not repurpose that directory or change completed task history. Current `main`
has no product attachment implementation. The installable pilot does not supply
an approved BundleDock attachment contract either.

The future feature needs a reviewed spec, plan, tasks and bounded implementation
scope. It must choose the attachment method, supported image/shell matrix,
immutable artifact and prompt policy. This paper supplies review input without
inventing approved tasks or changing the accepted bundle ports.

## Upstream layout and compatibility

The inspected source baseline is BundleDock
[`0c8f17a65223b04829af9a03a78811120069441b`](https://github.com/wstein/bundledock/tree/0c8f17a65223b04829af9a03a78811120069441b).
Its [toolbox documentation](https://github.com/wstein/bundledock/blob/0c8f17a65223b04829af9a03a78811120069441b/docs/toolbox.md#shared-shell-layer)
and [generator](https://github.com/wstein/bundledock/blob/0c8f17a65223b04829af9a03a78811120069441b/internal/shelllayer/shelllayer.go)
define these interactive entry points:

| Shell | Path relative to toolbox | Consumer loading responsibility |
| --- | --- | --- |
| bash | `share/shell/init.bash` | Guarded source in the image's system bashrc |
| zsh | `share/shell/init.zsh` | Guarded source in the image's system zshrc |
| fish | `share/fish/vendor_conf.d/bdock-shell.fish` | Vendor autoload through `XDG_DATA_DIRS` |
| Nushell | `share/nushell/vendor/autoload/bdock-shell.nu` | Vendor autoload through `XDG_DATA_DIRS` |

The consumer image supplies zsh. Fish, Nushell and Starship may come from the
toolbox, but image tools keep priority. These files are executable startup
content: a read-only mount prevents later modification of the mount, not harmful
code in the original artifact. Shortcuts execute commands on explicit use;
loading their definitions is still an owner-trusted code execution step.

Source presence does not prove that an older published carrier contains the
layer. Before attachment, resolve an immutable digest for the selected platform,
verify the required files and integrity, and record artifact, source provenance,
platform, attachment method and resolved paths. Reject missing or incompatible
layout before provisioning. Do not invent a BundleDock manifest API, build or
publish a carrier as part of WorkHarbor attachment, or substitute a mutable tag.
The final digest and compatibility evidence remain future feature inputs.

## Proposed prompt policy: default deny

The proposed default is shortcuts with BundleDock-managed Starship initialization
disabled whenever agents can write HOME, any effective prompt configuration, or
those paths' parent directories. Unknown control is treated as writable. This
includes `XDG_CONFIG_HOME`, `STARSHIP_CONFIG` and relevant shell startup files;
a different HOME string, a current ownership bit or a read-only toolbox alone
does not prove that agents cannot replace the prompt's inputs.

Before starting any shell or exposing the toolbox's share directory for vendor
autoload, the trusted workspace launch path must set and export `BDOCK_STARSHIP=0`
for this default. Do not rely on a bash/zsh rc assignment to protect fish/Nushell:
their vendor files may run first. Do not preserve an inherited `BDOCK_STARSHIP=1`
as owner approval. All supported launch and console reattachment paths must apply
the same policy before a shell starts. Existing connections do not gain trust
when a new one attaches.

This is a policy for WorkHarbor's initialization, not isolation from an agent
running as the same user. Such an agent can edit user rc files, reset environment
variables, install its own prompt hook or affect commands run by the owner.
`BDOCK_STARSHIP=0` also does not remove an already installed user prompt. Until the
owner explicitly accepts that shared trust domain or an enforced separation is
designed, WorkHarbor must not enable its prompt merely because a console belongs
to the owner. An image with an incompatible automatic prompt/startup path must
fail capability validation rather than be reported as protected. A guarantee
against malicious same-user code requires a different isolation design.

A future owner-approved prompt mode must identify who controls the entire
effective configuration and startup chain, how agents are denied writes, how
that property survives reconnect/rebuild, and how loss of it disables prompt
initialization. Alternatively the owner may explicitly accept agent-written
prompt commands as one trust domain. Neither choice is approved by this paper.

## Proposed attachment and loading behavior

1. Validate workspace ownership, revision-scoped trust, artifact integrity,
   platform and provider capabilities before side effects. The provider attacher
   materializes the selected toolbox read-only at `/opt/bundledock/toolbox`, or a
   separately approved fixed absolute path. Writable HOME, caches and project
   storage stay outside it. No host socket or host credentials enter the workspace.
2. Construct the environment in the trusted launch path. Append the toolbox's
   `bin` once, last on `PATH`; first remove all exact occurrences of that toolbox
   entry, preserving every other entry and its order. Likewise remove all exact
   toolbox `share` occurrences from `XDG_DATA_DIRS`, then prepend one only when
   the shell layer is enabled, retaining other entries and their order. With no existing XDG
   data directories, retain `/usr/local/share:/usr/share`. Reattachment, nested
   shells and repeated initialization must not duplicate either toolbox entry.
   Empty/unset PATH semantics need explicit supported-image validation; never
   introduce an empty entry that searches the working directory.
3. Apply the prompt policy before vendor autoload becomes reachable. Source
   bash/zsh only from their respective interactive system rc files, with a
   readable-file guard for the selected absolute path, for example
   `[ -r /opt/bundledock/toolbox/share/shell/init.bash ] && . /opt/bundledock/toolbox/share/shell/init.bash`.
   Use the zsh path in zshrc. Do not source shell-specific files from POSIX `sh`
   or rely on noninteractive shells reading these rc files. Validate the actual
   image rc paths and startup order; distro filenames are not interchangeable.
4. With no attachment, guarded startup is harmless and adds no missing toolbox
   paths. Removal or failed attachment must not leave stale paths, source hooks
   or prompt state in a newly started console, modify the language image, or
   delete project data. Existing persistent shells require an explicit close
   and new session for environment changes; reconnection alone retains them.

WorkHarbor owns attachment validation, launch environment and image integration.
BundleDock owns the shortcut table and generated shell behavior. Fix shared
upstream behavior there rather than maintaining four diverging copies here.

## Opt-outs and upstream limitations

`BDOCK_STARSHIP=0` disables BundleDock prompt setup. `BDOCK_ZSH_EXPAND=0` keeps
zsh shortcuts but disables expansion on space. `BDOCK_SHORTCUTS=0` suppresses
bash/zsh/fish shortcuts at startup, but the inspected Nushell definitions cannot
be conditional. For a complete shortcuts opt-out, leave toolbox `share` out of
`XDG_DATA_DIRS` and skip bash/zsh sourcing; toolbox binaries can remain available.
This also disables automatic BundleDock prompt setup in that mode. The future
feature must document this combined effect rather than claim uniform Nushell
support for `BDOCK_SHORTCUTS=0`. Opt-outs never override the prompt deny policy.
Changing settings does not unload aliases or hooks from an existing shell; apply
them to a new session and make that restart requirement visible.

The inspected source uses exported `STARSHIP_SHELL` as an initialization guard.
A child shell inherits that marker without inheriting the parent's prompt hooks;
therefore it can incorrectly skip its own initialization. Before any approved
prompt mode is accepted, require an upstream fix and behavioral evidence for all
four shells on the exact selected artifact. No prior source-only fix, unrelated
compiler repair or historical end-to-end claim proves the carrier has that fix.

## Required future acceptance

These are proposed acceptance cases, not tests executed by this documentation
change. Record exact image, platform, artifact digest, source revisions, shell
versions, commands and results on an authorized native Linux Docker host.

| Case | Required observable result |
| --- | --- |
| No toolbox; sh/bash/zsh startup | Normal startup; no missing-file error or added toolbox path; no shell-specific syntax evaluated by sh |
| Read-only attachment | Writes to the mount fail; files and commands remain readable/executable; HOME/cache/project writes succeed outside it |
| Image and toolbox command name collision | Image command wins; PATH has one toolbox bin entry at the end |
| XDG unset, customized, repeated/nested startup | Default or existing data directories retained; one toolbox share entry first when opted in |
| Each of bash/zsh/fish/Nushell, interactive and noninteractive | Actual vendor/system rc loading matches the matrix; no shortcuts or BundleDock prompt setup in noninteractive execution |
| Agent-writable/unknown HOME with inherited prompt enablement | Default deny is applied before autoload; a harmless custom-module sentinel is never executed by BundleDock initialization or prompt rendering |
| Prompt configuration redirected through XDG/STARSHIP_CONFIG | Same deny result; writable redirects do not create an owner-controlled exception |
| Each opt-out, including complete Nushell shortcuts opt-out | Documented definitions/hooks absent in a new session; existing-session restart requirement demonstrated |
| Approved prompt mode, four shells | Nested same-shell and cross-shell launches with inherited `STARSHIP_SHELL` initialize the current shell once; repeated sourcing does not duplicate hooks; an already active user prompt is preserved |
| Explicit shortcut use | `gst`, `gpsup` and representative jj shortcuts produce expected argv in all four shells; detached HEAD refuses branch-dependent actions before Git mutation; use local stubs/remotes, no real push |
| Stop/start, rebuild, console close/new session and reattach | Attachment and environment follow effective revisions; reconnect preserves the existing session and does not pretend settings were reapplied |
| Missing files, wrong platform, untrusted bundle or unsupported mount/startup policy | Validation fails before provisioning with a redacted actionable diagnostic; project data survives |

Repository checks validate this proposal's links and repository consistency.
They cannot establish any of these runtime results. Feature completion and issue
closure need the approved scope and its matching evidence; this paper alone does
not complete runtime integration or authorize activation.
