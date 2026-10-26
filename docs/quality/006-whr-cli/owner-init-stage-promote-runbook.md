# Owner host run: init, stage, promote and disposal

Status: planned acceptance procedure, with no host results recorded. Run after
#272's CLI and the separately authored throwaway-cleanup implementation are
accepted and merged. This procedure uses an already installed reviewed `whr`
binary. Building, installing, tagging and publishing are separate owner actions.
Repeat on Linux Docker and macOS with OrbStack; retain exact revisions.

## 1. Record the environment and inventory

Use disposable repositories and a disposable primary chosen by the owner.
Record OS/architecture, terminal, shell, Git, Docker client/server, Dev Container
CLI, Node and OrbStack versions, Docker endpoint and `whr --version`. Pause source
writers during execution as the previews require. Record implementation, review
and CI links, including the CLI and cleanup merge SHAs.

Choose owner-supplied variables:

- `WHR_SOURCE`: the host source directory matching the primary.
- `WHR_PRIMARY`: the exact primary container **name**, recommended over an ID.
  Promotion recreates it and changes its full ID; record that new ID afterward.
- `WHR_NAME` and `WHR_PROJECT_URL`: valid init inputs.
- `WHR_PRIMARY_HOME`: the original primary home volume name.
- `WHR_HOME_PATH`: that volume's container mount destination, checked with
  `docker inspect "$WHR_PRIMARY"` before writing the marker below.

Use the default private state location with physical, nonsymlink path components.
No host run is performed by this document. In a POSIX shell, create an evidence
folder and helpers. They save output rather than print untrusted remote text:

```sh
WHR_EVIDENCE=$(mktemp -d)
record() {
  label=$1
  shift
  if "$@" > "$WHR_EVIDENCE/$label.stdout" 2> "$WHR_EVIDENCE/$label.stderr"; then
    result=0
  else
    result=$?
  fi
  printf '%s\n' "$result" > "$WHR_EVIDENCE/$label.exit"
}
inventory() {
  label=$1
  docker ps -a --no-trunc --format '{{.ID}} {{.Names}} {{.Image}} {{.Status}}' \
    > "$WHR_EVIDENCE/$label.containers"
  docker volume ls --format '{{.Name}} {{.Driver}}' \
    > "$WHR_EVIDENCE/$label.volumes"
  whr ls --local --json > "$WHR_EVIDENCE/$label.workspaces.json"
  docker volume ls -q --filter name=anchorage-home- | sort \
    > "$WHR_EVIDENCE/$label.protected-homes"
  docker inspect --format '{{range .Mounts}}{{println .Name .Destination}}{{end}}' \
    "$WHR_PRIMARY" > "$WHR_EVIDENCE/$label.primary-mounts"
}
inventory before-init
```

Check every inventory command's exit status; a failed inventory blocks comparison.
`record` deliberately preserves the command status in a file and returns normally:
check that file against the expectations below before continuing. Do not use it
as proof of success by itself. Keep raw evidence private and sanitize it before
sharing; do not attach credentials or tokens.

Before stage, place a marker in the disposable original home, as its normal user:

```sh
docker exec "$WHR_PRIMARY" sh -c \
  'printf "%s\n" "whr owner acceptance marker" > "$1/.whr-acceptance-marker" && cksum "$1/.whr-acceptance-marker"' \
  sh "$WHR_HOME_PATH" > "$WHR_EVIDENCE/before-stage.home-marker"
```

Require exit 0. After removal, promotion and each failure scenario, run the same
`cksum` command without its `printf` write and compare it with this saved file.
Also compare the primary mount listing and protected-home list with their
before-stage files. The original home name and marker checksum must be unchanged.
The primary container ID may change after promotion; its name/home binding must
still match. Inspect raw data only with appropriate terminal escaping.

## 2. Check targets before confirmed mutations

- Never pass the primary to `whr rm`.
- Never use `--volumes --force` in this procedure.
- Never use `docker rm`, `docker volume rm` or any prune command against the
  primary, its home or `anchorage-home-*` resources.
- Never delete private candidate records manually to make a run appear clean.

Before every `--yes` step, inspect its corresponding dry-run JSON. Candidate
**disposal** targets must name only `whr-anchorage-staging`, its fresh
`whr-stage-home-*` volume and its candidate-only private paths. They must never
name the primary or an `anchorage-home-*` volume as disposal targets. Promotion
separately previews the contracted primary replacement using its original home;
that is not candidate disposal. Stop on an unexpected target. Init previews
must be confined to the intended disposable checkout.

If the owner wants candidate home data saved, back it up beforehand with Docker
tools, without deleting or changing the primary/home. There is no keep flag or
built-in backup. Normal removal confirmation covers losing unfetched clone work.

## 3. Init: flags and wizard

Start with an admitted existing devcontainer. For multiple choices, add the
selected repository-relative `--devcontainer` path consistently to preview and
apply. These commands are future steps, not observed transcripts:

```sh
record init-preview whr workspace init "$WHR_SOURCE" --name "$WHR_NAME" --project-url "$WHR_PROJECT_URL" --non-interactive --dry-run --json
record init-apply whr workspace init "$WHR_SOURCE" --name "$WHR_NAME" --project-url "$WHR_PROJECT_URL" --non-interactive --yes --json
record init-validate whr workspace validate "$WHR_SOURCE" --json
record init-rerun whr workspace init "$WHR_SOURCE" --non-interactive --yes --json
record doctor whr host doctor --json
inventory after-init
```

Dry-run preserves the tree. Validation must have no error; rerun must preserve the
result. Existing files require explicit per-path overwrite consent; no overwrite
flags are added automatically. Unless `--no-agents-block` is chosen consistently,
init previews/writes the default agent workflow block in `AGENTS.md` (FR-064).

In an equivalent disposable checkout, run `whr workspace init` on a real terminal,
enter equivalent settings, review and confirm. Compare generated files byte for
byte, **including `AGENTS.md`**, with the flag-driven result. Record answers and
the comparison. Cancel a wizard run and decline its per-path overwrite prompt; record exit 72
and unchanged files. Check selected devcontainer bytes and modes
before/after reuse. Record any unexpected exit as a failure, not a passed case.

## 4. Stage with identical preview/apply flags

Choose Docker healthcheck verification when declared. Otherwise choose explicit
workspace smoke. The existing Anchorage smoke script is fed over stdin; do not
run the host smoke orchestrator or change Anchorage files. For healthcheck:

```sh
inventory before-stage
record stage-preview whr workspace stage --local "$WHR_PRIMARY" --source "$WHR_SOURCE" --dry-run --json
```

For smoke, use this preview **instead**, before copying its fingerprint:

```sh
record stage-preview whr workspace stage --local "$WHR_PRIMARY" --source "$WHR_SOURCE" --smoke --dry-run --json
```

Review hooks, mounts, image, home, pause-writers condition and fingerprint limits.
Copy that exact preview's fingerprint into `WHR_FINGERPRINT`. Trust and mutation
confirmation are separate. Apply with the **same flags**, including smoke choice:

```sh
record stage-apply whr workspace stage --local "$WHR_PRIMARY" --source "$WHR_SOURCE" --trust "$WHR_FINGERPRINT" --yes --json
```

For smoke, use this apply **instead**:

```sh
record stage-apply whr workspace stage --local "$WHR_PRIMARY" --source "$WHR_SOURCE" --smoke --trust "$WHR_FINGERPRINT" --yes --json
```

```sh
inventory after-stage
```

Record the candidate's full ID/name as `WHR_CANDIDATE`, fresh home name, and all
candidate-only private clone/result/receipt/journal paths from the reports.
Successful stage retains a usable candidate. Check writable normal Git history,
project hooks and verification status. An unverified candidate is not a verified
promotion result. Never reuse a fingerprint from different flags or changed source.

## 5. Remove, then freshly stage and promote

Verify `WHR_CANDIDATE` is exactly the staged candidate, then:

```sh
record remove-preview whr rm --local "$WHR_CANDIDATE" --dry-run --json
record remove-apply whr rm --local "$WHR_CANDIDATE" --yes --json
inventory after-remove
```

Run the residual checks below. To create the promotion candidate, repeat section
4 in full: inventory, fresh dry-run with the chosen healthcheck/smoke flags, review,
copy its **fresh** fingerprint, then matching apply. Record the new candidate,
home and private paths; do not reuse the removed candidate's receipt or fingerprint.
Review the candidate commits/results, then:

```sh
record promote-preview whr workspace promote --local "$WHR_PRIMARY" --dry-run --json
record promote-apply whr workspace promote --local "$WHR_PRIMARY" --yes --json
inventory after-promote
```

Promote selects the primary. It imports the result refs/OIDs before deleting the
candidate clone; record those imported refs in the original repository. Compare
the original-home marker and mount binding, and run residual checks. Record the
new primary ID from `whr ls --local --json`. Replacement ends existing shell/exec
panes; trusted hooks may write the original workspace, as previewed.

## 6. Expectations, negative cases and residuals

The JSON envelope's `result` contains command-specific fields. Check the actual
schema for the reviewed binary, not invented sample output.

| Step | Expected exit and evidence |
| --- | --- |
| Init dry-run/apply/rerun/validate | 0; valid JSON, proposed/generated paths confined to source; preview changes nothing; validation has no error; rerun leaves bytes, modes and mtimes unchanged. |
| Stage preview | 0; `result.status` is `preview`, fingerprint present, candidate name `whr-anchorage-staging`, home `whr-stage-home-<id>`, disclosed verification policy; no new artifacts. |
| Verified stage | 0; `result.status` is `staged_verified`, candidate/home present, `verification.status` is `passed`; unverified output does not pass this scenario. |
| Remove/promote dry-run | 0; preview identifies only the contracted effects; no mutation. |
| Remove/promote apply | 0; candidate disposal complete, original home preserved; promote result refs present and replacement uses original home. |
| Unattended mutation without consent/trust | 70; reason identifies missing consent/trust, no new artifacts. |
| Wrong stage fingerprint | 68; fingerprint mismatch reason, no new artifacts. |
| Incomplete cleanup or uncertain recovery | 73; original cause, effects, residual inventory and retained records reported; never count as success. |
| Doctor | Record actual exit/diagnostics; platform-specific SC-019 evidence passes only when its required checks pass. |

Before a candidate exists, run these refusal cases with inventories before and
after **each** command. `WRONG_FINGERPRINT` is a valid-length lowercase hexadecimal digest different
from the fresh preview, not the actual digest:

```sh
record no-init-consent whr workspace init "$WHR_SOURCE" --non-interactive --json
record no-stage-trust whr workspace stage --local "$WHR_PRIMARY" --source "$WHR_SOURCE" --yes --json
record no-stage-consent whr workspace stage --local "$WHR_PRIMARY" --source "$WHR_SOURCE" --trust "$WHR_FINGERPRINT" --json
record wrong-stage-trust whr workspace stage --local "$WHR_PRIMARY" --source "$WHR_SOURCE" --trust "$WRONG_FINGERPRINT" --yes --json
```

Use the chosen `--smoke` flag consistently for stage refusals too. While a verified
candidate exists, run missing-consent checks (expect 70 and unchanged inventory):

```sh
record no-promote-consent whr workspace promote --local "$WHR_PRIMARY" --json
record no-remove-consent whr rm --local "$WHR_CANDIDATE" --json
```

Also decline the terminal confirmation (`n`) for stage/remove/promote: expect 70,
the refusal reason, and unchanged inventory. A JSON invocation never prompts.

After removal, successful promotion and each failure/rollback run, save:

```sh
docker ps -aq --filter 'name=^/whr-anchorage-staging$' \
  > "$WHR_EVIDENCE/residual.containers"
docker volume ls -q --filter name=whr-stage-home- \
  > "$WHR_EVIDENCE/residual.homes"
test ! -s "$WHR_EVIDENCE/residual.containers"
test ! -s "$WHR_EVIDENCE/residual.homes"
```

Use scenario-specific filenames for repeated checks. Require exit 0 and empty
files after complete disposal. For **each** recorded candidate-only clone,
result tree, receipt and journal path, record `test ! -e "$WHR_PRIVATE_PATH"` and
require 0 after complete disposal. A path that was not recorded is missing
evidence. Compare protected-home lists with `diff`, confirm the primary's original
home name/mount is unchanged, and compare its marker checksum. Never delete an
unexpected residual to make a check pass.

For failed stage (including `staged_unverified`) and failed promote, record the
original cause, effects and recovery instructions. Exercise failures only in the
owner-selected disposable scope; hermetic fault fixtures remain source evidence.
Verified rollback restores the original and disposes of failed replacement and
candidate artifacts. Uncertain recovery reports actual retained inventory.
Incomplete cleanup retains receipts/journals **last**, as reported residuals.
Record a normal confirmed remove retry against the receipt-bound candidate and
its final inventory; do not manually delete records.

## 7. Evidence template: one copy per host and scenario

```text
Host: Linux Docker | macOS OrbStack
OS / architecture:
Terminal / shell / Git:
Docker client / server / endpoint:
OrbStack (or not applicable):
Dev Container CLI / Node:
whr --version / binary SHA-256 / source revision:
CLI merge SHA / cleanup merge SHA / review links / CI links:
Primary name / old ID / new ID / original home / mount path:

Scenario:
Command and supplied flags (no secrets):
Expected exit / actual exit / refusal reason if applicable:
stdout JSON file / stderr file:
Inventory before files / after files / differences:
Original-home marker before / after / comparison:
Candidate name / ID / home / recorded private paths:
Result refs / OIDs (promotion):
Residual checks / retained records / remediation:
PASS | FAIL | BLOCKED:
Reason / deviations / unexecuted checks:
```

The [init audit](init-handoff-audit-20261026.md) keeps remaining SC-017 through
SC-020 evidence and T034 separate. Completion, real shell-completion transcripts,
offline whole-increment evidence and Feature 010 pane gates need separate
records and approval; this runbook ticks none of them.
