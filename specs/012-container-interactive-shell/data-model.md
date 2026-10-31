# Feature 012 data model

Status: proposed; no new entity. These are the additions to the [Feature 008 data model](../008-workspace-sandbox/data-model.md)
that [spec.md](spec.md) lists under "Changes to existing documents". Field names are proposals until the amendments of T022 land.

| Entity | Addition | Source |
| --- | --- | --- |
| ExecutionInstance | failure `reason` (closed set below), set only with state `failed` | FR-014 |
| ExecutionInstance | recorded status: the client exit status; when readable and different, also the shell (exec process) status | FR-014, D8d |
| ExecutionInstance | on a `running` record: owner process id and start time, for the reconciliation | FR-024 (author proposal) |
| ExecutionInstance | `ready` to `running` is an atomic conditional update (applies only if the state is still `ready`) | FR-014, D8c |
| Plan | working directory; login shell (`/bin/sh` for job profiles, the manifest `human_shell` for `developer`) | FR-008, D3 |
| ProviderCapabilities | `interactive_shell` value `pty` or `none` (field from Feature 011 A1) | FR-003, FR-004 |

No session content, environment or terminal bytes are stored; only the status and reason fields and the `whr:` lines (FR-014).

## State transitions used

| From | To | Trigger | Text change |
| --- | --- | --- | --- |
| `ready` | `running` | shell started (commit point, atomic) | "exec started" also covers a shell started |
| `running` | `finished` | exec process ended (FR-016 row 3) | none |
| `running` | `failed` | client cannot be launched, container or daemon lost, signal after the commit point, end not established, reconciliation | widened beyond "cancellation, crash or deadline" |

No transition returns to `ready`. Every other source state is `refused.invalid_state`. `finished` allows no second shell (D5).

## Failure reasons

`transport_lost`, `container_lost`, `end_undetermined`, `interrupted`, `start_failed`, `session_lost` (author proposal for the
field and values; job profiles export `partial=true` from `failed`, FR-014).

## End-of-session outcomes (FR-016)

| Case | Exit | Instance | `whr:` line |
| --- | --- | --- | --- |
| Daemon unreachable or query late (5 s) | 72 | `failed`/`transport_lost` | yes |
| Container stopped or removed | client status | `failed`/`container_lost` | no |
| Container running, exec ended | client status | `finished` | no |
| Container running, exec running or end unknown | 72 | `failed`/`end_undetermined` | yes |
