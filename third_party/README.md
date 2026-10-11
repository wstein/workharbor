# Third-party provenance

Spec Kit was initialized with the upstream CLI, not reconstructed templates:

```sh
uv run --project /path/to/spec-kit specify init anchorage --integration codex --script sh --ignore-agent-tools --non-interactive
```

Source: https://github.com/github/spec-kit
Commit: 0443760542e7e5269724744a2ac55c4bd386d095
CLI version: 1.1.4.dev0 (development snapshot, pinned by commit).
The generated `.specify/` infrastructure and `.agents/skills/` originate from this
snapshot. Its MIT notice is retained in `spec-kit/LICENSE`. The project constitution
is Anchorage-specific. Review release migration before upgrading; do not overwrite
project documents. No Symphony source is vendored. Anchorage's own license is
pending an owner decision; the third-party MIT license does not license Anchorage.
