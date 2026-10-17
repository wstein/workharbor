# Pilot research

Status: researched choices for implementation review; nothing claimed implemented.

- Separate Go modules plus root workspace: [Go workspaces](https://go.dev/doc/tutorial/workspaces).
  Build distribution archives from the full checkout; independent module publication
  needs directory-prefixed tags and is not implied by a root release tag.
- Cobra/pflag handles flags after positional arguments and completion;
  [stdlib flag](https://pkg.go.dev/flag) stops at the first positional.
  [Cobra guide](https://github.com/spf13/cobra/blob/main/site/content/user_guide.md).
  Silence framework printing and route failures through the frozen JSON/exit layer.
- [Coder WebSocket](https://pkg.go.dev/github.com/coder/websocket): encoded limits
  include base64 expansion; disable compression. Graceful Close can exceed five
  seconds, so cleanup must cancel/release independently and force close by deadline.
- [JWT best practices](https://www.rfc-editor.org/info/rfc8725/) and
  [Go validation options](https://pkg.go.dev/github.com/golang-jwt/jwt/v5): require
  explicit EdDSA, issuer/audience/expiry, configured keys, runtime generation and
  atomic replay consumption. A valid signature alone does not enforce single use.
- [Go SSH](https://pkg.go.dev/golang.org/x/crypto/ssh): FixedHostKey, fresh certs and
  an owner-authorized tunnel avoid personal config/agent access. External
  [OpenSSH config](https://man.openbsd.org/ssh_config) needs isolated known hosts,
  identities, disabled forwarding/connection sharing and no untrusted shell expansion.
- Linux host management uses fixed argument vectors for systemctl, atomic private
  state, and explicit Docker privilege. Docker-group membership is host-privileged;
  service sandbox flags do not turn it into a hostile-workload isolation boundary.
  [systemd source](https://github.com/systemd/systemd/blob/main/man/systemd.exec.xml).
- [Go build flags](https://pkg.go.dev/cmd/go): trim paths, stamp exact VCS revision;
  derive build date from commit/tag for reproducibility. Embedded version is not
  provenance. [GitHub attestations](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations)
  and [Syft](https://github.com/anchore/syft) are candidates for reviewed release jobs.
  Pin action/tool commits, publish drafts only, and retain downloadable verification.

- Local first increment (H77): Go [`os.Root`](https://pkg.go.dev/os#Root) gives traversal-resistant access but follows
  symlinks that stay inside the root, so `validate` adds an explicit per-component `Lstat` refusal. Cobra aliases
  rename siblings only, so short forms are generated root commands. Git on a hostile directory is limited to
  the fixed vectors listed in the plan (`ls-files --stage`, `rev-parse`, `ls-tree`, `cat-file`) with an isolated environment because `status`/`diff` can run fsmonitor or filters;
  `.gitmodules` is parsed in process.

## Decisions still needed before dependent WIs

Provider-reachable in-container endpoint transport without undeclared helper or
native-tool configuration; SSH-enabled workspace/server-key provisioning; actual
Linux disposable acceptance host and live authorization; signed release signer.
These do not block pure scaffolding or the selected migration-vector WI, but do
block the corresponding interoperation/live/release acceptance claims.
