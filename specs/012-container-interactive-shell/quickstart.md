# Feature 012 validation guide

Status: future acceptance guide; not a runnable product claim. Nothing here is available before T023. Offline checks use fakes only
(no real Docker, network, credential or host mutation).

## Offline checks (per PR)

```sh
python3 scripts/select_feature.py 012-container-interactive-shell
umask 022   # sandboxes may start at 0000
python3 scripts/check_repo.py
(cd apps/whr && go vet ./internal/runtime/... ./internal/sandbox/... && go test ./internal/runtime/... ./internal/sandbox/...)
git diff --check
git diff --stat $(git merge-base origin/main HEAD) -- contracts/cli docs/adr specs/006-whr-cli specs/008-workspace-sandbox specs/010-host-and-workspace-panes specs/011-vm-provider   # empty except the gated PRs
```

Scenario to test map: US1 `TestShellExecPlanFullIdEndpointPty`, `TestEndOfSessionTableFourRows`; US2 `TestAdmitInteractiveShellTable`;
US3 `TestShellNonTerminalExit64NoPtyRequested`, `TestShellAcceptsOnlyConfirmEndpointYesGlobal`, `TestShellMalformedContainerEndpointExit64`, `TestStderrNotTerminalNoConfirmExit70NamesConfirmFlag`; US4
`TestContainerLostPassesStatusRecordsFailed`, `TestKilledClientRowsClientStatusPassedShellStatusRecorded`; US5
`TestYesAloneStillPrompts`.

## Verification (V1 to V5)

<a id="verification-v1-to-v5"></a>
Owner-run on a real Docker host with a terminal (T002), using a disposable test container, recording versions, commands and
observed results; no session content or credentials. Check: V1 initial and later terminal size reach the in-container pty; V2 the
runtime can disable its detach key sequence and no key sequence detaches; V3 which route passes environment values without placing
them in argv (inspect the process list); V4 client behavior and exit status on SIGHUP, SIGTERM, SIGINT sent to it, for a killed
client, a stopped container and a removed container; V5 a read-only query that tells whether the exec process ended, and one for
an exec process gone or never started.

## SC-010 owner gate

<a id="sc-010-owner-gate"></a>
After T016, T017 and T023, on both real hosts, Linux with Docker Engine and macOS with OrbStack (T025; the macOS run is on the owner's Mac; plan open question 4), and record each host separately: start a shell and check its status passes
through; resize the terminal; suspend, resume and interrupt a job; stop the container from another terminal (expect the
`container_lost` row: status passed through, instance `failed`); kill the client with the shell running (expect `end_undetermined`,
exit 72); confirm no key sequence detaches; confirm no environment value in the process list; note the terminal state after the
killed client. Record product SHA, OS, architecture, Docker and terminal versions and the owner's acceptance link under
`docs/quality/012-container-interactive-shell/`. Until both hosts are recorded FR-003 keeps `interactive_shell=none`. A missing tool or host is a
blocker, not a pass.
