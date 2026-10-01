---
title: Documentation
cascade:
  type: docs
---

Design and reference documentation for workharbor. The project is building release 1, dogfood first (D34): the domain model, the store, hardened host git, the Apple Container adapter with its egress proxy, the Claude Code adapter in degraded mode and the tool store exist and are tested; the supervisor service, `whr serve`, does not exist yet.

{{< cards >}}
  {{< card link="design" title="Design" subtitle="Architecture, domain model, security, CLI and delivery plan." icon="book-open" >}}
  {{< card link="threat-model" title="Threat model" subtitle="Assets, trust boundaries, threats, controls and accepted risks." icon="shield-check" >}}
  {{< card link="glossary" title="Glossary" subtitle="Task, topic, run, Decision, ReviewCandidate, tool store and the other terms." icon="academic-cap" >}}
  {{< card link="spikes" title="Spikes" subtitle="The measured results behind the design." icon="beaker" >}}
  {{< card link="manual" title="Manual" subtitle="Prepare the Mac mini, the security notes and the agent vendors' terms." icon="book-open" >}}
{{< /cards >}}

## Planned CLI

```bash
whr serve
whr run <issue-url>
whr ls
whr logs <task> -f
whr say <task> "use the existing retry helper"
whr inbox
whr approve <decision>
whr answer <decision> <option>
```

These are the stable commands of the first slice ([D37](design/decisions.md)); none exists yet, and every other command is provisional.

## Stack

Go, a single static binary, SQLite, and a server-rendered web UI built with `templ`, htmx and server-sent events.

## License

[EUPL-1.2](https://github.com/wstein/workharbor/blob/main/LICENSE)
