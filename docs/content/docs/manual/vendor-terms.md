---
title: Agent vendor terms
description: What the agent vendors' terms mean for a subscription login under WorkHarbor, with links to the sources.
weight: 3
toc: true
---

WorkHarbor runs the agent CLIs you already use: Claude Code with a Claude subscription, Codex with a ChatGPT plan, or either with an API key. A subscription is priced for one person using the vendor's own tools, so WorkHarbor keeps to a few rules in that mode (design [D40](../design/decisions.md)). This page says what they are and where they come from. It is our reading, not legal advice: the vendors' own pages decide, and they change.

**D57 guardrail (decided; implementation pending, #339/#341).** The [human-initiation rule](../design/security.md) requires every product start, resume, retry or message to a subscription agent to come from one direct human interaction. After an interruption the product alerts through a Decision and waits for approval; a timer, reset time, queued task or expired request never authorizes a send. The agent and its subagents may continue their own steps within a human-initiated request, and the Crewbook skill in the human's client session is outside this product rule. Vendor terms have not been checked for this rule; it is a guardrail, not a legal statement or a finding of conformity. The vendor summaries below remain {{< status unverified >}}.

## What the vendors say

**Anthropic** ([Claude Code, legal and compliance](https://code.claude.com/docs/en/legal-and-compliance), read 1 October 2026) {{< status unverified >}}, in short:

- Signing in with a Claude account (Free, Pro, Max, Team or Enterprise) serves ordinary use of Claude Code and Anthropic's own apps, and the advertised limits assume ordinary, individual use.
- A developer may not collect, store or pass on Claude.ai credentials or session tokens, and the sign-in itself has to go through Anthropic's own flow.
- You may sign in to the unmodified Claude Code binary with your own subscription, also where a platform hosts Claude Code.
- Products that build on Claude should use an API key.

**OpenAI** ([Codex authentication](https://developers.openai.com/codex/auth), [Codex with a ChatGPT plan](https://help.openai.com/en/articles/11369540-using-codex-with-your-chatgpt-plan)) {{< status unverified >}}: signing in to Codex with ChatGPT uses your plan's included usage and the ChatGPT terms; an API key is recommended for automated use such as CI/CD.

**Google Antigravity and Gemini API** ([Google Terms of Service](https://policies.google.com/terms), [Generative AI Additional Terms](https://policies.google.com/terms/generative-ai), [Gemini API Additional Terms](https://ai.google.dev/gemini-api/terms), read 1 October 2026) {{< status unverified >}}, in short:

- Signing in with a Google account (including Google One AI Premium / Gemini Advanced) covers personal interactive use of Antigravity and official developer tools.
- Automated or programmatic pipelines must use the Gemini API (with `GEMINI_API_KEY`) or Vertex AI rather than driving consumer account sessions.
- Under the Gemini API and Generative AI Terms, automated agent workflows must not automatically bypass any requests for human confirmation: human-in-the-loop oversight is an explicit requirement for agentic actions, matching WorkHarbor's policy table and blocking Decisions.
- Credentials and session tokens (`jetski-standalone-oauth-token`) must not be collected, passed on, or shared across users. Under D40, signing in with the user's own account inside an isolated environment respects individual seat boundaries.

## What WorkHarbor does in subscription mode

- **It never touches your login.** You sign in inside each environment through the vendor's own flow; the agent CLI keeps the login on that environment's volume. `whr` does not read, copy, store, pass on or log it, and refuses a subscription token in its configuration. How the sign-in works from a terminal, and whether it can be done from the phone, is still being measured (issue #82).
- **You start every run.** `whr run`, the web app, or accepting a card's request. A new issue, a comment or a webhook only lands in your inbox. There are no scheduled runs and no overnight backlog.
- **One person per supervisor.** Do not let others submit work to your WorkHarbor: that would share your seat.
- **The vendor's binaries, unmodified.** The tool store holds the published releases, checked against the vendor's checksums.
- **A quota stop is a stop.** When your usage window is used up, the run pauses and asks you; WorkHarbor never retries in a loop or switches accounts.
- **Parallel tasks are fine**, as in several terminals: they all draw on your one usage window, which the web app shows as one figure.
- **One machine talks to the vendor.** Every run reaches the vendor from your WorkHarbor host, through its egress sidecar; your phone, tablet and laptop only talk to `whr` (design D29). So signing in from several devices or places is not something WorkHarbor adds. How vendors treat a login used from several networks at once is {{< status unverified >}}.

With an API key, the usage is billed per token to your own account. The same rules apply in WorkHarbor today; the key is the only agent credential WorkHarbor itself keeps, in a `0600` file.
