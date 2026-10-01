# Recorded streams

These are **real** recordings of Claude Code 2.1.285 (`claude-haiku-4-5`) run in
an Apple Container guest with `--input-format stream-json --output-format
stream-json --verbose --permission-prompt-tool stdio`, taken from spike #7:
branch `spike/agent-approval`, commit `22ddc7f`, `spikes/agent-approval/results/`
(`RESULTS.md` says what each case did). They are copied unchanged.

| File | Case |
| --- | --- |
| `case1-allow.jsonl` | a `Write` the supervisor allowed through the control channel |
| `case2-deny.jsonl` | the same, denied with a message |
| `case3-closed-stdin.jsonl` | stdin closed while the approval was pending |
| `case6-reqid.jsonl` | a `control_response` with a wrong request ID is ignored |
| `case7-resume.jsonl` | a session resumed after its process was killed mid-tool-call |

They were recorded in `manual` mode with the stdio approval channel (D26), not in
`dontAsk`, so they contain `control_request` events the adapter does not act on
yet. They hold real session IDs, request IDs and a short model output; no
credential is in them (checked with gitleaks).

The fixtures one directory up (`../*.jsonl`) are constructed from the shapes the
spike described, for the cases no recording exists for (an expired login, an
exhausted quota).
