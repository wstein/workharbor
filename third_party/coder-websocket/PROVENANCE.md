# Coder WebSocket provenance

The host agent pins github.com/coder/websocket v1.8.15. Go standard crypto, TLS,
HTTP and filesystem APIs implement identity and HTTPS; the standard library has
no WebSocket framing client. This maintained context-aware library supplies that
transport, with compression disabled and a 4096-byte message read limit. It adds
no runtime module dependencies. Exact module hashes are retained in
apps/host_agent/go.sum; preserve this ISC notice in later T023 artifact assembly.

Primary release: https://github.com/coder/websocket/releases/tag/v1.8.15
Primary security page: https://github.com/coder/websocket/security
Independent Sol review checked both on 2026-10-14 and found no advisories published
on that upstream security page. This is a bounded source/provenance check, not a
vulnerability clearance. Existing Bandit mitigation remains separately scoped.
