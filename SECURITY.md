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
