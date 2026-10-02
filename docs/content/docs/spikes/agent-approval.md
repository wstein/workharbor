---
title: "Spike #7: approvals and cancel"
description: "Routing Claude Code's permission prompts over the stdio control protocol from a container, fail-closed cases, and cancel and resume mid-turn."
weight: 3
---

> Source: [`spike/agent-approval`](https://github.com/wstein/workharbor/tree/spike/agent-approval/spikes/agent-approval) at `567b5ad2d3d33f66ac60c054d2ebbadb8a80e64b`, with the scripts and raw output next to the results. Tracks [#7](https://github.com/wstein/workharbor/issues/7), [#10](https://github.com/wstein/workharbor/issues/10). Published as recorded on 1 October 2026.
>
> **Since then:** D26 cites this run as its evidence. Case 7's resume finding is what the resume briefing, **D27**, answers.

Measured on 1 October 2026 on Mac mini (Apple silicon, 16 GiB, macOS 26.6.2) with Apple Container CLI 1.5.0, guest Linux 6.12.28 on `fedora:latest`, Claude Code 2.1.285 (linux-arm64), and `whr-shim` (static linux-arm64).

Tracks [Issue #7](https://github.com/wstein/workharbor/issues/7) (Route agent approvals from inside a container to the supervisor) and leftover items from [Issue #10](https://github.com/wstein/workharbor/issues/10) (Cancel from host mid-tool-call and session resume).

Raw execution streams (`*.jsonl`) and test output logs (`*.txt`) are in `results/`. All container resources used the `whspike-*` prefix and were cleaned up after testing.

## Summary

| Test Case | Scenario | Measured Result | Latency / Timing | Fail-Closed? | Verdict |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Case 1** | Allow roundtrip | Tool executed; `/work/allow_test.txt` created | Turn: 5.04 s; Dispatch: 0.07 ms | N/A (Approved) | **PASS** |
| **Case 2** | Deny roundtrip | Tool blocked; permission denial reported | Turn: 8.45 s; Dispatch: 0.17 ms | Yes (Blocked) | **PASS** |
| **Case 3** | Closed stdin / EOF | Stdin closed on `control_request`; agent aborted tool execution | Turn: 5.48 s | Yes (Aborted) | **PASS** |
| **Case 4** | Supervisor crash | Host `exec` killed (`SIGKILL`); guest process survived (`ALIVE:109`); killed via `whr-shim` | Reaped: 610.80 ms; 0 orphans | Yes (Unexecuted) | **PASS** |
| **Case 5** | Timeout / Deadline | 5.0 s unanswered approval; tool held; terminated via `whr-shim` | Reaped: 455.73 ms; 0 orphans | Yes (Unexecuted) | **PASS** |
| **Case 6** | Request ID validation | Mismatched `request_id` (`00000000-...`) ignored; real `request_id` deny processed | Real Deny: 0.17 ms | Yes (Ignored invalid) | **PASS** |
| **Case 7** | #10 Cancel & Resume | 30s Bash loop killed mid-turn via `whr-shim`; resumed session inspected | Reaped: 557.32 ms; 0 orphans | Cancellation: Yes; Resume: False | **FAIL** |

---

## Detailed Findings

### 1. Allow and Deny Round-Trip Protocol (Cases 1 & 2)

Claude Code supports an interactive stdin control channel when invoked with `--permission-prompt-tool stdio` in stream mode. When a tool requiring permission is called:
1. Claude Code emits a `control_request` message on stdout:
    ```json
    {
      "type": "control_request",
      "request_id": "8d44de59-8a20-4a04-a9ab-77a163304189",
      "request": {
        "subtype": "can_use_tool",
        "tool_name": "Write",
        "input": {"file_path": "/work/allow_test.txt", "content": "ALLOWED_SUCCESS"}
      }
    }
    ```
2. The supervisor inspects the request and responds on stdin with a `control_response`:
    ```json
    {
      "type": "control_response",
      "response": {
        "subtype": "success",
        "request_id": "8d44de59-8a20-4a04-a9ab-77a163304189",
        "response": {"behavior": "allow"}
      }
    }
    ```
    Or for a denial:
    ```json
    {
      "type": "control_response",
      "response": {
        "subtype": "success",
        "request_id": "270104dc-5bcf-4d5c-a215-fd0f8c669d29",
        "response": {"behavior": "deny", "message": "Denied by supervisor policy"}
      }
    }
    ```

**Measured Latency:**
- Supervisor response dispatch latency (time from receiving `control_request` line to writing `control_response` into the exec stdin pipe): **0.07 ms – 0.17 ms**.
- End-to-end turn time for an approved write: **5.04 s** (`results/case1-allow.txt`, stream in `results/case1-allow-stream.jsonl`). The tool executed and created `/work/allow_test.txt` with content `ALLOWED_SUCCESS`.
- End-to-end turn time for a denied write: **8.45 s** (`results/case2-deny.txt`, stream in `results/case2-deny-stream.jsonl`). The tool was blocked, `/work/deny_test.txt` was not created, and Claude Code emitted a `permission_denials` payload recording the denied tool use.

### 2. Stdin EOF / Lost Channel (Case 3)

When the supervisor closes the stdin pipe to `container exec` upon receiving a `control_request`:
- Claude Code detects EOF on stdin.
- The pending tool call is **not executed** (`/work/eof_test.txt` was not created).
- The agent cleanly aborts the tool call and terminates the turn in **5.48 s** (`results/case3-closed-stdin-stream.jsonl`).
- **Conclusion:** A severed stdin pipe fails closed without executing unapproved tools.

### 3. Supervisor Crash & Host Exec Client Death (Case 4)

In Apple Container 1.5.0:
- When the host `container exec` client process is killed with `SIGKILL` (`kill -9`) while an approval is pending:
  - The client process on the host terminates immediately.
  - **Empirical check of the guest process:** Checking the PID recorded in `/work/agent.pid` via `kill -0 "$pid"` confirmed the guest agent process remained alive (`ALIVE:109`). Killing the host client does **NOT** propagate a termination signal to the containerized process.
  - The agent remained blocked waiting for approval input; `/work/crash_test.txt` was **not** created.
- Recovery from the host:
  - Invoking `whr-shim kill -pidfile /work/agent.pid -grace 500ms` via a new `container exec` terminated the orphaned agent process group in **610.80 ms**.
  - Process table inspection confirmed **0 remaining orphan processes**.
  - The tool was never executed (`/work/crash_test.txt` does not exist).
- **Conclusion:** This empirically validates D25 and D26: because Apple Container does not kill guest processes when the host client disconnects, `whr-shim kill` is strictly necessary to clean up orphaned guest processes when the supervisor crashes or disconnects.

### 4. Approval Deadline & Timeout (Case 5)

- When an approval request is left unanswered for a 5.0 s deadline:
  - Claude Code remains blocked on stdin.
  - The tool is **not executed prematurely** (`/work/timeout_test.txt` was not created).
  - The supervisor aborts the turn via `whr-shim kill -pidfile /work/agent.pid -grace 500ms`, which cleanly terminates the agent in **455.73 ms**.
  - Zero orphan processes remain, and no target file was written.
- **Contract scope note:** This tests the **agent half** of the contract: Claude Code waits indefinitely without proceeding or executing tools while an approval is unanswered. Enforcing the decision deadline and cancelling the expired run is the supervisor's responsibility.

### 5. Request ID Validation (Case 6)

- When a `control_response` with a mismatched `request_id` (`00000000-0000-0000-0000-000000000000`) is sent:
  - Claude Code ignores the mismatched response and continues to wait for a matching response (`/work/reqid_test.txt` was not created).
  - When the genuine `request_id` (`decb182c-6bcc-412b-b70d-581d3581636d`) is subsequently sent with `behavior: "deny"`, Claude Code processes the denial immediately and finishes the turn cleanly.
  - **Conclusion:** The agent validates `request_id`, preventing race conditions or stale approval replay.

### 6. Mid-Tool-Call Cancellation & Session Resume (#10 Leftover) (Case 7)

A long-running execution (`for i in $(seq 1 30); do sleep 1; done && echo DONE > /work/done.txt`) was launched inside `whr-shim`:
1. While the loop was actively running (observed in `/proc/264` and `/proc/270`), the supervisor issued `whr-shim kill -pidfile /work/agent.pid -grace 500ms`.
2. Process group termination latency: **557.32 ms** (SIGINT followed by SIGKILL to the process group).
3. Post-kill inspection confirmed all subshells and sleep processes were reaped; **0 orphan processes** remained.
4. The target file `/work/done.txt` was **not created**.
5. **Process Group Cancellation:** **PASS**.
6. **Session Resume Inspection:**
    - The session was resumed with `claude -p --resume <session_id>`.
    - The resumed session received **no** system `task_notification` (`status: None`).
    - When asked what happened, the resumed model stated:
      > *"The command was never executed. The Bash tool use was rejected during the permission prompt, so the command never ran. The 30-second sleep loop and file write to `/work/done.txt` did not happen."*
    - This statement is **false**: the command was approved and ran for 3 seconds before being cancelled by `whr-shim`. Claude Code told the model that the tool was rejected before running rather than interrupted mid-execution.
    - **Resume Accuracy Evaluation:** **FAIL**. Because Claude Code tells the model a false story on resume after a cancel, a resumed agent may repeat or incorrectly skip work.
    - **Architectural Remedy (D27 / #66):** On resume after a cancel or pause, the supervisor must inject a synthetic message informing the agent which tool call was interrupted and that its partial effects are unknown.

---

## Architectural Verdict & D26 Status

1. **Routing Mechanism:** Standard bidirectional I/O over `container exec` (with stdin pipe enabled) is fast (<0.2 ms dispatch), simple, and requires no open ports or HTTP relay listeners in the sidecar.
2. **Fail-Closed Guarantees (Measured):**
    - Allow and Deny: **Measured**.
    - Stdin EOF: **Measured** (aborts cleanly).
    - Supervisor Crash: **Measured** (guest process survives host client death; reaped with `whr-shim`).
    - Unanswered Approval: **Measured** (agent waits without executing tools; supervisor enforces deadline).
    - Request ID Matching: **Measured** (agent ignores mismatched IDs).
    - Mid-Turn Process Group Cancellation: **Measured** (`whr-shim` reaps process tree in <600 ms with 0 orphans).
3. **Session Resume (#10):** **Open / Unresolved in Agent**. Claude Code does not provide accurate state to the resumed model after mid-turn cancellation; the supervisor must provide this context explicitly.
