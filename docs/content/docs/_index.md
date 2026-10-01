---
title: Documentation
cascade:
  type: docs
---

Design and reference documentation for workharbor. The project is in the design phase: the domain layer is implemented test first, and there is no runnable service yet.

{{< cards >}}
  {{< card link="design" title="Design" subtitle="Architecture, domain model, security, CLI and delivery plan." icon="book-open" >}}
  {{< card link="threat-model" title="Threat model" subtitle="Assets, trust boundaries, threats, controls and accepted risks." icon="shield-check" >}}
  {{< card link="manual" title="Manual" subtitle="Prepare the Mac mini, and the security notes for running agents." icon="book-open" >}}
{{< /cards >}}

## Planned CLI

```bash
whr login --server <url>
whr run <issue-url>
whr ls
whr logs <task> -f
whr say <task> "use the existing retry helper"
whr inbox
whr approve <decision>
```

Command names are provisional.

## Stack

Go, a single static binary, SQLite, and a server-rendered web UI built with `templ`, htmx and server-sent events.

## License

[EUPL-1.2](https://github.com/wstein/workharbor/blob/main/LICENSE)
