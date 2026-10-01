# Security policy

## Status

workharbor has no released versions yet, so there are no supported releases. Security-relevant material today is the [design](docs/content/docs/design/_index.md) and the [threat model](docs/content/docs/threat-model.md), the code that enforces them (the runtime adapter and its mount checks, the egress proxy, hardened host git, the policy guard, redaction, the configuration's secret handling), the CI workflows and the build tooling.

## Reporting a vulnerability

Please **do not open a public issue** for a security problem.

Report it privately through GitHub: open the [Security tab](https://github.com/wstein/workharbor/security/advisories/new) and choose **Report a vulnerability**. Include what you found, where, and how to reproduce it.

This is a small project maintained by one person. Reports are handled on a best-effort basis: expect an acknowledgement, not a guaranteed timeline. Please allow reasonable time to fix an issue before disclosing it publicly.

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

