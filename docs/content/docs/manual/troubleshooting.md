---
title: Troubleshooting
description: What to check when whr fails to start.
weight: 17
toc: true
---

## `zsh: killed` with no output

Running `whr` (for example `whr --dev setup host`) prints only `zsh: killed`. The suspected cause is a macOS code signature that is invalid or stale after the binary was copied or rewritten in place over a previous one. That cause is {{< status unverified >}} until `codesign -v` and `xattr -l` output from the affected host confirms it (issue #393). Check the binary:

```text
codesign -v <path>     # no output and exit 0 means the signature is valid
xattr -l <path>        # lists extended attributes such as com.apple.quarantine
```

Fix: rebuild with `make install` from the clean current `main`. It signs the new `whr` ad hoc and replaces the old file by rename.
