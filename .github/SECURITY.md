# Security policy

## Status

WorkHarbor has only pre-releases so far: `v0.1.0-alpha.1`, `v0.1.0-alpha.2` and `v0.1.0-alpha.3`. Only the latest alpha receives security fixes; older alphas do not, so update to the newest one. Security-relevant material is the [design](../docs/content/docs/design/_index.md) and the [threat model](../docs/content/docs/threat-model.md), the code that enforces them (the runtime adapter and its mount checks, the egress proxy, hardened host git, the policy guard, redaction, the configuration's secret handling), the CI workflows and the build tooling.

## Reporting a vulnerability

Please **do not open a public issue** for a security problem.

Report it privately through GitHub: open the [Security tab](https://github.com/wstein/workharbor/security/advisories/new) and choose **Report a vulnerability**. Include what you found, where, and how to reproduce it.

This is a small project maintained by one person. Reports are handled on a best-effort basis: expect an acknowledgement, not a guaranteed timeline for a fix. The aim is to acknowledge every report within 14 days; this is a goal, not a promise. Please allow reasonable time to fix an issue before disclosing it publicly.

## Response process

What happens after a report arrives. The timings are goals, not promises, in line with the best-effort wording above.

1. **Triage.** The maintainer acknowledges the report (goal: within 14 days), reproduces it and judges severity and whether it is in scope. The reporter is told the outcome, and asked for more detail if needed.
2. **Private fix.** The fix is prepared out of public view, in the private temporary fork of the report's GitHub security advisory or in a draft advisory, so that nothing public points at the problem before a fix exists. The reporter may be invited to review it.
3. **Release.** The fix is released in a new alpha, the latest one being the only supported release (see Status).
4. **Advisory.** After the release, the advisory is published on GitHub with the affected and fixed versions. The reporter is credited by name or handle if they want it, and stays unnamed if they prefer.
5. **Disclosure timeline.** The goal is to publish within 90 days of the report, sooner when a fix is ready. A complex fix or a dependency on a third party can take longer; the reporter is told when that happens and the date is agreed with them where possible.

## Scope

In scope:

- flaws in the design or the code that would let an agent escape isolation, reach the host or the LAN, obtain credentials it should not have, or bypass the approval policy
- a way for `whr` to read, store or pass on a subscription login, against D40
- vulnerabilities in the repository's code, CI workflows and build or release tooling
- secrets committed to the repository

Out of scope: vulnerabilities in third-party software (Apple Container, forges, agent runners), which should be reported to their maintainers. The design's claims about those tools are unverified; corrections are welcome as normal issues.

## If a secret leaks (runbook)

For the maintainer and every agent session. A value counts as leaked once it reaches a commit, a push, an issue or chat message, a log or a command line, even if nobody has used it.

1. **Revoke it first**, at the vendor (Anthropic, Google AI Studio, OpenAI, the GitHub App's key, workharbor's own API token), before cleaning anything up. A rewrite cannot recall what was already fetched, cached or forked.
2. **Find every copy.** Without printing the value, search the working tree and every commit of every ref (`git grep -F` over `git rev-list --all`), the stashes, the other sessions' worktrees and branches, issue and pull request text, CI logs, and the agent session that saw it.
3. **Replace the source,** so the value can never come back: a script or a test reads the key from a `0600` env file named by an environment variable (AGENTS.md, Secrets).
4. **Rewrite unpushed history freely** (`git commit --fixup` with an autosquash rebase). **Rewrite pushed history only when the maintainer asks**: an interactive rebase that edits only the commit that added the value, a check that the trees differ only where the value was and that gitleaks is clean, then `git push --force-with-lease=main:<the old tip>` so a push that landed in between is never overwritten.
5. **Move every branch off the old history** with `git rebase --onto <new base> <old base> <branch>`. A plain `git rebase main` replays the old commit, because its patch differs from the rewritten one, and brings the value back. Check with `git merge-base --is-ancestor <old commit> <branch>`.
6. **Ask GitHub support to purge cached views** of the old commits if the repository is public, and record what happened (without the value) in the issue that tracks the fix.

The hooks (`make hooks`) scan every commit and every push for secrets, and `make check-ci` scans the history being merged; a finding is never worked around. The custom rules in `.gitleaks.toml` flag a literal value assigned to a credential variable (for example `GEMINI_API_KEY=` or `CLAUDE_CODE_OAUTH_TOKEN=`), as in the leak of 1 October 2026.

