# Provider policy context

Reviewed 2026-10-13 for [Feature 004](../../specs/004-speckit-symphony-automation/spec.md).
This is dated research and proposed future work, not a provider integration, policy waiver or execution approval.
Additional providers remain outside Feature 004. Historical spec/plan approvals and task evidence remain unchanged;
new bytes do not automatically retain, renew or transfer approvals under FR-003.

## Primary-source facts

- [Anthropic Consumer Terms, §3.7](https://www.anthropic.com/legal/consumer-terms) restrict automated/non-human
  access except via an Anthropic API key or explicit permission. API-key usage is governed by Commercial Terms.
- [Claude Code legal guidance](https://code.claude.com/docs/en/legal-and-compliance) distinguishes consumer and
  commercial agreements. Product/service preinstallation or operation requires Commercial Terms unless otherwise
  agreed: the published binary and all built-in authentication methods remain intact; each user authenticates with
  their own API key, subscription credentials or supported cloud-provider credentials, with direct billing under
  their own agreement. Customers cannot pay for,
  resell or intermediate usage on users' behalf.
- [Claude Agent SDK plan notice](https://support.claude.com/en/articles/15036540-use-the-claude-agent-sdk-with-your-claude-plan),
  updated October 7, 2026, says Max/Team monthly API credits cover SDK, `claude -p`, API and Managed Agents;
  SDK, `claude -p` and third-party apps can still use subscription limits.
- [Antigravity Terms, §§6 and 8](https://www.antigravity.google/terms) prohibit third-party access to the consumer
  service and apply model-specific terms, including Anthropic Commercial Terms for Claude. The opening exception
  places listed enterprise/Workspace and Gemini Enterprise API-key access under the administrator's applicable
  agreement instead.
- [Antigravity FAQ](https://www.antigravity.google/docs/faq) describes `agy -p` as intended for direct scripts/CI;
  third-party agent/application wrapping and account rotation are unsupported under consumer plans. It recommends
  Gemini Enterprise or Google AI Studio API keys for third-party coding agents using Gemini.
- [Pinned Symphony README, steps 3–5](https://raw.githubusercontent.com/openai/symphony/be10a1b79df723d6d7612b5651c8522704dafb2e/elixir/README.md)
  describes launching Codex App Server and sending it a workflow prompt.

## Interpretation for a future review

Permission depends on the exact service, agent, authentication and execution mode. The SDK notice describes
subscription usage without specifying every third-party authentication/integration arrangement; it supports neither
a blanket ban nor universal OAuth permission. Native user authentication under the Claude Code conditions is
distinct from extracting, sharing or intermediating Claude login credentials. API/cloud inference is a separate
commercial route; it does not establish permission for every subscription wrapper.

Likewise, Gemini API access is a separate endpoint/route, not a key that unlocks consumer Antigravity for arbitrary
third-party access. An enterprise route needs review of its actual agreement. Direct `agy -p` scripting does not
establish permission for Symphony wrapping. Replacing Symphony's binary does not prove protocol compatibility,
safeguard parity or a working Claude/Antigravity adapter. None was implemented or live-verified here.

## Proposed future options

A future profile could record `(provider/backend, agent, authentication kind, execution mode, adapter,
policy evidence, project-owner opt-in)`. This tuple is a proposal, not a schema or new requirement. Provider
permission and project authorization would need separate evidence; one cannot supply the other.

The following author ratings express priority out of 10, not acceptance criteria, estimates or approvals.

| Option | Rating | Effort | Benefit and tradeoff |
| --- | --- | --- | --- |
| Explicit profile compatibility matrix | 10/10 | Low initially; recurring review | Makes supported combinations visible; policies and evidence expire. |
| Project-owner opt-in per profile/mode | 10/10 | Low–medium | Makes authorization explicit; adds preparation and renewal work. |
| Equivalent manual workflow | 9/10 | Medium | Keeps work possible without orchestration; operator effort remains. |
| Portable approval, scope and recovery guards | 9/10 | High | Preserves safeguards across routes; each route needs demonstrated parity. |
| Future commercial API/cloud adapters | 8/10 | High | Offers explicit service integration; adds cost, protocol and recovery maintenance. |
| Require every agent to use Symphony | 2/10 | High | Centralizes dispatch; narrows provider compatibility and may conflict with allowed modes. |

Any selected follow-up needs its own scoped review and authorization before requirements, plans, tasks, adapters
or runtime configuration change. Existing ownership, provider, sandbox and live-operation gates continue to apply.
