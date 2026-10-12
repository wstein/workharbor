# Feature 003 T018 recovery-test proposal

Historical preparation and owner-approved test-authoring acceptance for issue #19.
T018 completion means the exact reviewed red matrix matched; successor behavior is
still unimplemented. This record grants no publication or live execution authority.

Authoritative scope: `specs/003-single-task-entry/tasks.md` T018 and
`data-model.md` completion/recovery transitions. Changes are confined to the existing
`tests/test_single_task_state.py` and this proposal. The ten original state cases and
their class AST are unchanged. No guard, task checkbox, Feature 001/002 or policy changes.

## Source and observed evidence

- Reviewed prerequisite snapshot: `52b0d3dc86689440c0f90f765925293a64da38d1`.
- Current prerequisite binding: reviewed T017 publication candidate
  `767eb60fa6b69240e98b69ff379081469ea55484`, based on accepted main
  `ca68ff22a633d43577ef2ef7d9e2ec1e99e5d402`. Publication and final exact-head
  review remain coordinator gates; this record does not claim a merge.
- All `scripts/` and baseline `tests/` bytes are identical between the historical
  prerequisite and current publication candidate. The rebased T018 test bytes are
  identical to reviewed preparation `6723b68553db012d4f63b757c1ba51e8f4a929cf`;
  no heavy capture was repeated for documentation, checkbox or Git metadata changes.
- Independent author clone: `/private/tmp/workharbor-003-t018-independent`.
- Test bytes SHA-256: `fa50744be4dfcb935fea7da9b2d55437bbb64041d94c3f228021e1ca2620dc22`.
- Native classifier: unchanged `scripts/task_outcomes.py`, `sha256:3683aa2283e219e8e253b619cc3a41ec51c5bc2ba29c161305ee1e3e2c4938cd`.
- Historical capture4 input/output snapshot: `sha256:53d628c3b4e5b068713dff849bc5a1229398298bda9d33ea72b3b7075720ea6a`.
- Capture4: 60 collected and observed, 13 pass, 47 body assertions; raw exit 1;
  zero import/setup errors, skipped cases or unobserved IDs. Source snapshot unchanged.
- Capture4 JSON/raw: `/private/tmp/t018-state-capture4.json` and `.raw`.
- Earlier valid capture3 (55 cases: 13 pass, 42 body assertions) remains at
  `/private/tmp/t018-state-capture3.json` and `.raw`; its scope gap was corrected,
  with no retroactive acceptance claim.
- Earlier exploratory captures were rejected: initial missing test import caused
  39 setup errors; subsequent draft had three read-only-store fixture errors.
  Those are not approved reds or acceptance evidence.
- Focused controls: state primitives 12/12; direct acceptance 11/11; completion
  publication 10/10, each raw exit 0. Existing public suite: 65 cases, 63 pass,
  exactly two retained T023 assertions, raw exit 1. No full canonical `make check`
  was run by this preparation leaf; the coordinator owns that final check.

## Fixture assurance and limits

Each new case first performs real valid T010 entry with isolated local Git, reviewed
context and two ordered checks. Success/completion prerequisites use the actual
verify/complete CLI; commit fixtures are real local Git objects. Crash children
exit 91 at the existing acceptance, intent, replace, directory-fsync, marker-event
and completion-event boundaries. The parent checks exact bytes/progress/events
before invoking resume. Resume publication probes require durable intent and fresh
acceptance before replace. Deliberately corrupt semantic evidence uses the existing
Store API; no successful binding/event schema or owner authority is manufactured.

The observed 47 reds stop at the specified body assertion after a real CLI call
returns `command_unavailable` (exit 2); deeper success/blocker assertions remain
unexecuted until T020/T021/T023. The six checked-rework boundary/blocker bodies first require
genuine commit binding, then genuine rework entry (T022/T023); no prior binding is
fabricated to force those deeper assertions to run. Recovery of an earlier prepared
transition may return completed-awaiting-commit; already completed state must reject
missing commit evidence. No handoff-ready claim is made without binding and revalidation.

## Exact observed matrix

All IDs below are prefixed by `test_single_task_state.`. `pass` requires no
fingerprint. `assertion` binds the exact AssertionError identity using the unchanged
native `assertion_fingerprint`; unrelated failures or unexpected green require review.

| Collected ID suffix | Outcome | Reached CLI assertion | Fingerprint | Successor |
| --- | --- | --- | --- | --- |
| `SingleTaskStateTests.test_append_only_events` | pass | — | — | implemented control |
| `SingleTaskStateTests.test_corrupt_state_retained` | pass | — | — | implemented control |
| `SingleTaskStateTests.test_immutable_context` | pass | — | — | implemented control |
| `SingleTaskStateTests.test_invalid_entry_preserves_tasks` | pass | — | — | implemented control |
| `SingleTaskStateTests.test_legacy_state_retained` | pass | — | — | implemented control |
| `SingleTaskStateTests.test_lock_released_after_exit` | pass | — | — | implemented control |
| `SingleTaskStateTests.test_one_lock_owner` | pass | — | — | implemented control |
| `SingleTaskStateTests.test_sanitized_structured_error` | pass | — | — | implemented control |
| `SingleTaskStateTests.test_transaction_rollback` | pass | — | — | implemented control |
| `SingleTaskStateTests.test_unsupported_state_retained` | pass | — | — | implemented control |
| `TaskRecoveryTests.test_block_after_acceptance_check` | assertion | resume: desired 4, actual 2 | `sha256:f2a8839a79b7987e033cd2921b08b1a0994bc670ea94b4311edf838ed5db5a93` | T021/T023 |
| `TaskRecoveryTests.test_block_after_acceptance_content` | assertion | resume: desired 3, actual 2 | `sha256:86c279fd7b91cdc3b2c28b4aabcd59314b7ff823db539ca5d6d267e36953e798` | T021/T023 |
| `TaskRecoveryTests.test_block_after_acceptance_missing_review` | assertion | resume: desired 3, actual 2 | `sha256:41aba18624f803707d6c0b4655a29ac3a49f31d254a97bbd77bca1c4fd92aa2c` | T021/T023 |
| `TaskRecoveryTests.test_block_after_acceptance_review` | assertion | resume: desired 3, actual 2 | `sha256:b367be2c2b5b9b05a28d829308c10357a5847eb406362ab5dd038f5875ab1c85` | T021/T023 |
| `TaskRecoveryTests.test_block_after_acceptance_task_bytes` | assertion | resume: desired 3, actual 2 | `sha256:2c88bdf8419bd61538d4bdfd9769b93971ec53dbe760e78a18ddef7f0377041d` | T021/T023 |
| `TaskRecoveryTests.test_block_after_intent_check` | assertion | resume: desired 4, actual 2 | `sha256:90fb87dc398f074f376d22568932977b4b184caf8990364964852397e1821d01` | T021/T023 |
| `TaskRecoveryTests.test_block_after_intent_content` | assertion | resume: desired 3, actual 2 | `sha256:5dc9c6460f07fd7ab32375b1e219ff217ea27f21a007bddffc9e02c9e53f62f3` | T021/T023 |
| `TaskRecoveryTests.test_block_after_intent_missing_review` | assertion | resume: desired 3, actual 2 | `sha256:486b1e70f1799925053c15db3e2666118c8df3de9ea047c7039cb3d2aaa9d8b1` | T021/T023 |
| `TaskRecoveryTests.test_block_after_intent_review` | assertion | resume: desired 3, actual 2 | `sha256:df403bcf59260c2750c1e0e3417b335399471dbe8117c396eafaed3c3b3e8d1d` | T021/T023 |
| `TaskRecoveryTests.test_block_after_intent_task_bytes` | assertion | resume: desired 3, actual 2 | `sha256:dd27cd0dbebe77aad0519c6a3180cc70ccc4316f317c5244c3abaa9a6b2d7493` | T021/T023 |
| `TaskRecoveryTests.test_block_after_replace_check` | assertion | resume: desired 4, actual 2 | `sha256:ba0616ab446c16264fa8e38547dbae3986a01cc9b17fdf0ce958161b524cfe48` | T021/T023 |
| `TaskRecoveryTests.test_block_after_replace_content` | assertion | resume: desired 3, actual 2 | `sha256:f5485319959661745c7f88312815bf6798da0e9e7d6e809f0da8f794c80f8893` | T021/T023 |
| `TaskRecoveryTests.test_block_after_replace_missing_review` | assertion | resume: desired 3, actual 2 | `sha256:8b6aab78d34f7f4dfab5acad33354a8fd55d980a5ab9eeb92beabbf81867122f` | T021/T023 |
| `TaskRecoveryTests.test_block_after_replace_review` | assertion | resume: desired 3, actual 2 | `sha256:ab26674cfd0d927cb87f2ec18dc8b5aaf5778ea42a4ca2b965cb4475fe3bc259` | T021/T023 |
| `TaskRecoveryTests.test_block_after_replace_task_bytes` | assertion | resume: desired 3, actual 2 | `sha256:7c55f3d19759e1fa869a0b757a10830ade5af79659bd17839dfb90687fdb1844` | T021/T023 |
| `TaskRecoveryTests.test_block_before_acceptance_check` | assertion | resume: desired 4, actual 2 | `sha256:cca4441661e0d0abd1b0a6b31a73e446c8fb158f0e1e7df2dc4592a750bff55d` | T021/T023 |
| `TaskRecoveryTests.test_block_before_acceptance_missing_review` | assertion | resume: desired 3, actual 2 | `sha256:b437f8560511264a41ec2aa60751b9fda9c6cf8888631ba851130d6f601eef07` | T021/T023 |
| `TaskRecoveryTests.test_block_before_acceptance_review` | assertion | resume: desired 3, actual 2 | `sha256:fa831200ada2b2a9e81fe56f6c3abfa09af35fbe36fd0f8c0169227d11aa13d2` | T021/T023 |
| `TaskRecoveryTests.test_block_before_acceptance_task_bytes` | assertion | resume: desired 3, actual 2 | `sha256:da49b5a615d138b4454ccea8921b8ede02528e23620aa466e9b2106e065a87e9` | T021/T023 |
| `TaskRecoveryTests.test_checked_rework_after_acceptance` | assertion | record-commit: desired 0, actual 2 | `sha256:6a315cc4b6526d32624dfad801774b0d6f8f2a977b6947484d93ae66899d3c37` | T020/T021/T023; fixture T022 |
| `TaskRecoveryTests.test_checked_rework_before_acceptance` | assertion | record-commit: desired 0, actual 2 | `sha256:2226587d9867a384467c56459182b34abec50572c083a65090db045d07e33bf0` | T020/T021/T023; fixture T022 |
| `TaskRecoveryTests.test_checked_rework_block_check` | assertion | record-commit: desired 0, actual 2 | `sha256:af654104ad30b1579a30c89c5eb7648cf31b3b1314cbeb4d89e726ee75ddb173` | T020/T021/T023; fixture T022 |
| `TaskRecoveryTests.test_checked_rework_block_content` | assertion | record-commit: desired 0, actual 2 | `sha256:4f27f44e20e965bd5fb7e520af19084ce39c468d107d9424d0dce6058fcda1bb` | T020/T021/T023; fixture T022 |
| `TaskRecoveryTests.test_checked_rework_block_missing_intent` | assertion | record-commit: desired 0, actual 2 | `sha256:2d3b86e9ff3886fd077e72fbe4a054a681e4529be676800ea42f5556275ae92b` | T020/T021/T023; fixture T022 |
| `TaskRecoveryTests.test_checked_rework_block_review` | assertion | record-commit: desired 0, actual 2 | `sha256:7fe0b1d1a4d6f95e2aa45c98f5d107e1248c8c362abbdd91feb30b62bcb4e60f` | T020/T021/T023; fixture T022 |
| `TaskRecoveryTests.test_contradictory_intent_blocks_recovery` | assertion | resume: desired 3, actual 2 | `sha256:883ff6c7d62f8e3c89305603c100f6f2bf8fc7be8a335130ba845dc25f83e4cd` | T021/T023 |
| `TaskRecoveryTests.test_corrupt_context_copy_is_retained` | pass | — | — | implemented control |
| `TaskRecoveryTests.test_corrupt_journal_is_retained` | pass | — | — | implemented control |
| `TaskRecoveryTests.test_corrupt_latest_acceptance_blocks_recovery` | assertion | resume: desired 3, actual 2 | `sha256:e7e8b954999636ce2bb6dee41582bb1faf22a6a2b3e59048a88c808c80974df7` | T021/T023 |
| `TaskRecoveryTests.test_failed_handoff_rerun_preserves_bound_evidence` | assertion | record-commit: desired 0, actual 2 | `sha256:ad6b0314f18d3a41e616e4fdd93e625aef92fe7b4a45d0e57eea6401a0aa3c96` | T020/T023 |
| `TaskRecoveryTests.test_later_progress_without_intent_is_corruption` | assertion | resume: desired 3, actual 2 | `sha256:e907026a3484251ab83e61483e2bd6d65b61cbfd7cf0997b251dc5fdb7404ff0` | T021/T023 |
| `TaskRecoveryTests.test_missing_journal_is_not_reconstructed` | pass | — | — | implemented control |
| `TaskRecoveryTests.test_record_commit_rejects_changed_mode` | assertion | record-commit: desired 3, actual 2 | `sha256:c438cfda4ff328e99e9e0b74b0d7cfb1993ec1af8f634cbaf0bb9c661af99c5e` | T020/T023 |
| `TaskRecoveryTests.test_record_commit_rejects_reverted_history_violation` | assertion | record-commit: desired 3, actual 2 | `sha256:836bda3730e54b6fcafb3bc568e9aa312b67e31f9810f242901a7b490c5deb99` | T020/T023 |
| `TaskRecoveryTests.test_record_commit_rejects_staged_hidden_violation` | assertion | record-commit: desired 3, actual 2 | `sha256:1b5b34eeff038a252eefd3119bf96cd2df33edf933d99508608b83aa2e127ad7` | T020/T023 |
| `TaskRecoveryTests.test_record_commit_with_mismatched_sealed_output` | assertion | record-commit: desired 3, actual 2 | `sha256:999d40e217b473b8a206fb0f54b4c849afab2e38e2797278162a5c09c84b9481` | T020/T023 |
| `TaskRecoveryTests.test_record_commit_without_completed_marker` | assertion | record-commit: desired 3, actual 2 | `sha256:80b682e3837dbb0b20a6b09f1b8450b0e7bdfcc696cc17471910d2d979aec08a` | T020/T023 |
| `TaskRecoveryTests.test_record_matching_commit_and_fresh_handoff` | assertion | record-commit: desired 0, actual 2 | `sha256:61edb5ffbf213d2d30161a91e0fb81717d171734574ee4d4aa912bb1e1c8fd39` | T020/T023 |
| `TaskRecoveryTests.test_record_unavailable_commit_retains_evidence` | assertion | record-commit: desired 3, actual 2 | `sha256:fdc81dfb129732f7f7e58a7a1e771e041a95259b641afc00a9bef7cd9e4185ff` | T020/T023 |
| `TaskRecoveryTests.test_record_unreachable_existing_commit` | assertion | record-commit: desired 3, actual 2 | `sha256:36306727bc5e690dea744d2d4e6fa047e9ffd23b3e8143e7f4f1c1912aaa1892` | T020/T023 |
| `TaskRecoveryTests.test_resume_after_acceptance` | assertion | resume: desired 0, actual 2 | `sha256:7fb014124886e5c56c2397648a317a3be7f917f7ecd274e85de6417764d1bff8` | T021/T023 |
| `TaskRecoveryTests.test_resume_after_check_result` | assertion | resume: desired 0, actual 2 | `sha256:ea26aec9618027e1fe7e83af2e3064dedba1e3b5b7f332d59267b662417a2b25` | T021/T023 |
| `TaskRecoveryTests.test_resume_after_checkbox` | assertion | resume: desired 0, actual 2 | `sha256:f3c6a77aa2c29b2d20f5ad4f3b23706dd8d35b98462e3d60cf3a97983d5adb37` | T021/T023 |
| `TaskRecoveryTests.test_resume_after_completion` | assertion | resume: desired 3, actual 2 | `sha256:e3ecbb0f98d1f4f08da2b5d60d4913d7efc8332a3dd489d3a098012048e9d9af` | T021/T023 |
| `TaskRecoveryTests.test_resume_after_directory_flush` | assertion | resume: desired 0, actual 2 | `sha256:8419078af7e88bc09349c7f4ff27838eaaecdfaa43ef7610401fa8c71c59e962` | T021/T023 |
| `TaskRecoveryTests.test_resume_after_intent` | assertion | resume: desired 0, actual 2 | `sha256:1feb00e3d7727f11886fb3e1d9c7c81345bc4e3ffc51d961fa7196695ebae103` | T021/T023 |
| `TaskRecoveryTests.test_resume_after_replace` | assertion | resume: desired 0, actual 2 | `sha256:fc07e4e1265cf8d0b4353fa55e46cb611a1806e6399bbe5e505df653acfe8aac` | T021/T023 |
| `TaskRecoveryTests.test_resume_before_acceptance` | assertion | resume: desired 0, actual 2 | `sha256:17d2de6080be9d718d13db8b300f4c1ba96615f3064d7bf00953983e1f5d7964` | T021/T023 |
| `TaskRecoveryTests.test_resume_before_intent` | assertion | resume: desired 0, actual 2 | `sha256:7af4cc183d595dbd58561131581d9b285825674b753ecb0eeabca080980b9080` | T021/T023 |
| `TaskRecoveryTests.test_verification_retains_original_implementation_identity` | assertion | record-commit: desired 0, actual 2 | `sha256:7f133c0f23653b74e1a2f9b135755dca917541f03cb1dced8ecb778557445c99` | T020/T023 |

## Proposed exact check inputs (not approved or invoked)

The historical capture4 matrix remains at
`/private/tmp/t018-proposed-checks.capture4.json`, digest
`sha256:5ab22d42a2af568587e2347452853b9b51692f4b12bcbd952df5e297bfddae19`.
Its input revision is historical and is not used for current approval. Its original
argv and full proposed package remain recorded by preparation commit
`6723b68553db012d4f63b757c1ba51e8f4a929cf`.

After this proposal commit is frozen, the same 60-case expectations are rebound to
its full sealed-root snapshot in `/private/tmp/t018-final-checks.json`. Exact source
SHA/tree, native tool revision, current checks digest, complete wrapper argv and
historical outcome provenance are supplied together by
`/private/tmp/t018-owner-approval-package.json`, outside the sealed root. The source
comparison is `/private/tmp/t018-final-source-equivalence.json`.

Proposed discovery descriptor (native validator requires the current interpreter's
canonical `sys.executable` spelling):

```json
["/opt/homebrew/opt/python@3.14/bin/python3.14", "-I", "-B", "-m", "unittest", "discover", "-s", "tests", "-p", "test_single_task_state.py", "-v"]
```

Invoke the native wrapper through `/opt/homebrew/bin/python3.14` with command
`PATH=/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin`; no shell evaluation is part of
the approved discovery argv. At preparation freeze no `--matrix-approved` invocation had occurred, and the
rebound checks were expectations only. Capture4 remains the historical outcome
provenance; the subsequent approved current run is recorded below.

The coordinator retains required prior-behavior checks, native negative checks and
the source-immutability guard. The preparation itself checked no task marker.
The owner decision and matched current run below authorize T018 test-authoring
completion only. Raw red suites remain red.

## Owner-approved test-authoring acceptance

The owner approved the exact final package in the active session after the combined
explicit package and issue-contract recommendation. This is supplied session
operator authority, not independently authenticated owner identity or live dispatch.
The original issue contract and preparation provenance remain retained.

- Approved execution source: `e090cfd698976286f69fb74abd8499fc26eec6c5`.
- Exact native checks digest:
  `sha256:37987a0991a476cda2ca71e3f5707f1635c01cef16f26d250e0fff84b2a2b78e`.
- Exact package, native argv and command PATH: `/private/tmp/t018-owner-approval-package.json`;
  package digest `sha256:8c48b978748bdb350e948e7abb1133e598209cc74b587a3b319f44476b594e12`.
- Actual approved native wrapper exit: **0**, accepted true. Its raw child exit was
  **1**: exactly 60 collected/observed IDs, 13 passes and 47 expected body assertions,
  with every approved fingerprint matched. No import/setup/skip outcome qualified.
- Source snapshot before and after the approved run was identical:
  `sha256:1399648f6a40d7d4ee102e58b671280197f94fdb239c00ec97b6746ffe8ec57e`.
- Full native result: `/private/tmp/t018-approved-acceptance.stdout`, digest
  `sha256:b8692778d33ea1576fa0602d9cabf7934899facf9adcde344c7aa84f84f7fd7e`;
  stderr was empty. Actual argv/PATH/exits/snapshots are retained in
  `/private/tmp/t018-approved-acceptance-execution.json`; supplied authority is retained
  in `/private/tmp/t018-approved-execution-authority.json`.

All tracked completion bookkeeping followed the matched approved run. Only T018's
checkbox and this task's evidence change. No guard or test code changes, successor
implementation, new CI exception, publication, merge or live execution is included.
The 47 reds still stop at unsupported public-command behavior; deeper recovery and
binding assertions remain unexecuted. T020/T021/T023 must implement that behavior,
with genuine checked-rework entry requiring T022/T023. A matching red matrix does
not claim those behaviors work or a green raw/full repository suite.

## T020 implementation checkpoint
The separately reviewed owning implementation is frozen at
`b4e9cc5442af154996b6590362a6a8fc162fa508`. Direct commit-binding
controls passed **12/12**, raw exit **0**, with zero errors, skips or failed
subtests. The before/after source snapshot was identical:
`sha256:f3fc101dca09b805544dc2362dd6c2d04d4672d5ce273cbf42667de4c52013b5`.
The actual command was `/opt/homebrew/bin/python3.14 -B
/private/tmp/collect-feature003-handoff.py test_single_task_binding.py t020-final`.
- `/private/tmp/workharbor-003-t020-final-raw.json`: SHA-256 `a59a383106d73d0478cdccdd953632adfa3b1ec0d1fe12010ea089859e190592`.
- `/private/tmp/workharbor-003-t020-final-raw.log`: SHA-256 `31ca588e2ac9e95249b47c06aec3570b60e6c1436185302e6fc6c88248af789f`.
- `/private/tmp/workharbor-003-t020-rework-red.log`: SHA-256 `4ff4ed07207292c69bf027e3b26689d7f2abe77d45c4c31849b96dea170cdde7`.
- `/private/tmp/workharbor-003-t020-rework-green.log`: SHA-256 `c08cd0ce21dd7be9e077f641f1a48d938bbd6cad820dd25afa7ccc2e823b8671`.

The rejected rework fixture run failed on stale semantic-review preparation
before binding; it does not establish a criterion-specific production RED.
The corrected positive uses an explicitly primitive-created linked checked
rework attempt, actual acceptance/publication and actual binding to prove exact
uppercase `[X]` preservation. It does not claim T022 entry authorization.
The first 11-case frozen run remains earlier-source evidence.

The original owner-approved test-authoring matrix and acceptance above remain
unchanged. T020 binds one immutable reachable matching local implementation
commit and appends later verification identities separately. T021 recovery,
T022 authorized entry and T023 public integration remain undelivered here;
there is no full-suite, handoff-ready, CI-policy or live-execution claim.

## T021 implementation checkpoint

Reviewed source `e2a8383d7f44c6dab29230527f2007b959763ee5` passed
15 owning recovery controls, raw exit 0, zero errors/skips/failed subtests.
Exact before/after snapshot: `sha256:f320734949c19a124d5bd968a09ea0aa1f6f8c05e9f03c77a9a9099b153c220f`.
Command: `/opt/homebrew/bin/python3.14 -B /private/tmp/collect-feature003-handoff.py test_single_task_recovery.py t021-final`.
- `/private/tmp/workharbor-003-t021-final-raw.json`: SHA-256 `1018c0c62333e1ce953a26226576608d33dda48535578c65e516da08d6de2211`.
- `/private/tmp/workharbor-003-t021-final-raw.log`: SHA-256 `0de39510759f2562e92d4c70ae0d477355595dfece437e18a57d29802f8c0ac9`.
- `/private/tmp/workharbor-003-t021-completion-regression.log`: SHA-256 `4f42c97fbf9bdff2435457adf81d5edcec23be822ee3f9f2c004a1ed35958218`.
- `/private/tmp/workharbor-003-t021-first-red.log`: SHA-256 `a85fb91bddd89454aab024bb841c22616f7bb530d4e27b981234d0eec0ffb6c0`.

The existing ten completion controls also passed on the earlier coherent draft;
the final recovery controls exercise actual fresh checks, exact retained intent,
phase-corruption rejection and FIFO-race rejection. A real child exits after
temporary-file fsync; reopen preserves the private intent-owned file and recovery
consumes it through atomic replacement with both directories flushed. Old events
and attempt identity remain intact. T022 entry and T023 CLI/handoff remain pending;
original test-authoring acceptance above is unchanged. No full-suite or live claim.

## T023 implementation and final-qualification checkpoint

Reviewed source `238fff8fa65c6e4e5b0b98e5f2bbacdea25dec9b` wires actual
record-commit, resume and authorized rework under the worktree lock. First-draft
public measurements passed 60 state/recovery, 116 context/rework, 65 retained CLI
and 12 direct CLI cases. They precede the fixed readiness race and have no stable
source-snapshot qualification; final combined acceptance remains pending.
Actual race/ready-record controls passed 2/2 after the fix; the post-completion
safe-argument control passed 1/1. Readiness now retains the exact fresh acceptance
sequence/digest/checked HEAD, independently revalidated after binding inspection.
- `/private/tmp/workharbor-003-t023-recovery-first.log`: SHA-256 `725c06a90e0dd692eca5c188734373662ba35eacdf0189c6db29887dd5dfc0c3`.
- `/private/tmp/workharbor-003-t023-context-first.log`: SHA-256 `2cce52fa852a3dc2b23166c27809c4287abb00c0f480929e9b814fe71d84fd3c`.
- `/private/tmp/workharbor-003-t023-retained-first.log`: SHA-256 `55184e7281c310b9fddf1a64ad89fe107a33d8e85f3a90b32e1a3cab3f6dfff4`.
- `/private/tmp/workharbor-003-t023-direct-cli-behavior.log`: SHA-256 `f748ab6419f7411f8062cdcf26d97c25776b29c2dbbb96db7f58c54bb1832fc6`.
- `/private/tmp/workharbor-003-t023-handoff-green.log`: SHA-256 `179303feba2227e1a2df8a19fbee0e9e5c0c7229173a2e1592bac25a4a11e12b`.
- `/private/tmp/workharbor-003-t023-argument-green.log`: SHA-256 `bea9f3973c75aa2973820253d2f692aa91bd96f077eae6f5393c90d5ee783a7f`.
- `/private/tmp/workharbor-003-t023-policy-inventory.json`: SHA-256 `ea82e48c24aeced75845869a5a7085ce0d859bc102b256a5f6899cf05775f144`.

The legacy frozen `verify_t004_cli.py` wrapper was also invoked directly and
correctly refused changed frozen inputs (exit 1); its historical seal is not current
CI authority. The actual retained direct class passed through unittest discovery.
The inventory proposal preserves 377 prior required IDs and adds all 108 new owning
IDs (485 required; 791 total discovered), removing only the two now-passing T023
body assertions. No classifier change, new exception, skip or owner-adoption claim.
Original approval matrices and evidence above remain unchanged. T023's task marker
will follow final combined acceptance; T024+ and live execution remain outside scope.

### Final local combined check: incomplete at aggregate deadline

Frozen candidate `abebbd637e470e7eb94febb26a14c3964f2163cb` ran exactly
`make check PYTHON=/opt/homebrew/bin/python3.14` once, with
`PATH=/opt/homebrew/opt/python@3.14/libexec/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin`.
Both outer and nested `python3` resolve to Python 3.14.8. Actual Make exit was **2**:
the unchanged raw aggregate 300-second deadline expired during a T018 recovery case.
Before the deadline, 343 completed `ok` lines and no failure/error lines were observed.
The qualified phase did not run. Cleanup removed the disposable baseline while the
child suite finished, producing a final 393-case summary with 44 missing-file errors
and two assertions; this cleanup cascade is retained, never classified as accepted
or a complete product test result. A narrow process probe confirmed the named run
had drained before bookkeeping or integration; the sole checkout remained clean.
- `/private/tmp/workharbor-003-us3-make-check.log`: SHA-256 `6fb2eac7357dfbba0572479eb77bd20cf439c9d5375e2317c5c323bd91c64f6f`.
- `/private/tmp/workharbor-003-us3-canonical-record.json`: SHA-256 `80a99f72cfc1f81afe6d792d3d9439ec17d4e5f8f6163884d9eae4c07f9b4d71`.

No deadline/policy waiver or repeated local canonical run was introduced. T023's
marker remains unchecked pending final Linux qualification of all 791 collected
cases on the combined published head. The subsequently integrated, independently
reviewed milestone status changes are documentation only; tested source provenance
above remains explicit. Owner PR review/merge is still required to adopt the inventory
proposal. T024+ and live/provider/runtime activation remain outside this route.
