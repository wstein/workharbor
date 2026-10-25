# T038 source-capture evidence

This is the first replacement slice of PR #261, limited to the source/Git capture
library needed by stage. It adds no lifecycle command, Docker mutation, result
import or promotion. T038/T039 and owner-host acceptance remain open.

## Review dispositions and reproducible checks

The capture portion of #261's findings is addressed here:

| Finding | Implementation and regression |
| --- | --- |
| F9 | Combined packed/loose ref counting; streaming main/shared-index validation and hashing. `TestLocalStageGitCombinedRefQuota`, `TestLocalStageGitIndexHeaderQuotaBeforeBody`, `TestLocalStageGitSharedIndexHeaderQuota`, `TestLocalStageGitIndexRejectsQuotaWithoutReadingBody`, `TestLocalStageGitIndexStreamsLargeExtension`. |
| F11 | Unsupported symbolic HEAD namespaces refuse before cloning: `TestLocalStageGitRejectsCustomHEADBeforeCapture`. |
| F15 (shared capture subprocess helper) | Final group kill precedes ownership release even after leader-observation failure: `TestLocalStageCaptureObservationFailureKillsOwnedGroup`, both timer cases. Stage runner progress precedence belongs to the next slice. |
| F16 (capture only) | Remove unused snapshot/working-copy wrappers and the stale descriptor-error check. Promotion/stage dead-code findings remain separate. |
| F20 (credential remotes) | Omit credential-bearing scp-style URLs: `TestLocalStageGitCredentialFreeSCPRemotes` and the real clone-config assertions in `TestLocalStageGitFormatsRefsAndSafeConfig`. |

Commit `7c2547a4` contains assertion-red Git regression cases, before their fixes.
Commit `802c7f01` contains the assertion-red owned-group cases before its fix.
The extraction commit retains the prior capture tests from #261; it is not a claim
that the original feature's entire implementation history was red-first.
Run from `apps/whr`:

```sh
go test ./internal/localdocker -run '^TestLocalStage(Trust|Capture|Git)' -count=1
go test -json ./... -count=1
go vet ./...
GOOS=darwin GOARCH=arm64 go vet ./internal/localdocker
```

All four commands exited zero locally. The full Go run recorded 3,210 passing test
identities, no failures and one local skip: `TestInitPhysicalENOSPCPremountedFallback`
requires unavailable local namespace capabilities. This is not full acceptance;
native CI must run that existing case without a test-level skip. Existing required IDs remain; the 18 capture
regression test names are added to the CI minimum. Native CI and independent review
of the published head remain separate gates.

Seven compiled Git mutants were killed: remove combined ref quota, remove custom
HEAD refusal, admit scp credentials, remove main-index quota, bypass shared-index
validation, swap the refusal reason while retaining exit 68, and read the whole
index before validation. Each failure was an assertion, not a compile failure.

## Boundaries and remaining acceptance

Capture tests preserve full history and dirty index/work bytes, including SHA-1,
SHA-256, populated submodules, split/conflict/intent-to-add state and locally
complete shallow history. Host configuration is read as data; source executable
configuration is not inherited by the clone. These fixtures do not prove stage
provisioning or official Dev Container CLI behavior.

Credential/generated-output omissions are listed in capture evidence. A tracked
file omitted by that policy appears deleted in the candidate; committing there can
turn the omission into a result change. Later promotion previews must disclose
this; no result import or automatic integration is present in this slice.

The named S1 acceptance tests in plan.md span later primary selection, source
preparation and Stage CLI wiring. This PR supplies only their capture prerequisite;
it does not relabel these broader criteria as complete. The final stage/composition
record must map each literal acceptance ID to its test coverage. No completion box
is changed here.
