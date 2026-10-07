---
title: Per-client API credentials
description: "Proposal: several named API clients so the audit tells callers apart."
weight: 9
toc: true
---

## 5.12 Per-client API credentials (proposal)

{{< status open >}} Design note for [#364](https://github.com/wstein/workharbor/issues/364),
a follow-up to [#357](https://github.com/wstein/workharbor/issues/357). The
first slice is implemented: the `api_clients` configuration, per-client audit
actors and the `api-clients` doctor check. The rest of this note, including the
open questions below, is still a proposal.

### Threat addressed

The API accepts one shared bearer token (`api_token_file`, D29). Every caller
holds the same secret, so every action carries the same audit actor. #357
derives a short non-secret id from that token (`actorFor` in
`internal/api/server.go`, shape `api:<12 hex>`), which names the credential but
not the caller. If an automation misbehaves or a token leaks, the audit cannot
say which caller acted, and the only remedy is to replace the token for all.

### Assumption: number of callers

A handful of clients per host (at most about ten: the owner's CLI, a few
automations, perhaps a second person). Clients are listed by hand in the host
configuration. If this is wrong (dozens of clients, or clients created by
software), a file-per-client directory and a `whr` command to add, rotate and
revoke become worth their cost, and the open question on that below turns into
a requirement.

### Config shape

Today `ReadSecret` (`internal/config`) reads one file and `TokenFromConfig`
(`internal/api`) returns one token. Proposed, in the host configuration:

```json
{
  "api_token_file": "/etc/workharbor/secrets/api.token",
  "api_clients": [
    { "name": "ci-nightly", "token_file": "/etc/workharbor/secrets/ci-nightly.token" },
    { "name": "alice",      "token_file": "/etc/workharbor/secrets/alice.token" }
  ]
}
```

- One file per client, one token per file, read with `ReadSecret` (absolute
  path, no links, private mode, bounded size). Tokens never live in the
  configuration file.
- The unchanged single-token setup is the default client: `api_token_file`
  alone becomes one client named `default`, and its audit actor stays the
  #357 derived id, so existing audit rows and setups do not change.
- `api_clients` is additive. When both are present, `default` and the named
  clients are all accepted.
- `api_token_file` stays required: the configuration check requires it, and
  `ReadClientConfig` (`internal/cli/client.go`) and the doctor probe use it as
  the CLI's own credential. So the CLI keeps using `default`; other clients
  are for other callers. Whether that should change is an open question.
- Configuration check refuses a duplicate name, a duplicate token (compared by
  digest; the error names the two clients and paths only, never a digest or
  prefix), and a secret file shared with another secret setting.
- Every client token is registered with the Redactor, and `whr serve` refuses
  to start if that fails, as `Redactor` in `internal/serve/real.go` does today
  for the single token.

### Audit actor naming

- The client name becomes the actor id: `api:<name>`. The default client keeps
  `api:<12 hex>` from #357, which cannot collide with a name because names
  are restricted (below) and cannot be exactly 12 hex digits.
- Names match `[a-z][a-z0-9-]{0,31}`, are unique, and `default` is reserved.
  Names that look like the derived form (12 lowercase hex digits) are refused.
- A name is a label chosen by the operator, not a secret and not proof of a
  person. Renaming a client starts a new actor; old audit rows keep the old
  name. Tokens are never part of the actor.
- `authorized` must return the matched client so the actor is set per request;
  today `Server` holds a single `actor` field and `Actor()`.
- Old and new tokens of one client share the actor until the old one is revoked.
- Authorization stays flat: every client has the same rights as today.
  Per-client permissions are out of scope.

### Rotation

The token is read once at start (`internal/serve/serve.go`, `real.go`); only
its digest stays in memory. There is no reload, so every change needs a
restart until open question 4 is decided.

1. Create a new token file for the client (mode `0600`, new random value).
2. Point the client entry at the new file and restart `whr serve`.
3. Update the caller to send the new token.
4. Delete the old file (cleanup only).

One client accepts one token, so that client has a short outage; other clients
are unaffected. A grace window with two tokens per client is deferred.

### Revocation

Remove the client ENTRY from the configuration and restart `whr serve`. That is
the only revocation step. Until the restart the old digest stays valid in
memory. After it, the token gets the same plain 401 as any wrong token. Delete
the file afterwards as cleanup: deleting it first while the entry is still
present makes `whr serve` refuse to start (`checkSecretFile`). Revoking
`default` means removing `api_token_file`, which the configuration check
requires today, so that needs its own change. The name should not be reused for
a different caller without a note (documentation, not enforced).

### Doctor check

A check per credential file, named by client and path, never by content. It
mirrors `checkSecretInfo` (`internal/config/config.go`), so every finding is Fail:

| Finding | Severity |
| --- | --- |
| File missing, unreadable, a link, not a regular file, empty, or larger than 64 KiB | Fail |
| Mode is anything other than exactly `0600` | Fail |
| Owner is not the current account | Fail |
| Hard-link count above 1 | Fail |
| Two clients share a token, or a name is invalid or duplicate | Fail |

The owner check is against the account running the check. Doctor may run as a
different account than `whr serve` (D49); the note leaves the doctor's
account rule to the existing doctor behaviour.

The existing `doctor` probe that sends the configured token keeps working for
the CLI's own credential.

### Hard requirement: no token in any output

No token or token digest appears in audit rows, logs, errors, doctor output,
`--json` output or the process list. Only the client name, the derived id and
the file path may appear. Errors from `ReadSecret` already name the path, never
the content; the new code follows the same rule and `redact` stays as the
last line of defence, not the first. Comparison stays constant time over
digests (`authorized`), now against every configured client, without early exit
on the first match.

### What this does not cover

- No OAuth, user accounts, sessions or per-request signing.
- No protection against a host user who can read the token files (the check
  reports bad modes; it does not stop root or the same account).
- No protection against a stolen token in use: it acts as that client until
  revoked. The audit then at least names which client to revoke.
- No per-client permissions, rate limits or expiry.
- The client name is not authenticated identity beyond the token that selects it.

### Open questions for Werner

1. Is a handful of clients per host right? If not, should this be a file-per-client directory with `whr` commands?
2. Should rotation allow two live tokens per client for a grace window?
3. Is `api:<name>` the right actor shape, and should `default` keep the derived id (proposed) or become `api:default`?
4. Should tokens be reloaded on a signal, or only on restart of `whr serve`?
5. Should the CLI keep using `default`, and should `api_token_file` stay required once `api_clients` exists?

### After note approval: implementation slice

Not part of this note. A first slice is configuration, actor naming and the
doctor check only, with these tests:

- Two clients produce two distinct audit actors.
- No token (or digest) appears in audit output, logs, errors or doctor output.
- The single-token setup is unchanged: same actor id as #357, same behaviour.
- Doctor reports an unreadable and a world-readable credential file.
- Config check refuses duplicate names, duplicate tokens and reserved names.
