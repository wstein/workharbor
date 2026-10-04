# Issue 34: nonbilling target discovery

This script reads existing Apple Container metadata and checks executable
availability in the named candidate. It creates no resources, reads no homes
or credentials, runs no vendor CLI and requests no inference. It does not use
the imported issue 174 scripts.

The retained output is an unedited capture of deliberately filtered metadata,
not raw container inspection. Host source paths and environment values are
omitted before capture; no subsequent secret redaction is applied.

On 4 October 2026, whtmp-mcp-ag1 had no Codex in PATH or at the three checked
system/tool paths. The tool mount listed only claude, probe and whr-shim.
Its network was hostOnly; the companion's configured proxy
allowlist contained only Anthropic hosts. Configuration does not establish
effective traffic enforcement. The only other listed container was the stopped
builder. No qualifying Codex target, Linux CLI artifact/version or target schema
was identified. The human's working in-container login report remains accepted;
its target identity cannot be inferred from these observations.

At source commit c9af5f17e446ff1d79f1ee21bf9795fdb45572cd,
`internal/agent/codex/adapter.go` Start and Resume return ErrUnsupported before
launch; Capabilities reports unsupported. The private offline launch builds
`<configured-bin> app-server --listen stdio://` through runtime Exec, then
initialize, initialized, thread/start or thread/resume, and turn/start.
TestProductionGate checks the refusal. The schema described in
`internal/agent/codex/testdata/README.md` came from macOS codex-cli 0.160.0,
not this target.

The product entrypoint is `whr run <issue-url> --agent <workspace>/<role>`
(`internal/cli/commands.go`), through the shared service. However,
`internal/serve/real.go` constructs only the Claude adapter. The named agent
does not select a Codex adapter. There is currently no production Codex run
entrypoint; direct app-server execution would bypass this product gate.

The first target blocker is identifying the already authorized environment
with the verified pinned Linux Codex tool and required internal-sidecar
topology. Production additionally needs adapter/service wiring and D53
isolation, child, binding and lifecycle measurements. All issue 34 acceptance
criteria remain open; discovery establishes no native support or target E2E.

Validation: shell syntax, TestProductionGate and make check passed. The latter
includes formatting, vet, lint, editorconfig, the full offline suite and race
checks. These are source checks, not live target conformance.
