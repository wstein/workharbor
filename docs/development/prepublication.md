# Prepublication preparation

`scripts/prepare_publication.py` checks an explicitly pinned proposal against an
explicitly pinned remote base in private disposable Git storage. It never pushes,
opens a PR, merges, changes labels or grants publication permission. Owner approval
and independent exact-candidate review remain separate prerequisites.

The publication base is always `refs/heads/main`, independent of the task's
execution baseline or accepted checker source. Other bases return `BLOCKED` with
`invalid_pr_base` before fetching or running checks. Follow the
[PR target and linear-history rule](../../WORKFLOW.md#pr-target).

Use Python 3.11+, POSIX process groups and Git with `merge-tree --write-tree` support:

```sh
python3 -I -B scripts/prepare_publication.py \
  --repository https://github.com/wstein/workharbor-nxt \
  --source REVIEWED_CHECKER_COMMIT \
  --base-ref refs/heads/main --base BASE_COMMIT \
  --head-ref refs/heads/topic --head PROPOSAL_COMMIT \
  --timeout 300
```

Replace each commit placeholder with its full 40-character lowercase Git SHA.
The operator supplies trusted repository identity, branch refs and expected pins;
proposal metadata cannot establish that authority. Absolute local Git repository
paths support offline reproduction. Remote access supports credential-free HTTPS
only. User information, query strings, SSH and other transports are rejected;
private authenticated repositories require a separately reviewed host adapter.

Preparation reads both remote refs, fetches complete reachable history without
resetting existing work, verifies the fetched pins, computes Git's actual merge
tree and creates a deterministic synthetic commit with ordered base/head parents.
A second scratch worktree loads `scripts/check_ci.py` from `--source`, independently
of the integration tree. The checker receives `--root`, `--event pull_request`,
`--base`, `--head`, `--checkout` and `--source`. This exact source pin identifies
the loaded and checked checker bytes/mode; it does not attest owner review or adoption.
Use a reviewed checker revision matching the proposal's checker code. There is no
external policy-source setting or historical-tree acceptance gate.

The checker applies the repository's [explicit acceptance policy](harness.md#ci-and-explicit-acceptance)
and reviewed manifest to actual integration. Its deprecated `--policy-source` alias
is accepted only as the same code-identity pin and emits a deprecation diagnostic;
the helper uses `--source` by default. Older unsupported checker interfaces, missing
tools/objects, conflicts, failed checks and deadlines return `BLOCKED`. Independent
exact-candidate review and owner authorization remain separate prerequisites.
Preparation adds no task guard, approval exception or task-specific phase exemption.
Raw suite failures remain visible even when the explicit acceptance matrix qualifies.

After checks, the command rereads both refs. Any mismatch blocks, preserving the
check exit separately. Exit 0 means `PREPARED`; exit 3 means `BLOCKED`. One JSON
record contains schema version, status, source/base/head/checkout/tree IDs,
check exit and a fixed blocker code. Diagnostics and raw command output are never
forwarded. Missing evidence remains null. Raw suite outcomes remain the checker's
responsibility; preparation success does not turn a red suite green.

Temporary storage is removed on success, failure and handled timeout; timed-out
process groups are killed. Retrying the same pins produces the same synthetic
commit. Existing checkout files, index, refs and history are read without writes.
No credentials are inherited by children; HOME and temporary paths point at scratch
storage, and global/system Git configuration and prompting are disabled. These
controls are not a filesystem or network sandbox. Checks execute repository code;
the operator must use trusted code or an appropriately isolated credential-free
host. The command cannot attest host isolation or provider approval.

`PREPARED` records only the observed pins at the final read. A future authorized
publisher must recheck both pins immediately before its write and reconcile any
uncertain write outcome; this command provides no atomic provider publication
transaction. On movement, fetch fresh pins and perform a new preparation/review.

Run the additional operator tests explicitly:

```sh
python3 -I -B scripts/test_prepare_publication.py -v
make check
make check-raw  # current raw baseline: five assertion failures, Make exit 2
```

The operator tests are also collected by `make check`. Historical Feature 003
acceptance records remain intact; this location creates no test waiver.
