# Development harness

## Bootstrap checks
Python 3.11+ and Bash are enough for `make check`; no product runtime or container
engine is needed. Checks validate project document links, tracked JSON, required
artifacts, pending feature status, contract shape of the workspace example, and
coarse dependency boundaries. Harness regression tests inject broken references,
unsafe paths and forbidden dependencies to prove rejection. These checks do not
replace compilation, runtime authorization tests or a security audit.

## Feature loop
Read constitution → specify/clarify → plan → tasks → analyze → implement → converge.
Use generated Codex skills (`$speckit-*`) from `.agents/skills/`. Bootstrap 001 already
has a spec/plan/tasks; do not regenerate them without reviewing intended changes.
Select local skill context with `python3 scripts/select_feature.py 001-workspace-lifecycle`.
Use `SPECIFY_FEATURE_DIRECTORY=specs/001-workspace-lifecycle SPECIFY_FEATURE_NO_PERSIST=1` on main for upstream prerequisite
scripts; create `001-workspace-lifecycle` for implementation.

Run `make check`, targeted behavior tests, then appropriate real-host/browser
acceptance. Record commands, versions, output summary, limitations and commit.
Implement product format/build/test targets in T001 before claiming product checks.
The architecture lint catches obvious prohibited references only; add compiler-backed
Elixir/Go dependency enforcement when application packages exist.

## Reproducibility and feedback
Pin dependencies and retain lockfiles. Avoid hidden developer-machine prerequisites.
Every task uses its own checkout; no shared database or test host by default. Capture
structured operation logs with correlation IDs, redact secrets and use failure-path
fixtures. Feed repeated review comments into a focused check or authoritative doc.
Review quality gaps and documentation freshness per release; do not schedule agents
or background services implicitly.

## CI
The included GitHub Actions workflow runs the owner-pinned trusted checker with
`python3 -I -B` on pushes/PRs.
It is optional infrastructure; local checks work without GitHub. Other forges can
run `make check`. Publishing a remote or enabling CI is outside this bundle.

Documentation and proposals share one CI path above accepted source
`8f8211afdda9e46984ccfb95505f83434da06db7`. Only regular non-executable Markdown
or JSON under docs, specs or contracts, and README.md, may change. Existing pinned
contracts, verifiers and task markers remain fixed. Executable, test, workflow,
product and other configuration changes require strict full-green acceptance.
The checker itself is CI authority and requires independent review; it cannot
authenticate its own approval. Index/worktree drift, untracked files, unsafe file
types/modes and inconsistent pull-request merge metadata block the docs path.

This path validates current documents and reuses the accepted baseline checks:
twelve direct state tests, 31 baseline passes and exactly ten approved CLI reds,
the exact 53-case T004 preimplementation rejection, and all static checks. Raw
`make check` must still exit 2 and is reported as red. This is documentation
validation only; it grants no task execution, implementation acceptance or owner
approval. New documentation does not require a task-specific CI exception.

The owner-approved T003 acceptance in [bootstrap-t003.md](bootstrap-t003.md) is a
temporary CI phase: twelve direct state tests must pass, and the unchanged T002
verifier must confirm all 31 baseline passes and exactly ten approved CLI assertion
failures. Repository, diff, shell syntax and feature prerequisite checks must also
pass. Raw `make check` still runs and must report exit 2; its full suite remains red,
and CI acceptance does not claim CLI implementation or full-suite acceptance.

The helper retains the exact accepted T003 identities, contracts, prerequisite
blobs and verifier matrices. Documentation validation uses a separately pinned
accepted CI policy source (`2d965c15587cdc4d9fc9a00083bd00d7788b8306`), while the
behavior baseline remains unchanged. It checks proposal changes from the merge
base through every intervening commit, including reverted changes. Independent
base documentation changes are allowed; changed protected prerequisites, runtime,
tests, Feature 001–003 artifacts or executable inputs require strict acceptance.
A proposal cannot authenticate changes to the CI helper itself.

PR source metadata binds exact base, proposal head and integration commits. The
checkout must have exactly those ordered parents and the tree produced by Git's
actual merge. Index bytes, physical file types/modes and worktree content must
match that checkout; dirty, staged, untracked, symlink and hidden mode changes
block. Executable Git configuration is refused or disabled before inspection.
Scope is checked against the proposal head; integration checks run against the
synthetic merge, which may include unrelated accepted base documentation changes.
Missing objects, stale/foreign event metadata, conflicts or uncertain identity
block without inferring rebase equivalence. The accepted T003 rebase identity
remains explicit and does not require its orphaned original commit.

An independently reviewed, explicitly host-adopted immutable policy source can
validate the bounded operator-tool category with `--policy-source SHA`. This pin
must identify the loaded checker's exact bytes and mode, and the proposal's
operator/authority files must match that reviewed source. The category enumerates
three tools and their separate `scripts/test_*.py` files; it never permits arbitrary
scripts or changes to frozen baseline behavior. All new tool suites must be
nonempty and strictly green, with no skips or expected failures. The unchanged
12 direct state tests, 31 baseline passes, ten exact known CLI assertions and 53-case
T004 rejection remain required, together with repository/shell/prerequisite checks.
Raw `make check` must still report exit 2. This is **operator validation only**:
it grants no task execution, live approval, publication or full-green acceptance.
Absent or invalid policy adoption leaves executable changes on strict full-green
acceptance. Supplying a SHA is host trust input, not proof of review or adoption;
the coordinator must verify those separately. The GitHub workflow loads its checker from the owner-controlled repository
variable `WORKHARBOR_CI_POLICY_SOURCE`, with credentials excluded from the checkout.
Missing, malformed or unavailable pins block; there is no proposal-head default.
The workflow itself must match the externally adopted policy source exactly.
The adopted revision remains the owner-approved content anchor after a rebase or
squash. The checker derives exact file/type/mode and test inventory bindings from
that trusted Git object, including its operator contracts. It permits only the
same ordered reviewed policy states or a forward collapse of those states; it
rejects invented, mixed, reversed or ambiguous merge history. Head and integration
must match the final manifest; a base may contain an earlier reviewed state plus
unrelated documentation. Frozen baseline inputs and all source/checkout/merge
checks remain required. Changed policy, scope, checks or dependencies require
owner approval of a new content anchor. A new candidate SHA still requires fresh
independent review and checks, without automatically changing the adopted pin.
CI execution requires a pristine isolated checkout, including ignored files:
ignored source or bytecode can alter imports. The gate routes Python caches to
fresh external storage and blocks preexisting untracked inputs before checks.
Launch tools with `-I -B` before imports; isolated child interpreters receive the
fresh cache location explicitly through `-X pycache_prefix`. Inherited Python
path/home/user-site overrides are removed for descendants. The trusted source
checkout is checked separately. Frozen tests run in a disposable checkout of the
same exact integration commit, so their legacy `.symphony` evidence never becomes
an input in the pristine operator checkout. Raw exits remain unchanged.
Use prepublication scratch when a working checkout contains legitimate local
`.specify` or `.symphony` state; the gate does not delete that state. Ordinary
root `make check` remains available with its raw result reported separately.
After exact independent review and authorized publication, the owner can configure
that immutable reviewed revision with:

```sh
gh variable set WORKHARBOR_CI_POLICY_SOURCE --repo wstein/workharbor-nxt --body REVIEWED_POLICY_SHA
```

That external adoption is a separate owner action and has not been performed by
these local checks. Existing repository CI requires this configuration before it
can validate the new operator tooling.

The checker's callable `source_context(root, event, base, head, checkout)` returns
validated immutable metadata or raises `ValueError`. `check(root, context,
policy_source=None)` runs the relevant checks and returns an exit code. A trusted
source checker can inspect a separate integration root with:

```sh
python3 -I -B /trusted/source/scripts/check_ci.py --root /isolated/integration \
  --event pull_request --base BASE_SHA --head HEAD_SHA --checkout MERGE_SHA
```

All four source options are required together. `--policy-source` adds the separate
explicit adoption input for operator validation; it never selects a task exception.
Run `python3 -I -B scripts/check_ci.py --self-test` and
`python3 -I -B scripts/test_check_ci.py` for legacy baseline and new source regressions.
Frozen verifier inputs remain unchanged; no external approval is refreshed by CI.

For a pre-baseline main promotion, the old base must be an ancestor of the fixed
accepted anchor and the complete approved source, head and actual merge trees
must match. The validated source/head history and canonical-main metadata remain
mandatory. The gate does not claim current prerequisites on that old base; it runs
all protected snapshot and execution checks on the promoted head and integration.

Full-range owner rebases retain the immutable source authority: a candidate may
use a rewritten baseline identity only after proving its complete ordered prefix
trees and exact baseline snapshot. Existing protected history and policy checks
then begin at that proven candidate anchor; a missing snapshot fails closed.
