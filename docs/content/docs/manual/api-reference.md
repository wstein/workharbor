---
title: API reference
description: The provisional HTTP API contract and its relationship to CLI JSON output.
weight: 12
toc: true
---

{{< status unverified >}} **Provisional until the beta: the API is not a stable
promise yet.** This reference is rendered from `internal/api/openapi.json`, the
same document embedded in `whr serve` and available at `/v1/openapi.json` over
the supervisor's private unix socket. The docs build publishes that file
unchanged; it describes the API, not a public supervisor endpoint. See
[Run the supervisor](run-the-supervisor.md) for access and command exceptions,
and [the scripting contract](../design/interfaces.md#92-scripting-contract)
for the intended interface.

## CLI JSON today

Successful API-backed commands with `--json` print the response envelope:
`schema_version: 1`, `ok: true` and route-specific `data`. The route responses
below describe that data. JSON escaping may change how control characters are
written without changing their decoded value.

The current implementation has these provisional differences from the intended
scripting contract; they were checked against source and focused CLI tests,
not a released supervisor:

- **No `--jsonl` flag exists yet.** Use `whr logs <task> --json` for one event per
  line, or add `-f` to follow. Both modes print `seq`, `kind`, `tier`, `at` and
  `data`, without `schema_version` or an envelope. Compared with the
  `Event` schema below, the CLI omits the required `task_id` and always emits
  `seq` and `data`, including zero and null values. Do not validate these lines
  as complete API `Event` objects.
- **`ErrorEnvelope` describes HTTP errors, not CLI stdout.** The API client
  decodes its `code`, `exit_code` and `message`; the CLI prints a human message
  on stderr and returns the exit code. Local argument and connection errors
  also use stderr. `--json` does not make these errors JSON envelopes.
- **Local commands have separate formats.** `doctor`, `version`, service status
  and GitHub App creation do not use the route response schemas. Commands
  that print paths, scripts or interactive output also differ; the supervisor
  manual lists them.

{{< api-reference >}}
