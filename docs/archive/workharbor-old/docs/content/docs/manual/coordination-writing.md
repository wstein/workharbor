---
title: Writing and durable evidence
description: "Readable issues and comments with complete, portable evidence."
weight: 12
toc: true
---

This page supports AGENTS.md's writing rules and the approved reporting scope
in [crewbook #13](https://github.com/wstein/crewbook/issues/13). The immediate
WorkHarbor rules apply now. Full reusable crewbook templates remain future
M3 work after full native Codex support, before Antigravity.

## Issues and comments

Use normal grammatical prose. An issue targets 150–250 words for the problem,
expected result, 3–5 testable acceptance criteria and dependencies. A comment
targets 50–100 words for its outcome, decisive evidence and blocker or next
action. These are defaults, not blind limits. Expand when acceptance facts,
security conditions, exceptions or uncertainty would otherwise disappear.
Before posting, check whether a reader can identify the task, completion
conditions and blocker within 30 seconds.

Consolidate new requirements into the current description instead of appending
repeated sections. Keep completed acceptance criteria and meaningful decisions.
Distinguish committed, tested, independently reviewed and runtime-measured work
from queued work and unverified claims. A clean review identifies its exact
reviewed SHA, scope, result and remaining verification limits. A finding retains
its location, trigger, consequence, evidence and correction, expanding enough
to explain the security boundary.

Public issues and comments exclude machine home, temporary, user-folder and
local worktree paths, local thread IDs and command transcript noise. Use
portable repository paths, reproducible commands when material, and durable
committed links. Preserve meaningful identifiers, conditions, units and error
excerpts after sanitization; never replace a decisive condition with a vague
success claim.

## Preserve contracts before shortening

Detailed contracts, runbooks and reports belong in versioned documentation.
Before shortening their only issue or comment copy, map every acceptance and
security fact, approved protocol, exception and pending question to a committed
section. Link the exact revision and section from the shortened issue. Do not
claim archival is complete until the artifact exists and the preservation map
has been checked. A local file or inaccessible commit is not public evidence.

For [workharbor #283](https://github.com/wstein/workharbor/issues/283), existing
committed sources include [external skill sets](../design/skill-sets.md),
[native Codex supervision](../design/native-codex.md),
[canonical native role bindings](../design/native-bindings.md) and the
[external skill-set contract](skill-set-contract.md). These cover D52–D54 and
the package boundary, schema and measurement gates; they do not establish that
every later issue protocol has been archived. Preserve remaining approved
protocols before removing their only copy. Project-policy authority and
intentional-empty semantics remain pending Werner; documentation must not
resolve them by shortening. Likewise, the pending capacity choice and human
test G204 annotation remain pending.

The [workflow protocol record](workflow-protocols.md) preserves approved
[crewbook #11–#16](https://github.com/wstein/crewbook/milestone/3) contracts and
their research conditions. The [split runbook record](skill-split-runbook.md)
preserves #283's historical extraction commands and file boundaries; the
[pending policy proposal](pending-policy-proposal.md) retains its conditional
reader contract without deciding it. These artifacts still need a committed,
reviewed revision and preservation check before desk replaces their source
copies with public links. An issue summary is not a replacement for an
evaluation protocol or security contract.

## Publish evidence safely

Follow `.agents/desk.md` before publishing an artifact: the publication file
must exist, be regular and not a symlink. Run the pinned redacted secret scan
from the repository root. A successful scan requires exit 0, `no leaks found`
and more than zero bytes scanned. Any other result stops publication. Inspect
for personal data and machine paths as well; never print a secret match.

Prepare a sanitized publication artifact and repeat those checks after every
change. Label redactions and excerpts; sanitized evidence is not raw evidence.
Route the artifact to its documentation owner for versioned publication and
independent review. Spike scripts and raw output retain their required spike
branch location. Post a short conclusion with the sanitized durable evidence
link; full report pastes and multi-comment transcripts are not the default.
If evidence is not yet accessible, state that limitation without a local path.
