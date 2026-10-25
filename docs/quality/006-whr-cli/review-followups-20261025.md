# CLI review follow-ups (2026-10-25)

This bounded test/evidence package follows reviews
[#255](https://github.com/wstein/workharbor/pull/255#pullrequestreview-5640605892),
[#252](https://github.com/wstein/workharbor/pull/252#pullrequestreview-5640157788),
[#249](https://github.com/wstein/workharbor/pull/249#pullrequestreview-5640350485)
and [#250](https://github.com/wstein/workharbor/pull/250#pullrequestreview-5640572957).
It changes test coverage and the four existing package evidence records.
Production, task/progress status and CI inventory are unchanged. T036 authoring
uses disjoint files and is outside this package.

Initial base: `3f899fd7391a5038b1185295e52ece87afeb2da7`. After the owner merged
#250, the first three authored commits were purely rebased onto
`960d84316e3459f55d33b431c9463c3ca1ba7ad1`; all three range-diff entries were
equal. No #250 commit was duplicated or rewritten. Measurements used Go
`go1.27.2 linux/arm64`, with offline module/toolchain settings. The ENOSPC
checks used umask 022 and an ordinary owner (real/effective UID 1000).

## Regression evidence

Each temporary Go source overlay compiled and reached a test assertion. The
production/test source mutations below are not retained in the repository.

| Mutation | Observed result |
| --- | --- |
| Admit only `./custom.json` in scaffold `relativePath` | Old traversal assertion passed. New assertion fails: exit remains 64, but the diagnostic names unrelated manifest flags instead of the safe-relative-path requirement. |
| Consume regexp through `base + loc[0]`, instead of `loc[1]` | Old submatch test passed. New suffix assertion fails: 10 pending bytes instead of 5. |
| Reset literal matcher's remembered offset to zero | Controlled 64-chunk test fails: work 2359305 exceeds linear bound 196749. |
| Reset the projected-byte offset to zero | Controlled sequence test fails: work 2201880 exceeds linear bound 196620. |
| Remove the namespace runner's ErrWaitDelay arm | Owned stdout-holding-grandchild fixture fails the required bound-sentinel assertion; real ErrWaitDelay is surfaced. |
| Disable namespace child-started guard | Owned nonzero shell result with a native-looking prefix becomes incorrectly eligible for fallback, and the test fails. |
| Disable all four premounted-helper UID/EUID/GID/EGID comparisons, retaining parsing and group checks | Both wrong-expected-owner subprocess tests fail their required identity diagnostic. |
| Disable helper's actual closed-environment assertion | Extra-field subprocess test fails its required environment diagnostic. |

The ENOSPC helper negatives invoke the actual runtime test entry point as the
ordinary owner. The identity check now precedes namespace inspection; identity
and supplementary-group failures have distinct diagnostics. All namespace IDs
remain unchanged, so a bypassed guard still stops before mount/fill. No negative
probe executes sudo, unshare, mount, root Go code or product Apply. These prove
the guard blocks; they cannot independently vary real/effective identity as an
unprivileged owner. The fixed trusted root prelude is unchanged.

The PTY integration floods drain before measuring, and controlled chunk tests
separately reject repeated-prefix work. This retains the linearity oracle while
removing scheduler-dependent reader wake-up counts. No sleeps, retries, relaxed
thresholds or production hooks were added.

## Focused validation

From the repository root, with `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`:

```sh
go test -count=1 -v ./apps/whr/internal/commands -run '^TestInit(Devcontainer|NetworkCandidateBoundsDoNotFallback|NetworkStartedChildDoesNotFallback)'
go test -count=1 -v ./apps/whr/internal/ptytest -run '^(TestExpectRegexpSubmatches|TestIncrementalMatcherWorkIsLinear|TestIncrementalSequenceWorkIsLinear|TestLargeOutputIsMatchedLinearly|TestUnterminatedSequenceFloodIsMatchedLinearly|TestTerminatedSequenceFloodAcrossReads|TestExpectRegexpWorkIsBounded)$'
go test -count=1 -v ./apps/whr/internal/scaffold -run '^TestInitPhysicalENOSPC(HelperRejectsUnsafeRuntime|Fallback|DirectMap|MappingHelper|Prelude|Privileged|Required|Native|Child)'
go vet ./apps/whr/internal/commands ./apps/whr/internal/scaffold ./apps/whr/internal/ptytest
```

The focused runs passed with 14 command, 7 PTY and 31 scaffold test/subtest
outcomes, zero skips and zero failures. Source policy, gofmt and whitespace
checks passed. Commands/PTY ran before the pure rebase; the new scaffold
negatives ran on the merged base. Fresh head checks and independent review are
separate gates, coordinated before publication.

The hosted paths now have durable prior-head evidence: network run
[40136687890](https://github.com/wstein/workharbor/actions/runs/40136687890)
used sudo, setpriv and a closed environment without a skip; physical ENOSPC run
[40141145793](https://github.com/wstein/workharbor/actions/runs/40141145793/job/121013356109)
passed all four cases and the dedicated premounted fallback. The existing
records retain their earlier local refusals as history. Those runs do not
claim fresh CI for this follow-up or complete T030/T031/T033 acceptance.

No full suite, race run, standalone build, installation, privileged launcher,
mount, host/browser acceptance, task completion, publication or CI rerun was
performed by this author. The coordinator owns final checks, independent
review and the single PR publication round.
