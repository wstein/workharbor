# Feature 003 T018 recovery-test proposal

Preparation only for issue #19. T018 is pending; this record grants no test-authoring
acceptance, implementation, owner approval, publication or live execution authority.

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
the approved discovery argv. No `--matrix-approved` invocation has occurred.
The new checks digest is a proposed expectation binding, not a fresh raw capture or
an acceptance result. Capture4 remains the actual observed outcome provenance.

The coordinator retains required prior-behavior checks, native negative checks and
the source-immutability guard, then obtains a concrete revision-bound owner decision
on the final package. A current approved run is required before T018 acceptance;
this preparation checks no task marker. Raw red suites remain red.
