# Cobra, pflag and mousetrap provenance

The `whr` CLI (`apps/whr`) pins github.com/spf13/cobra v1.10.2 (Apache-2.0,
[LICENSE](LICENSE)) with its dependencies github.com/spf13/pflag v1.0.9
(BSD-3-Clause, [../pflag/LICENSE](../pflag/LICENSE)) and
github.com/inconshreveable/mousetrap v1.1.0 (Apache-2.0,
[../mousetrap/LICENSE](../mousetrap/LICENSE)), as chosen by ADR 0010 and task
T025. They generate the command tree, help hooks and shell completion scripts
from the command table. Nothing is vendored; exact module hashes are in
`apps/whr/go.sum`. Preserve these notices in later T023 artifact assembly.

github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 (Apache-2.0) is a test-only
dependency that validates `--json` output against
`contracts/cli/output.schema.json`; no product package imports it, and its
indirect modules (golang.org/x/text, github.com/dlclark/regexp2) are not linked
into the `whr` binary. No notice is retained for it because it is not
distributed. Revisit this if it ever enters product code.

Cobra's hidden `__complete` command is part of the completion protocol the
generated scripts call; it is not a user-facing command and is not in
`contracts/cli/commands.json`.

Primary release: https://github.com/spf13/cobra/releases/tag/v1.10.2
