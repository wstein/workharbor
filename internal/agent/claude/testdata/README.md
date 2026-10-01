# Fixtures

These streams are **constructed**, not recorded. No recording of Claude Code's
stream-json output is in the repository, so each file follows the event shapes
that spike #1 documents in `spikes/transcript/RESULTS.md` (branch
`spike/transcript`). Field values (IDs, costs, reset times) are made up.

Replace them with real recordings when they exist (issue #25). Until then the
golden tests prove the parser against the documented shapes, not against the
real CLI.
