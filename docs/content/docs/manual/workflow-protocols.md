---
title: Approved workflow protocol record
description: "Preserved acceptance and research protocols before issue consolidation."
weight: 13
toc: true
---

This sanitized preservation record captures the existing approved crewbook
#11–#16 requirements before desk shortens their issue bodies. It records future
M3 implementation contracts, not completed work or new operative policy.
Full native Codex support remains first, then M3, then Antigravity. The immediate
writing rules are in [Writing and durable evidence](coordination-writing.md).

The capacity wording recorded in #16 is historical human direction. Operative
policy now follows H164 and H175: the per-repository two and cross-repository
three source-code workers are defaults, not limits, and there is no upper cap
on code workers. This archive does not retry the rejected capacity patch.
Project-policy authority, intentional-empty choices and the human test G204
annotation remain pending.

The sections below preserve every original unchecked acceptance criterion,
starting prompt, condition, dependency and research qualification. The ACM
review in [comment 5980987737](https://github.com/wstein/crewbook/issues/14#issuecomment-5980987737)
is mapped to the research additions under #11, #13, #14 and #16; its procedural
instruction to append text is historical and superseded by consolidation.
No live evaluation, paid judge, daemon, publication or support claim follows
from archival. Research interpretations are retained as previously approved
attributions, not newly measured results.
Publication links use the cited author copies to avoid publisher access-denied
responses, with the original DOI identifiers retained.

| Source | Preserved section |
| --- | --- |
| [crewbook #11](https://github.com/wstein/crewbook/issues/11) | [#11 protocol](#crewbook-11) |
| [crewbook #12](https://github.com/wstein/crewbook/issues/12) | [#12 protocol](#crewbook-12) |
| [crewbook #13](https://github.com/wstein/crewbook/issues/13) | [#13 protocol](#crewbook-13) |
| [crewbook #14](https://github.com/wstein/crewbook/issues/14) | [#14 protocol](#crewbook-14) |
| [crewbook #15](https://github.com/wstein/crewbook/issues/15) | [#15 protocol](#crewbook-15) |
| [crewbook #16](https://github.com/wstein/crewbook/issues/16) | [#16 protocol](#crewbook-16) |

## crewbook 11

Source: [Add a safety-preserving simplicity ladder to coding skills](https://github.com/wstein/crewbook/issues/11).

Team recommendation: **5/5**. Adapt Ponytail.

Agents should choose a small, complete solution after understanding the actual flow, rather than optimize for lines of code. Add one canonical simplicity rule and reference it from the relevant coding roles.

### Acceptance criteria
- [ ] Adopt the recommended starting prompt below as the canonical baseline, coordinating portable role content with [crewbook #3](https://github.com/wstein/crewbook/issues/3)/#4 and native Codex support with [crewbook #9](https://github.com/wstein/crewbook/issues/9).
- [ ] Explain the selection order: stated requirement, existing repository implementation or pattern, standard library, appropriate native feature, existing dependency, focused new implementation. Trace the affected flow before choosing.
- [ ] Explicitly preserve readability, required behavior, trust-boundary validation, authorization, isolation, data-loss handling, accessibility where relevant and required tests.
- [ ] Remove one-line, shortest-diff, fewest-file and net-LOC targets. Do not categorically forbid a single-implementation interface when it defines a genuine adapter or policy boundary.
- [ ] Limit added explanation to useful guidance; avoid repeating the full rule in every provider descriptor. Alternative skill sets and explicit none remain supported.
- [ ] Verify the baseline is reachable from relevant cb-* coding entrypoints and include representative review examples of reuse, a justified abstraction and a security check that must remain. Do not claim token/cost savings without the controlled evaluation item.

### Recommended starting prompt

> Implement the smallest complete solution to the stated requirements. Read the affected flow first. Reuse existing code and prefer standard-library or native features. Avoid speculative abstractions and dependencies. Preserve readability, validation, authorization, isolation, error handling and required tests. Report the result, verification and remaining limitations concisely.

### Scope and boundaries
Werner approved this adaptation after the team review. Keep reusable guidance in crewbook, using cb-* identities and ordinary GFM. Executable tools, runtime provisioning and policy enforcement stay in WorkHarbor. Do not install upstream plugins, lifecycle hooks, proxies or global settings. Guidance remains subordinate to the consuming project's instructions and supervisor policy.

Use focused atomic Conventional Commits, verified wstein author/committer identity and required Co-Authored-By trailers. Apply required checks and independent review; no push is authorized by this work item. Record upstream inspiration and pin source revisions if text is copied, retaining applicable notices.

### Inspiration
https://github.com/DietrichGebert/ponytail/blob/main/skills/ponytail/SKILL.md

### Related work
Canonical prompt owner: this issue. Related adaptations: [crewbook #12](https://github.com/wstein/crewbook/issues/12), [crewbook #13](https://github.com/wstein/crewbook/issues/13) and [crewbook #15](https://github.com/wstein/crewbook/issues/15). Controlled rollout evidence: [crewbook #14](https://github.com/wstein/crewbook/issues/14). Existing foundations: [crewbook #1](https://github.com/wstein/crewbook/issues/1)-[crewbook #4](https://github.com/wstein/crewbook/issues/4); Codex integration has priority in [crewbook #9](https://github.com/wstein/crewbook/issues/9).


#### Research-informed acceptance additions

- [ ] Before choosing an implementation, inspect a small set of relevant, nonredundant repository examples and their tests; explain the applicable behavior, API or policy boundary and why each example fits. Skip irrelevant examples rather than force a fixed example count. Clarify inputs, outputs, boundary cases and security requirements with focused test cases or equivalent acceptance examples where useful; do not treat generated tests as an independent correctness oracle or prescribe tests for every trivial change.
- [ ] Include review examples showing useful reuse, a superficially similar but inapplicable example, and an edge case missed by an initial implementation. Keep examples subordinate to project policy, retain applicable provenance, and coordinate their controlled evaluation with [crewbook #14](https://github.com/wstein/crewbook/issues/14). Describe adaptation to repository-level Go tasks and current clients/models as unmeasured until evaluated; this item adds guidance, not a retrieval engine or new platform dependency.

Research basis: [AceCoder (2024), §§3–6](https://ligechina.github.io/My%20Papers/2024%20-%20TOSEM%20-%20AceCoder-%20An%20Effective%20Prompting%20Technique%20Specialized%20in%20Code%20Generation.pdf) (DOI `10.1145/3675395`), [author full text](https://ligechina.github.io/My%20Papers/2024%20-%20TOSEM%20-%20AceCoder-%20An%20Effective%20Prompting%20Technique%20Specialized%20in%20Code%20Generation.pdf). The study combines relevant-example retrieval, redundancy filtering and requirement/test/code examples, with improvements on standalone Python/Java/JavaScript benchmarks. Applying those practices to our Go repository guidance is a design inference; repository-scale/current-model transfer was not established by this paper.

## crewbook 12

Source: [Add root-cause and coherent-change checks to coding and review skills](https://github.com/wstein/crewbook/issues/12).

Team recommendation: **5/5**. Borrow and adapt Ponytail.

A small symptom patch can leave sibling callers broken. Add bounded root-cause checks to coding and independent review workflows.

### Acceptance criteria
- [ ] In cb-code, locate the responsible behavior, inspect relevant callers and trace the actual failing path before editing. Keep investigation proportional to the issue rather than mandate an exhaustive repository audit.
- [ ] Prefer one correctly placed fix for shared behavior; cover all callers required by the acceptance criteria while respecting layer ownership and existing adapter contracts.
- [ ] In cb-review, assess the reproduced trigger, cause, affected callers, invariants and meaningful verification. Judge correctness and coherent scope rather than file counts or deleted lines.
- [ ] Preserve the separation between author and independent reviewer established in [crewbook #4](https://github.com/wstein/crewbook/issues/4). This checklist does not replace security review or required tests.
- [ ] Include examples of a shared-cause fix and a case where separate layer-specific checks are necessary; demonstrate how to report genuinely deferred callers or gaps.
- [ ] Reference the canonical starting prompt rather than duplicate or override its safety rules, and coordinate native role descriptors with [crewbook #9](https://github.com/wstein/crewbook/issues/9)/#10.

### Recommended starting prompt

> Implement the smallest complete solution to the stated requirements. Read the affected flow first. Reuse existing code and prefer standard-library or native features. Avoid speculative abstractions and dependencies. Preserve readability, validation, authorization, isolation, error handling and required tests. Report the result, verification and remaining limitations concisely.

### Scope and boundaries
Werner approved this adaptation after the team review. Keep reusable guidance in crewbook, using cb-* identities and ordinary GFM. Executable tools, runtime provisioning and policy enforcement stay in WorkHarbor. Do not install upstream plugins, lifecycle hooks, proxies or global settings. Guidance remains subordinate to the consuming project's instructions and supervisor policy.

Use focused atomic Conventional Commits, verified wstein author/committer identity and required Co-Authored-By trailers. Apply required checks and independent review; no push is authorized by this work item. Record upstream inspiration and pin source revisions if text is copied, retaining applicable notices.

### Inspiration
https://github.com/DietrichGebert/ponytail/blob/main/AGENTS.md

### Related work
Reuse the canonical prompt from [crewbook #11](https://github.com/wstein/crewbook/issues/11). Use [crewbook #13](https://github.com/wstein/crewbook/issues/13) for findings and handovers, [crewbook #15](https://github.com/wstein/crewbook/issues/15) for honest limitations, and [crewbook #14](https://github.com/wstein/crewbook/issues/14) for evaluation.

## crewbook 13

Source: [Standardize concise, precise role handovers and review reports](https://github.com/wstein/crewbook/issues/13).

Team recommendation: **4.3/5**. Adapt Caveman.

Make reports easy to act on while retaining the evidence needed for human oversight. Use concise grammatical prose and role-appropriate structured handovers.

### Acceptance criteria
- [ ] Define a compact handover contract: outcome, commits or files, verification and decisive evidence, remaining limitations or uncertainty, and next action when one is required.
- [ ] Preserve exact commands, paths, identifiers, numbers, units, error excerpts, negations, exceptions and relevant conditions. Redact secrets and never present sanitized excerpts as raw evidence.
- [ ] Define review findings with location, trigger, consequence, evidence and recommended correction. Expand security or complex findings when a single line would hide the trust boundary or consequence.
- [ ] Use normal grammar; avoid forced fragments, invented abbreviations, personas and rigid word-count or one-line limits. Required progress updates and clear explanations of blockers remain intact.
- [ ] Clearly distinguish committed, tested, reviewed, queued, runtime-measured and still-unverified work; a queue handoff is not completed implementation.
- [ ] Adapt cb-* coordinator/worker/reviewer handovers and the GFM manual consistently, preserving [crewbook #4](https://github.com/wstein/crewbook/issues/4)'s role separation and consuming-project policy. Persisted issues, commits and documentation remain sufficiently detailed.
- [ ] Provide concise example handovers and a security finding demonstrating that shortening preserves meaning and evidence. Reference the recommended starting prompt.

### Recommended starting prompt

> Implement the smallest complete solution to the stated requirements. Read the affected flow first. Reuse existing code and prefer standard-library or native features. Avoid speculative abstractions and dependencies. Preserve readability, validation, authorization, isolation, error handling and required tests. Report the result, verification and remaining limitations concisely.

### Scope and boundaries
Werner approved this adaptation after the team review. Keep reusable guidance in crewbook, using cb-* identities and ordinary GFM. Executable tools, runtime provisioning and policy enforcement stay in WorkHarbor. Do not install upstream plugins, lifecycle hooks, proxies or global settings. Guidance remains subordinate to the consuming project's instructions and supervisor policy.

Use focused atomic Conventional Commits, verified wstein author/committer identity and required Co-Authored-By trailers. Apply required checks and independent review; no push is authorized by this work item. Record upstream inspiration and pin source revisions if text is copied, retaining applicable notices.

### Inspiration
https://github.com/JuliusBrussee/caveman/blob/main/skills/caveman/SKILL.md

### Related work
Reuse the canonical prompt from [crewbook #11](https://github.com/wstein/crewbook/issues/11). Coordinate root-cause findings with [crewbook #12](https://github.com/wstein/crewbook/issues/12) and limitation reporting with [crewbook #15](https://github.com/wstein/crewbook/issues/15); evaluate meaning preservation in [crewbook #14](https://github.com/wstein/crewbook/issues/14).


#### Research-informed acceptance additions

- [ ] Provide two reporting examples: a familiar, well-specified task with a compact outcome/evidence handover, and an unfamiliar or ambiguous task with enough explanation of alternatives, assumptions and validation to support a decision. Allow the report to expand when exploration or a security finding requires it; do not impose universal brevity, automatic mode classification or a one-line rule. Preserve the existing handover contract and exact evidence in both examples.
- [ ] Evaluate concise reports for factual and semantic completeness against the source artifact and human rubric in [crewbook #14](https://github.com/wstein/crewbook/issues/14), including a verbose report that obscures the outcome and a short report that drops a decisive condition. A fluent or confident judge score alone cannot establish that meaning was preserved.

Research basis: [Grounded Copilot (2023), §§3–6](https://arxiv.org/pdf/2206.15000) (DOI `10.1145/3586030`), [author preprint](https://arxiv.org/pdf/2206.15000); [LLM-as-a-Judge in SE (2025), §§5–6](https://arxiv.org/pdf/2502.06193) (DOI `10.1145/3728963`), [author preprint](https://arxiv.org/pdf/2502.06193). Grounded Copilot's 20-participant qualitative study distinguishes acceleration and exploration; adapting this to handover detail is an inference, not an experimentally validated reporting policy. The judge study reports weak summary alignment and a verbosity-bias case; that does not imply all explanations should be shortened.

## crewbook 14

Source: [Evaluate prompt adaptations on controlled Go and security tasks](https://github.com/wstein/crewbook/issues/14).

Team recommendation: **5/5**. Adapt the benchmark approach.

Establish whether the approved guidance improves our workflows before broad rollout or publishing savings claims. Upstream benchmarks do not verify our Go tasks, clients or model bindings.

### Acceptance criteria
- [ ] Compare current crewbook without the additions, plain concise-output guidance, YAGNI alone, the upstream YAGNI plus one-liners comparator and the approved guarded starting prompt. Test rule additions separately where practical. The unsafe comparator is an isolated experimental arm, never a production default.
- [ ] Use representative pinned Go fixtures and security cases: shared caller bugs, adapter boundaries, path traversal/symlinks, authorization/config injection, process cleanup and concurrency. Score acceptance-criterion completion and adverse outcomes before efficiency.
- [ ] Use fresh isolated contexts, matched task descriptions/tools/permissions/budgets, repeated paired runs and randomized arm order. Record repository/client/model/reasoning settings and hashes of the actual loaded instructions; exclude plugin/cache contamination.
- [ ] Honor verified Codex role/model bindings from [crewbook #9](https://github.com/wstein/crewbook/issues/9). Report unavailable models or provider capabilities explicitly; do not invent aliases or aggregate unlike client configurations as a single result.
- [ ] Score correctness, meaningful checks, policy violations, review findings, completeness and human review effort before total billed cost, input/output/reasoning tokens, cache effects, latency, retries or diff size. Unknown metrics are unknown rather than zero.
- [ ] Provide a plain-Go, credential-free fixture/scoring self-check consistent with [crewbook #8](https://github.com/wstein/crewbook/issues/8)/#5. Keep executable evaluation tooling outside the declarative runtime package; host/runtime enforcement and agent execution remain workharbor-owned. Coordinate ownership explicitly before implementation.
- [ ] Live model runs are separately operator-triggered, bounded and use approved credential paths inside isolated environments. CI runs deterministic offline checks and must not require subscriptions, secrets, paid inference or unsafe permissions.
- [ ] Preserve sanitized reproducible evidence, pinned manifests, failed/missing runs, sample sizes and limitations. Report a gate for adopting or revising each prompt rule; no claim of universal safety or transferable upstream savings.

### Recommended starting prompt

> Implement the smallest complete solution to the stated requirements. Read the affected flow first. Reuse existing code and prefer standard-library or native features. Avoid speculative abstractions and dependencies. Preserve readability, validation, authorization, isolation, error handling and required tests. Report the result, verification and remaining limitations concisely.

### Scope and boundaries
Werner approved this adaptation after the team review. Keep reusable guidance in crewbook, using cb-* identities and ordinary GFM. Executable tools, runtime provisioning and policy enforcement stay in WorkHarbor. Do not install upstream plugins, lifecycle hooks, proxies or global settings. Guidance remains subordinate to the consuming project's instructions and supervisor policy.

Use focused atomic Conventional Commits, verified wstein author/committer identity and required Co-Authored-By trailers. Apply required checks and independent review; no push is authorized by this work item. Record upstream inspiration and pin source revisions if text is copied, retaining applicable notices.

### Inspiration
https://github.com/DietrichGebert/ponytail/blob/main/benchmarks/results/2026-06-18-agentic.md

### Related work
Evaluate the canonical prompt and adaptations from [crewbook #11](https://github.com/wstein/crewbook/issues/11), [crewbook #12](https://github.com/wstein/crewbook/issues/12), [crewbook #13](https://github.com/wstein/crewbook/issues/13) and [crewbook #15](https://github.com/wstein/crewbook/issues/15). Plain-Go maintenance tooling: [crewbook #8](https://github.com/wstein/crewbook/issues/8); offline CI: [crewbook #5](https://github.com/wstein/crewbook/issues/5); pinned Codex support: [crewbook #9](https://github.com/wstein/crewbook/issues/9).


#### Research-informed acceptance additions

- [ ] Where [crewbook #11](https://github.com/wstein/crewbook/issues/11)'s example guidance is evaluated, separate relevant-example selection and requirement/edge-case clarification from the combined prompt, holding other task/tool/budget conditions matched. Pin and record the example corpus and selection; exclude held-out answers and evaluation-oracle leakage, report possible training contamination and include irrelevant/nonredundant-example cases. Generated tests remain candidate artifacts, with independent hidden checks or human-validated expectations used for correctness.
- [ ] Keep functional correctness, security/policy outcomes and confidence separate. Include code that passes ordinary tests while violating a trust boundary, and record unsupported claims of safety or confidence against independently established defects. Use human-calibrated security review and executable adversarial checks; neither self-confidence nor LLM consensus replaces security evidence. Treat the older codex-davinci-002 user study as a motivation for this control, not a claim about current Codex or our approved bindings.
- [ ] If an LLM judge is used, pin its model/effort, rubric, prompt and scoring method, calibrate it against blinded human ratings and executable outcomes on representative held-out tasks, and report agreement, false accepts/rejects and disagreements by task and severity. Prefer independently rubric-scored candidates as the baseline; if pairwise judging is used, reverse candidate order and report consistency. Include verbosity and omitted-condition cases, preserve missing/invalid judgments and adjudicate consequential disagreements. A high aggregate correlation is not a security clearance or a substitute for required independent review. CI self-checks use offline fixtures; live judge calls require separate operator authorization.
- [ ] Measure generated-program execution time separately from model-generation latency, token/billed cost, human review effort and coordination overhead. Benchmark only correctness-qualified outputs with repeated runs, pinned inputs/toolchain/environment and stated warmup/load controls; report failures and missing runs separately, the common-pass denominator and resulting selection bias, and any regressions hidden by aggregate speedup. Do not substitute LOC, model size or output length for execution efficiency. Report distributions/uncertainty and task-specific adoption gates; no universal brevity, safety, speedup or more-agent-benefit claim follows from these papers.

Research basis: [AceCoder (2024), §§3–6](https://ligechina.github.io/My%20Papers/2024%20-%20TOSEM%20-%20AceCoder-%20An%20Effective%20Prompting%20Technique%20Specialized%20in%20Code%20Generation.pdf) (DOI `10.1145/3675395`); [Do Users Write More Insecure Code with AI Assistants? (2023), §§3–7](https://arxiv.org/pdf/2211.03622) (DOI `10.1145/3576915.3623157`), [author full preprint](https://arxiv.org/pdf/2211.03622); [LLM-as-a-Judge in SE (2025), §§4–6](https://arxiv.org/pdf/2502.06193) (DOI `10.1145/3728963`), [author preprint](https://arxiv.org/pdf/2502.06193); [On Evaluating the Efficiency of Source Code Generated by LLMs (2024), §2–3](https://happygirlzt.com/publications/forge24.pdf) (DOI `10.1145/3650105.3652295`), [author full text](https://happygirlzt.com/publications/forge24.pdf).

Evidence versus inference: the security study analyzed 47 participants using codex-davinci-002 and found worse security on several tasks alongside greater perceived security; it did not test today's client/models. The judge study used 450 responses across three tasks; strong output-based individual scoring in translation/generation coexists with weak summary alignment and order-sensitive pairwise judgments. The efficiency paper measures Python/C++ execution on correctness-qualified subsets, using repeated simulator/platform measurements; its speedup convention clips failed/slower optimizations to 1. These motivate our controls; Go/runtime/security/calibration results remain to be measured. Separating failures, reporting selection bias and testing judge false accepts are our evaluation requirements, not claimed paper implementations.

## crewbook 15

Source: [Record material limitations with concrete revisit triggers](https://github.com/wstein/crewbook/issues/15).

Team recommendation: **4/5**. Adapt Ponytail.

Keep deliberate simplifications visible without creating speculative debt paperwork. Document a limitation only when it affects a real acceptance criterion, measured constraint or known operating boundary.

### Acceptance criteria
- [ ] Add concise guidance for recording a material limitation, its current boundary/evidence, a concrete measurable revisit trigger and a plausible next step in the existing issue, design record or nearby comment.
- [ ] Use examples such as measured lock contention or dataset size; avoid triggers like later, if needed, or a speculative future architecture. State an unmeasured threshold or assumption explicitly.
- [ ] Avoid mandatory branded markers, a new debt ledger or boilerplate notes on every shortcut. Reuse the project's existing issue and design records.
- [ ] Distinguish an acceptable deliberate tradeoff from an unmet acceptance criterion or security defect. Missing safeguards remain findings or tracked work and cannot be waived by a limitation note.
- [ ] Add the guidance to relevant cb-code/cb-review handovers and the GFM manual, with the canonical starting prompt and precise reporting contract as references.
- [ ] Verify examples include an actual ceiling, an actionable trigger and honest verification status; preserve consuming-project design ownership and security policy.

### Recommended starting prompt

> Implement the smallest complete solution to the stated requirements. Read the affected flow first. Reuse existing code and prefer standard-library or native features. Avoid speculative abstractions and dependencies. Preserve readability, validation, authorization, isolation, error handling and required tests. Report the result, verification and remaining limitations concisely.

### Scope and boundaries
Werner approved this adaptation after the team review. Keep reusable guidance in crewbook, using cb-* identities and ordinary GFM. Executable tools, runtime provisioning and policy enforcement stay in WorkHarbor. Do not install upstream plugins, lifecycle hooks, proxies or global settings. Guidance remains subordinate to the consuming project's instructions and supervisor policy.

Use focused atomic Conventional Commits, verified wstein author/committer identity and required Co-Authored-By trailers. Apply required checks and independent review; no push is authorized by this work item. Record upstream inspiration and pin source revisions if text is copied, retaining applicable notices.

### Inspiration
https://github.com/DietrichGebert/ponytail/blob/main/AGENTS.md

### Related work
Reuse the canonical prompt from [crewbook #11](https://github.com/wstein/crewbook/issues/11) and the handover contract from [crewbook #13](https://github.com/wstein/crewbook/issues/13); coordinate root-cause gaps with [crewbook #12](https://github.com/wstein/crewbook/issues/12) and evaluation evidence with [crewbook #14](https://github.com/wstein/crewbook/issues/14).

## crewbook 16

Source: [Prevent dispatcher stalls from missed handbacks and stale capacity](https://github.com/wstein/crewbook/issues/16).

Werner asks desk to supervise dispatch so it does not get stuck, and authorizes a new work item where needed. This item addresses completed handbacks, reviewer handoffs and idle capacity; it is distinct from coordinator/leaf ownership ([crewbook #4](https://github.com/wstein/crewbook/issues/4)) and WorkHarbor's context-budget issue (#271).

### Observed problem
The dispatch thread ended with ongoing children and subsequently appeared idle while the workharbor #281 reviewer had already completed. That reviewer posted a clean scoped review but reported an outstanding Opus requirement even though Werner explicitly substituted sol6.1 medium for Opus. Previous handbacks also left reusable worker slots occupied by completed agents. An idle coordinator is not inherently a fault; the failure is leaving a known actionable completion without handling it or a documented wait/resume path.

Review record: https://github.com/wstein/workharbor/issues/281#issuecomment-5980221322. Workharbor #262 and crewbook [crewbook #9](https://github.com/wstein/crewbook/issues/9) illustrate the parallel author/design handoffs that must remain owned and visible. These observations describe coordination state, not a source-code review or proof of an autonomous scheduler fault.

### Acceptance criteria
- [ ] Add a concise cb-dispatch supervision cycle: consume available completions, validate handbacks, reconcile owned card/worktree state, route findings or independent review, reclaim completed thread slots, and select the next eligible task before waiting.
- [ ] Maintain a compact run registry identifying issue, role, agent/thread, worktree, allowed file scope, exact model/effort, current phase, last substantive progress and next awaited artifact. Reconstruct safely from issue/card records after context turnover; never infer an author is gone from silence alone.
- [ ] Define handling for a landed author, a paused author, a failed start, a completed clean review, review findings and a thread-limit rejection. Close completed agents only after preserving their handback; retry failed starts after capacity is reclaimed without duplicate claims.
- [ ] In BOTH repositories, every landed In review handback enters the compact registry and gets an independent scoped review immediately when a review slot is free. Review an immutable exact SHA with bounded file/issue scope; do not wait for blanket [crewbook #5](https://github.com/wstein/crewbook/issues/5) CI or unrelated author completion. Hold only for a named concrete validation dependency and record the missing artifact. Crewbook [crewbook #1](https://github.com/wstein/crewbook/issues/1)–[crewbook #4](https://github.com/wstein/crewbook/issues/4) review starts at frozen f6aadff without inspecting [crewbook #6](https://github.com/wstein/crewbook/issues/6)'s mutable work; [crewbook #8](https://github.com/wstein/crewbook/issues/8) already has an active exact frozen medium review and must not receive a duplicate.
- [ ] Honor the human's explicit model substitutions. Verify the actual independent review model/strength and exact reviewed SHA; tool branding or an obsolete Opus/Sonnet label must not create a false dependency. A real missing review or finding still blocks Ready to push.
- [ ] Keep board reads/writes scoped through WorkHarbor's board adapter; reconcile stale In progress claims and provisional Todo cards only after verifying ownership. Never change Ready to push without the independent clean review record or Done without the established close flow.
- [ ] Count occupied source-worker/worktree slots by the repository being edited, not the issue tracker: Werner explicitly provides three total source-code worker slots, with at most two source-code workers per repository (WorkHarbor: at most two; crewbook: at most two). Keep all three slots occupied while eligible work exists, refilling a released slot immediately after processing the author handback and preserving review ownership. Independent review/design readers do not consume source-worker slots. Preserve one writer per worktree and disjoint file boundaries, including generated inventory, build/module files and documentation; allocate a second owned worktree only with an explicit scope/ownership record. This three-total/two-per-repository rule replaces the prior global two-source-worker cap as written here. Keep crewbook [crewbook #9](https://github.com/wstein/crewbook/issues/9) Codex P1 ahead of lower-priority work. A completed handback or idle reader is not an editing author. Later annotation (H164, H175): the per-repository two and cross-repository three above are now defaults, not limits; there is no upper cap on code workers and the desk accepts whatever count the human demands; host controls still apply. The record above is kept as written.
- [ ] Wait on named active work and its next expected artifact, with bounded checks rather than busy polling. When progress stalls, inspect the actual tool/test state and send a narrow unblock request; do not interrupt healthy long-running checks or manufacture an ETA.
- [ ] Define valid stop conditions: no eligible work, a concrete external dependency, or an explicit human pause. If context limits require ending, write a resume note naming ownership, pending completions and the next action, then notify desk through the existing channel. Coordinate workharbor #271 rather than duplicate its budget decision.
- [ ] Add deterministic, credential-free scenario checks for a completion arriving while dispatch is idle, a clean review with a legacy model label, a full thread pool containing completed children, failed starts, stale card ownership and two independent pending handbacks. Verify no duplicate claim, lost review, unauthorized status transition or missing runnable continuation. Plain Go maintenance checks coordinate [crewbook #8](https://github.com/wstein/crewbook/issues/8)/#5; no paid/live model run is required.
- [ ] Demonstrate recovery of at least one observed handback and provide a concise outcome/evidence/remaining report using [crewbook #13](https://github.com/wstein/crewbook/issues/13); do not mark complete merely by documenting a loop.

- [ ] Review handoff is automatic within the authorized dispatch session for both repositories: after a landed author handback, assign a fresh independent reviewer as soon as a review slot is available, recording issue/scope, immutable SHA, reviewer model and owner. Drain existing In review cards before treating the queue as idle. Do not defer a completed content review until all unrelated CI/package work finishes; document any genuine validation dependency explicitly and separate scoped content review from final integration review. Scenario checks cover crewbook [crewbook #1](https://github.com/wstein/crewbook/issues/1)-[crewbook #4](https://github.com/wstein/crewbook/issues/4) and [crewbook #8](https://github.com/wstein/crewbook/issues/8) as well as WorkHarbor handbacks.

- [ ] Werner's explicit empty-queue rule: when the eligible dispatch queue becomes empty, send desk one concise request for more work items, stating completed work, active ownership and any blocked dependencies. Distinguish an empty queue from a full worker pool or a backlog blocked on a decision. Drain pending handbacks/reviews first; ask once per empty-queue transition, then wait for new work or the human's response without polling or repeated requests. Reflect this behavior in cb-dispatch and the GFM manual.

- [ ] Werner's explicit hourly design round: while an authorized coordination session is active, dispatch schedules a single design batch when an hour has elapsed since the last design start. Design prioritizes and assigns lanes to existing work items, resolves dependency/ownership questions, and hands dispatch a ready queue with concrete scope/file boundaries and prerequisites so free worker slots can be filled. Dispatch starts the workers; design does not create competing worker ownership. Record the last design start, next due time and ready-queue handoff in the compact registry/resume note. Preserve the existing one-design-owner and at-most-once-an-hour rules, including the blocked-P1 exception. Do not start an empty design batch solely to satisfy the clock; when no eligible work can be prepared, request more work through desk once. This coordinates human-authorized agent work, not an unattended supervisor backlog, forge-triggered run, new daemon or paid scheduler. Update the cb-design/cb-dispatch responsibilities and GFM manual consistently.

- [ ] Werner's milestone transition rule: prepare milestone 3's eligible queue while Codex is finishing, and start its work immediately when full Codex support is independently ready. Use available author slots and approved lane/file boundaries, ahead of Antigravity or nonblocking backlog. Do not wait for the next hourly design round if the work is already routed; do not treat a mocked adapter foundation as full Codex readiness.

### Scope and boundary
Crewbook owns reusable dispatcher instructions and lifecycle examples, following [crewbook #4](https://github.com/wstein/crewbook/issues/4) and the approved simplicity/precise-handover guidance in [crewbook #11](https://github.com/wstein/crewbook/issues/11)-[crewbook #13](https://github.com/wstein/crewbook/issues/13). Executable board, runtime, event notification and enforcement tools remain in WorkHarbor; any needed host implementation gets an explicit file boundary before work begins. No new daemon, timer, autonomous backlog runner, credential access or push is authorized. Preserve D40's human-start requirement. New related work items remain in crewbook.

Use focused atomic Conventional Commits and verified wstein author/committer identity, required Co-Authored-By trailers/checks and independent review. Dispatch/design should assign the priority and lane; this coordination improvement must not preempt the explicitly prioritized Codex implementation.


#### Research-informed acceptance additions

- [ ] Evaluate coordination as well as the final artifact using the existing offline handback/recovery scenarios: record lost or duplicate handbacks/claims, ownership or dependency conflicts, review omissions, unauthorized transitions, runnable-continuation failures and recovery steps. Where timings are available, distinguish queue wait, completion-to-review handoff and idle time despite eligible work; otherwise report them as unknown. Persist exact inputs, expected state transitions and observed outcomes so recovery can be reproduced.
- [ ] Coordinate with [crewbook #14](https://github.com/wstein/crewbook/issues/14) to compare the existing supervision cycle with the revised one under matched task/model/tool/budget conditions when separately authorized; assess task completion, security, human intervention and communication/retry overhead before claiming benefit. Vary agent count or role allocation only within Werner's active limits and record the allocation. Literature discussion and deterministic scenario checks do not establish that more agents, a particular architecture or a shorter handover improves real model runs. No new daemon, live inference or unattended scheduler is authorized.

Research basis: [LLM-Based Multi-Agent Systems for SE (2025), §§4–6](https://arxiv.org/pdf/2404.04834) (DOI `10.1145/3712003`), [author full preprint](https://arxiv.org/pdf/2404.04834). The review covers 71 studies, with two ChatDev/GPT-3.5-turbo game cases; Tetris still lacked row removal after ten attempts. Its collaboration metrics and human-intervention proposals are a research agenda, not a validated optimal architecture. Applying them to handback/review recovery is our bounded inference, preserving the existing supervision scope and human-start rule.
